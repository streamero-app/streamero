import AVFoundation
import AVKit
import SwiftUI

/// Plays a downloaded file from the device: local position, local WebVTT sidecars, progress
/// recorded on the device (and sent to the server too while online).
@MainActor
@Observable
final class OfflinePlayerModel {
    let entry: OfflineEntry
    private(set) var player: AVPlayer?
    private(set) var missing = false
    private(set) var subtitleIndex = -1
    private(set) var cue: SubtitleCue?
    private var cues: [SubtitleCue] = []
    private var progress = ProgressTrigger()
    private var closed = false

    init(entry: OfflineEntry) {
        self.entry = entry
    }

    var titleLine: String {
        let it = entry.item
        if it.kind == "episode" {
            return (entry.series?.title ?? it.seriesTitle).map { "\($0) · " + episodeTitle(it) } ?? episodeTitle(it)
        }
        return it.title
    }

    func begin() async {
        do {
            try AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
            try AVAudioSession.sharedInstance().setActive(true)
        } catch {}
        let manager = DownloadManager.shared
        // The latest state (a sync may have moved the position since the list was drawn).
        let current = manager.entry(entry.itemId) ?? entry
        guard let url = manager.fileURL(for: current), FileManager.default.fileExists(atPath: url.path) else {
            missing = true
            return
        }
        let p = AVPlayer(url: url)
        // Seeking before the item is ready can land on the wrong spot or be dropped.
        await p.seekWhenReady(toMs: current.resumeMs)
        player = p
        p.play()
        // Forced subtitles (foreign dialogue) start on by themselves.
        if let forced = current.subtitles.firstIndex(where: { $0.forced }) {
            selectSubtitle(forced)
        }
    }

    func selectSubtitle(_ index: Int) {
        subtitleIndex = index
        cues = []
        cue = nil
        guard entry.subtitles.indices.contains(index),
              let url = DownloadManager.shared.localURL(entry.subtitles[index].file),
              let text = try? String(contentsOf: url, encoding: .utf8) else { return }
        cues = WebVTT.parse(text)
    }

    private var positionMs: Int64 {
        guard let player else { return 0 }
        let seconds = player.currentTime().seconds
        return seconds.isFinite ? Int64(seconds * 1000) : 0
    }

    private var durationMs: Int64? {
        if let item = player?.currentItem, item.duration.isNumeric {
            let seconds = item.duration.seconds
            if seconds.isFinite && seconds > 0 { return Int64(seconds * 1000) }
        }
        return entry.durationMs ?? entry.item.durationMs
    }

    var isPlaying: Bool { player?.timeControlStatus == .playing }

    /// One tick per second: subtitles; the position is recorded every 10 s and after a pause or seek.
    func tick() async {
        guard let player else { return }
        cue = WebVTT.cue(in: cues, at: positionMs)
        if progress.tick(playing: player.timeControlStatus == .playing, positionMs: positionMs) { await record() }
    }

    func record() async {
        let pos = positionMs
        guard pos > 0 else { return }
        let manager = DownloadManager.shared
        manager.recordProgress(itemId: entry.itemId, positionMs: pos, durationMs: durationMs)
        // Online too: the server hears it now; the sync later confirms the device's record.
        if manager.online, let client = manager.client {
            await client.reportProgress(item: entry.itemId, positionMs: pos, durationMs: durationMs)
        }
    }

    func close() async {
        guard !closed else { return }
        closed = true
        await record()
        player?.pause()
        player = nil
        await DownloadManager.shared.sync()
    }
}

/// Full-screen player for a downloaded item; works without any connection.
struct OfflinePlayerView: View {
    /// Music to pause while the video plays (nil in the offline library, where none runs).
    let music: MusicPlayer?
    @State private var model: OfflinePlayerModel
    @State private var chrome = PlayerChrome()
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase

    init(entry: OfflineEntry, music: MusicPlayer?) {
        self.music = music
        _model = State(initialValue: OfflinePlayerModel(entry: entry))
    }

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if let player = model.player {
                SystemVideoPlayer(player: player) { point, size in chrome.tap(at: point, in: size) }
                    .ignoresSafeArea()
            } else if model.missing {
                Text("downloads.missing").foregroundStyle(.white).multilineTextAlignment(.center).padding()
            } else {
                ProgressView().tint(.white).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            if let cue = model.cue {
                SubtitleStyle(prefs: nil).view(cue.text)
                    .padding(.horizontal, 24).padding(.bottom, chrome.visible ? 112 : 36)
                    .frame(maxHeight: .infinity, alignment: .bottom)
                    .allowsHitTesting(false)
                    .animation(.easeOut(duration: 0.25), value: chrome.visible)
            }
            VStack {
                topBar
                    .padding(.top, 40)
                    .opacity(chrome.visible ? 1 : 0)
                    .allowsHitTesting(chrome.visible)
                Spacer()
            }
        }
        .videoAudioFocus(music)
        .task {
            UIApplication.shared.isIdleTimerDisabled = true
            await model.begin()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(1))
                await model.tick()
                chrome.sync(playing: model.isPlaying)
            }
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .background { Task { await model.record() } }
        }
        .onDisappear {
            UIApplication.shared.isIdleTimerDisabled = false
            Task { await model.close() }
        }
    }

    private var topBar: some View {
        HStack {
            Button {
                Task {
                    await model.close()
                    dismiss()
                }
            } label: {
                Image(systemName: "xmark.circle.fill").font(.title).foregroundStyle(.white.opacity(0.8)).padding()
            }
            .accessibilityLabel(Text("player.back"))
            Text(verbatim: model.titleLine).font(.subheadline.weight(.semibold)).foregroundStyle(.white).lineLimit(1)
            Spacer()
            Image(systemName: "arrow.down.circle.fill").font(.footnote).foregroundStyle(.white.opacity(0.5))
                .accessibilityLabel(Text("downloads.offlineCopy"))
            if !model.entry.subtitles.isEmpty {
                Menu {
                    Button { model.selectSubtitle(-1) } label: { Text("off") }
                    ForEach(Array(model.entry.subtitles.enumerated()), id: \.offset) { idx, s in
                        Button { model.selectSubtitle(idx) } label: {
                            Text(verbatim: [s.language.uppercased(), s.forced ? Loc.string("forced") : nil].compactMap { $0 }.joined(separator: " · "))
                        }
                    }
                } label: {
                    Image(systemName: "captions.bubble.fill").font(.title).foregroundStyle(.white.opacity(0.8)).padding()
                }
                .accessibilityLabel(Text("subtitles"))
            }
        }
    }
}
