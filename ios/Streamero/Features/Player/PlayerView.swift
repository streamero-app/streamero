import AVFoundation
import AVKit
import SwiftUI

/// Everything the video player does besides drawing: stream setup, progress every 10 s,
/// session commands every 4 s, skip intro, next episode, audio and subtitle tracks.
/// Inside a watch-together group (D-036) the player follows the group instead of its own controls.
@MainActor
@Observable
final class PlayerModel {
    let client: MediaClient
    private let target: PlayTarget
    private(set) var current: String
    private(set) var player: AVPlayer?
    private(set) var info: PlaybackInfo?
    private(set) var detail: ItemDetail?
    private(set) var prefs: Preferences?
    private(set) var error = false
    private(set) var position: Int64 = 0
    private(set) var next: ItemSummary?
    private(set) var countdown = 10
    private(set) var ended = false
    private(set) var notice: String?
    /// Set when an administrator stopped playback; the view closes itself.
    private(set) var stopped = false
    private(set) var subtitleIndex = -1
    private(set) var audioIndex: Int?
    private(set) var cues: [SubtitleCue] = []
    private(set) var cue: SubtitleCue?
    var dismissedNext = false
    private(set) var skippedSegment: Int64?

    private var offset: Int64 = 0
    private var ticks = 0
    private var noticeUntil: Date?
    private var counting = false
    private var creditsHandled = false
    private var loading = false

    // Watch together
    let together: SyncSession
    /// Following a watch-together group.
    private(set) var inGroup = false
    private var syncBusy = false
    private var syncDirty = false
    private var pendingSync: [SyncAction] = []
    private var syncLoop: Task<Void, Never>?
    private var startTask: Task<Void, Never>?

    init(client: MediaClient, target: PlayTarget, together: SyncSession) {
        self.client = client
        self.target = target
        self.current = target.id
        self.together = together
    }

    private var introMode: String { prefs?.skipIntro ?? "button" }
    private var creditsMode: String { prefs?.skipCredits ?? "button" }
    private var autoplay: Bool { prefs?.autoplayNext ?? true }
    private var upNextSeconds: Int { prefs?.upNextSeconds ?? 10 }

    var duration: Int64 { info?.durationMs ?? detail?.item.durationMs ?? 0 }

    /// The intro or recap under the playhead that still offers a skip.
    var intro: MediaSegment? {
        info?.segments?.first { ($0.kind == "intro" || $0.kind == "recap") && position >= $0.startMs && position < $0.endMs - 1000 }
    }

    var showSkipButton: Bool {
        guard let intro else { return false }
        return introMode == "button" && skippedSegment != intro.startMs
    }

    private var inCredits: Bool {
        info?.segments?.contains { $0.kind == "credits" && position >= $0.startMs } == true
    }

    var showNext: Bool {
        guard !inGroup, next != nil, !dismissedNext, creditsMode != "off" else { return false }
        return ended || inCredits || (duration > 0 && position > 0 && duration - position <= 20_000)
    }

    var autoplaysNext: Bool { autoplay && creditsMode != "off" }

    var titleLine: String {
        guard let it = detail?.item else { return "" }
        return (it.seriesTitle.map { "\($0) · " } ?? "") + it.title
    }

    // MARK: Start

    func begin() async {
        do {
            try AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
            try AVAudioSession.sharedInstance().setActive(true)
        } catch {}
        prefs = try? await client.preferences()
        detail = try? await client.item(current)
        next = try? await client.nextEpisode(current)
        await load(startMs: target.start, audio: target.audio, sub: target.sub)
        if together.group != nil { attachGroup() }
    }

    private func load(startMs: Int64?, audio: Int?, sub: Int?) async {
        loading = true
        defer { loading = false }
        do {
            let profile = MediaClient.device(preferences: prefs)
            // The version picked on the item page only applies to that item.
            let file = current == target.id ? target.file : nil
            let pi = try await client.startPlayback(item: current, startMs: startMs.map { max(0, $0) },
                                                    audioIndex: audio, fileId: file, profile: profile)
            guard let url = client.absolute(pi.url) else { throw APIError(status: 0, code: "bad_url") }
            player?.pause()
            info = pi
            audioIndex = audio ?? pi.audioIndex
            offset = pi.method == "direct" ? 0 : pi.startMs
            let chosen = sub ?? pi.subtitleDefault ?? -1
            subtitleIndex = chosen
            await loadCues(index: chosen)
            let p = AVPlayer(url: url)
            // Rate changes (group speed, drift correction) keep the pitch.
            p.currentItem?.audioTimePitchAlgorithm = .spectral
            if pi.method == "direct" && pi.startMs > 0 {
                await p.seek(to: CMTime(value: pi.startMs, timescale: 1000))
            }
            player = p
            if inGroup || together.group != nil {
                // The group decides when to play.
                together.realign()
                syncDirty = true
            } else {
                p.play()
            }
        } catch {
            self.error = true
        }
    }

    private func loadCues(index: Int) async {
        cues = []
        cue = nil
        guard let subs = info?.subtitles, subs.indices.contains(index), let url = client.absolute(subs[index].url) else { return }
        if let text = try? await client.text(from: url) { cues = WebVTT.parse(text) }
    }

    // MARK: Tracks

    func selectAudio(_ index: Int) async {
        await load(startMs: absolutePosition(), audio: index, sub: subtitleIndex)
    }

    func selectSubtitle(_ index: Int) async {
        subtitleIndex = index
        await loadCues(index: index)
    }

    private func absolutePosition() -> Int64 {
        guard let player else { return position }
        let seconds = player.currentTime().seconds
        return seconds.isFinite ? offset + Int64(seconds * 1000) : position
    }

    // MARK: Loop, one tick per second

    func tick() async {
        guard let player, info != nil else { return }
        position = absolutePosition()
        cue = WebVTT.cue(in: cues, at: position)
        if let item = player.currentItem, item.duration.isNumeric, player.currentTime() >= item.duration { ended = true }
        ticks += 1
        if ticks % 10 == 0 && player.timeControlStatus == .playing { await report() }
        if ticks % 4 == 0 { await pollCommands() }
        if let until = noticeUntil, Date() >= until { notice = nil; noticeUntil = nil }
        await handleSegments()
        await handleNext()
    }

    private func handleSegments() async {
        if !inGroup, let intro, introMode == "auto", skippedSegment != intro.startMs {
            skippedSegment = intro.startMs
            await seek(to: intro.endMs)
        }
    }

    private func handleNext() async {
        if inGroup { return }
        if inCredits && creditsMode == "auto" && !creditsHandled && next != nil {
            creditsHandled = true
            await playNext()
            return
        }
        guard showNext, autoplaysNext else { counting = false; return }
        if !counting { counting = true; countdown = upNextSeconds }
        countdown -= 1
        if countdown <= 0 { await playNext() }
    }

    private func pollCommands() async {
        guard let commands = try? await client.commands(), !commands.isEmpty else { return }
        for command in commands {
            if command.kind == "stop" {
                player?.pause()
                showNotice(command.text ?? Loc.string("stopped_by_admin"))
                try? await Task.sleep(for: .seconds(4))
                stopped = true
                return
            }
            if let text = command.text { showNotice(text) }
        }
    }

    private func showNotice(_ text: String) {
        notice = text
        noticeUntil = Date().addingTimeInterval(10)
    }

    // MARK: Seeking

    func skipIntro() async {
        guard let intro else { return }
        skippedSegment = intro.startMs
        if inGroup {
            together.seek(intro.endMs)
            return
        }
        await seek(to: intro.endMs)
    }

    /// Seeks inside the stream when possible; a transcoded stream past what is ready restarts there.
    private func seek(to ms: Int64) async {
        guard let info, let player else { return }
        if info.method == "direct" {
            await player.seek(to: CMTime(value: ms, timescale: 1000))
            return
        }
        let relative = ms - offset
        if relative >= 0 && relative <= bufferedEnd(player) + 30_000 {
            await player.seek(to: CMTime(value: relative, timescale: 1000))
        } else {
            await load(startMs: ms, audio: audioIndex, sub: subtitleIndex)
        }
    }

    private func bufferedEnd(_ player: AVPlayer) -> Int64 {
        guard let range = player.currentItem?.loadedTimeRanges.last?.timeRangeValue else { return 0 }
        let end = range.end.seconds
        return end.isFinite ? Int64(end * 1000) : 0
    }

    // MARK: Next episode

    func playNext() async {
        guard let n = next else { return }
        await report()
        player?.pause()
        player = nil
        current = n.id
        info = nil
        next = nil
        position = 0
        ended = false
        dismissedNext = false
        skippedSegment = nil
        creditsHandled = false
        counting = false
        cues = []
        cue = nil
        detail = try? await client.item(current)
        next = try? await client.nextEpisode(current)
        await load(startMs: 0, audio: nil, sub: nil)
    }

    private func report() async {
        let pos = absolutePosition()
        if pos > 0 { await client.reportProgress(item: current, positionMs: pos, durationMs: info?.durationMs) }
    }

    func close() async {
        if inGroup {
            detachGroup()
            await together.leave()
        }
        await report()
        player?.pause()
        player = nil
    }

    // MARK: Watch together

    /// Leaves the group; playback goes on here on its own.
    func leaveGroup() async {
        guard inGroup else { return }
        detachGroup()
        await together.leave()
        showNotice(Loc.string("together.left"))
    }

    private func attachGroup() {
        guard together.group != nil, !inGroup else { return }
        inGroup = true
        dismissedNext = true
        player?.currentItem?.audioTimePitchAlgorithm = .spectral
        together.realign()
        together.listener = { [weak self] event in self?.onSync(event) }
        syncLoop?.cancel()
        syncLoop = Task { [weak self] in
            while !Task.isCancelled {
                guard let self else { return }
                self.syncNow()
                try? await Task.sleep(for: .milliseconds(250))
            }
        }
    }

    private func detachGroup() {
        inGroup = false
        together.listener = nil
        syncLoop?.cancel()
        syncLoop = nil
        startTask?.cancel()
        startTask = nil
        pendingSync = []
        if let p = player, p.rate != 0 { p.rate = 1 }
    }

    private func onSync(_ event: SyncEvent) {
        switch event {
        case .state:
            syncNow()
        case .correction(let c, let serverTime):
            pendingSync += together.correction(c, serverTime: serverTime)
            syncNow()
        case .ended(let reason):
            detachGroup()
            showNotice(Loc.string(reason == "left" ? "together.left" : "together.closed"))
        }
    }

    /// Runs the engine and executes its actions, one batch at a time.
    private func syncNow() {
        guard inGroup else { return }
        if syncBusy {
            syncDirty = true
            return
        }
        syncBusy = true
        Task {
            repeat {
                syncDirty = false
                let queued = pendingSync
                pendingSync = []
                await run(queued)
                if inGroup { await run(together.reconcile(localPlayback())) }
            } while syncDirty && inGroup
            syncBusy = false
        }
    }

    /// The local player as the sync engine sees it.
    private func localPlayback() -> LocalPlayback {
        let p = player
        let ready = p?.currentItem?.status == .readyToPlay
        let rate = p?.rate ?? 0
        return LocalPlayback(itemId: current,
                             loaded: info != nil && !loading && ready,
                             positionMs: absolutePosition(),
                             playing: rate != 0,
                             rate: Double(rate),
                             stalled: p?.timeControlStatus == .waitingToPlayAtSpecifiedRate,
                             seeking: loading,
                             ended: ended)
    }

    private func run(_ actions: [SyncAction]) async {
        for action in actions {
            guard inGroup else { return }
            switch action {
            case .open(let itemId, let startMs):
                await openForGroup(itemId, startMs: startMs)
            case .seek(let ms):
                await syncSeek(ms)
            case .play(let rate):
                startTask?.cancel()
                startTask = nil
                player?.rate = Float(rate)
            case .pause:
                startTask?.cancel()
                startTask = nil
                player?.pause()
            case .setRate(let rate):
                if let p = player, p.rate != 0 { p.rate = Float(rate) }
            case .startAt(let localMs, let positionMs, let rate):
                startTask?.cancel()
                player?.pause()
                await syncSeek(positionMs)
                startTask = Task { [weak self] in
                    let wait = localMs - SyncSession.nowMs()
                    if wait > 0 { try? await Task.sleep(for: .milliseconds(wait)) }
                    guard !Task.isCancelled, let self, self.inGroup else { return }
                    self.player?.rate = Float(rate)
                }
            case .send(let message):
                together.send(message)
            }
        }
    }

    /// Exact seek for the group; a transcoded stream past what is ready restarts there.
    private func syncSeek(_ ms: Int64) async {
        guard let info, let player else { return }
        let target = max(0, ms)
        let relative = info.method == "direct" ? target : target - offset
        if info.method == "direct" || (relative >= 0 && relative <= bufferedEnd(player) + 30_000) {
            _ = await player.seek(to: CMTime(value: relative, timescale: 1000), toleranceBefore: .zero, toleranceAfter: .zero)
            position = target
            ended = false
        } else {
            await load(startMs: target, audio: audioIndex, sub: subtitleIndex)
        }
    }

    /// Switches to the item the group plays now.
    private func openForGroup(_ itemId: String, startMs: Int64) async {
        await report()
        player?.pause()
        player = nil
        current = itemId
        info = nil
        position = 0
        ended = false
        skippedSegment = nil
        creditsHandled = false
        counting = false
        cues = []
        cue = nil
        next = nil
        detail = try? await client.item(itemId)
        await load(startMs: startMs, audio: nil, sub: nil)
    }
}

/// Full-screen player over AVKit's controls.
struct PlayerView: View {
    let target: PlayTarget
    let client: MediaClient
    let music: MusicPlayer
    @Environment(\.dismiss) private var dismiss
    @State private var model: PlayerModel

    init(target: PlayTarget, client: MediaClient, music: MusicPlayer, together: SyncSession) {
        self.target = target
        self.client = client
        self.music = music
        _model = State(initialValue: PlayerModel(client: client, target: target, together: together))
    }

    private var subtitleStyle: SubtitleStyle { SubtitleStyle(prefs: model.prefs) }

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if let player = model.player {
                if model.inGroup {
                    // The group's own controls replace AVKit's, so every action goes to the group.
                    PlayerLayerView(player: player).ignoresSafeArea()
                } else {
                    VideoPlayer(player: player).ignoresSafeArea()
                }
            } else if model.error {
                Text("player.error").foregroundStyle(.white).padding()
            } else {
                ProgressView().tint(.white).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            if let cue = model.cue {
                subtitleStyle.view(cue.text)
                    .padding(.horizontal, 24).padding(.bottom, 56)
                    .frame(maxHeight: .infinity, alignment: .bottom)
                    .allowsHitTesting(false)
            }
            VStack {
                topBar
                if let notice = model.notice {
                    Text(verbatim: notice).font(.callout).foregroundStyle(.white).multilineTextAlignment(.center)
                        .padding(14).background(.black.opacity(0.85), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                }
                Spacer()
                HStack(alignment: .bottom) {
                    Spacer()
                    if model.showNext, let next = model.next {
                        nextCard(next)
                    } else if model.showSkipButton, let intro = model.intro {
                        Button { Task { await model.skipIntro() } } label: {
                            Label(intro.kind == "recap" ? "player.skipRecap" : "player.skipIntro", systemImage: "forward.end.fill")
                        }
                        .buttonStyle(MutedButtonStyle())
                    }
                }
                .padding(.horizontal, 24).padding(.bottom, model.inGroup ? 150 : 90)
            }
            if model.inGroup, let group = model.together.group {
                TogetherOverlay(model: model, group: group)
            }
        }
        .task {
            UIApplication.shared.isIdleTimerDisabled = true
            music.pause()
            await model.begin()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(1))
                await model.tick()
                if model.stopped { dismiss(); return }
            }
        }
        .onDisappear {
            UIApplication.shared.isIdleTimerDisabled = false
            Task { await model.close() }
        }
    }

    private var topBar: some View {
        HStack {
            Button { Task { await close() } } label: {
                Image(systemName: "xmark.circle.fill").font(.title).foregroundStyle(.white.opacity(0.8)).padding()
            }
            .accessibilityLabel(Text("player.back"))
            Text(verbatim: model.titleLine).font(.subheadline.weight(.semibold)).foregroundStyle(.white).lineLimit(1)
            Spacer()
            if model.inGroup {
                Button { Task { await model.leaveGroup() } } label: {
                    Image(systemName: "person.2.slash").font(.title2).foregroundStyle(.white.opacity(0.8)).padding()
                }
                .accessibilityLabel(Text("together.leave"))
            }
            if let info = model.info, info.audio.count > 1 {
                Menu {
                    ForEach(Array(info.audio.enumerated()), id: \.offset) { idx, a in
                        Button { Task { await model.selectAudio(idx) } } label: {
                            Text(verbatim: [a.language?.uppercased() ?? "#\(idx + 1)", a.codec.uppercased(), a.title].compactMap { $0 }.joined(separator: " "))
                        }
                    }
                } label: {
                    Image(systemName: "speaker.wave.2.circle.fill").font(.title).foregroundStyle(.white.opacity(0.8)).padding()
                }
                .accessibilityLabel(Text("player.audio"))
            }
            if let info = model.info, !info.subtitles.isEmpty {
                Menu {
                    Button { Task { await model.selectSubtitle(-1) } } label: { Text("off") }
                    ForEach(Array(info.subtitles.enumerated()), id: \.offset) { idx, s in
                        Button { Task { await model.selectSubtitle(idx) } } label: {
                            Text(verbatim: [s.language?.uppercased() ?? "?", s.forced == true ? Loc.string("forced") : nil].compactMap { $0 }.joined(separator: " · "))
                        }
                    }
                } label: {
                    Image(systemName: "captions.bubble.fill").font(.title).foregroundStyle(.white.opacity(0.8)).padding()
                }
                .accessibilityLabel(Text("subtitles"))
            }
        }
    }

    private func close() async {
        await model.close()
        dismiss()
    }

    private func nextCard(_ n: ItemSummary) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Group {
                if model.autoplaysNext { Text("player.nextIn \(max(0, model.countdown))") } else { Text("player.upNext") }
            }
            .font(.caption).foregroundStyle(.white.opacity(0.7))
            Text(verbatim: episodeTitle(n)).font(.subheadline.bold()).foregroundStyle(.white).lineLimit(2)
            HStack {
                Button { Task { await model.playNext() } } label: { Label("player.playNext", systemImage: "play.fill") }
                    .buttonStyle(PrimaryButtonStyle())
                Button { model.dismissedNext = true } label: { Image(systemName: "xmark") }
                    .buttonStyle(MutedButtonStyle())
                    .accessibilityLabel(Text("player.back"))
            }
        }
        .padding(14)
        .frame(maxWidth: 320, alignment: .leading)
        .background(.black.opacity(0.8), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
    }
}

/// How subtitles are drawn, from the profile's subtitle look settings.
struct SubtitleStyle {
    var size: CGFloat
    var color: Color
    var background: String

    init(prefs: Preferences?) {
        switch prefs?.subtitleSize {
        case "small": size = 16
        case "large": size = 26
        case "xlarge": size = 32
        default: size = 20
        }
        switch prefs?.subtitleColor {
        case "yellow": color = .yellow
        case "cyan": color = .cyan
        default: color = .white
        }
        background = prefs?.subtitleBackground ?? "shadow"
    }

    @ViewBuilder func view(_ text: String) -> some View {
        let line = Text(verbatim: text).font(.system(size: size, weight: .medium)).foregroundStyle(color).multilineTextAlignment(.center)
        switch background {
        case "box":
            line.padding(.horizontal, 8).padding(.vertical, 3).background(.black.opacity(0.75), in: RoundedRectangle(cornerRadius: 4))
        case "none":
            line
        default:
            line.shadow(color: .black, radius: 2, y: 1)
        }
    }
}
