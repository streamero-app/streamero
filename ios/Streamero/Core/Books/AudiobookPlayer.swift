import AVFoundation
import MediaPlayer
import Observation
import UIKit

/// Audiobook playback: the book's files played as one timeline in the background, with the
/// lock screen, chapters, speed, skip and a sleep timer. Positions are in the whole book and
/// are reported through the normal progress endpoint (every 15 s and on pause).
@MainActor
@Observable
final class AudiobookPlayer {
    private(set) var item: ItemSummary?
    private(set) var timeline: AudiobookTimeline?
    private(set) var playing = false
    private(set) var position: Int64 = 0
    private(set) var loading = false
    private(set) var failed = false
    private(set) var rate: Float = 1
    private(set) var sleep: SleepTimer = .off
    private(set) var sleepPlan: SleepTimer.Plan = .none

    static let speeds: [Float] = [0.75, 1, 1.25, 1.5, 1.75, 2]
    static let sleepMinutes = [5, 10, 15, 30, 45, 60]

    var duration: Int64 { timeline?.totalMs ?? 0 }
    var chapterIndex: Int? { timeline?.chapterIndex(at: position) }
    var chapterTitle: String? {
        guard let timeline, let i = timeline.chapterIndex(at: position) else { return nil }
        return timeline.chapters[i].title
    }

    /// Called before a book starts so the music player can stop.
    @ObservationIgnored var onStart: (@MainActor () -> Void)?

    private let client: MediaClient
    private let player = AVPlayer()
    @ObservationIgnored private var partIndex = -1
    /// Where the current stream starts inside its file (non-zero only for transcoded streams).
    @ObservationIgnored private var streamOffset: Int64 = 0
    @ObservationIgnored private var direct = true
    /// The file ended without moving on (sleep timer or end of book); play loads again.
    @ObservationIgnored private var needsReload = false
    @ObservationIgnored private var loadTask: Task<Void, Never>?
    @ObservationIgnored private var endObserver: (any NSObjectProtocol)?
    @ObservationIgnored private var timeObserver: Any?
    @ObservationIgnored private var lastReport = Date.distantPast
    @ObservationIgnored private var artwork: MPMediaItemArtwork?

    init(client: MediaClient) {
        self.client = client
        timeObserver = player.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.5, preferredTimescale: 600), queue: .main) { [weak self] _ in
            Task { @MainActor in self?.tick() }
        }
    }

    // MARK: Opening

    /// Opens a book at its saved position (or `startAt`) and plays it; the same book just resumes.
    func open(_ book: ItemSummary, startAt: Int64? = nil) {
        if item?.id == book.id, timeline != nil, !failed {
            if let startAt { seek(startAt) }
            if !playing { play() }
            return
        }
        if item != nil { reportNow() }
        onStart?()
        stopPlayer()
        item = book
        timeline = nil
        position = 0
        failed = false
        loading = true
        artwork = nil
        sleep = .off
        sleepPlan = .none
        setupRemoteIfNeeded()
        activateAudio()
        loadTask = Task { [client] in
            do {
                let manifest = try await client.openAudiobook(book.id)
                guard !Task.isCancelled, item?.id == book.id else { return }
                let t = AudiobookTimeline(manifest)
                timeline = t
                let start = startAt.map { min(max(0, $0), t.totalMs) } ?? t.resumePosition(manifest.positionMs)
                position = start
                updateNowPlaying()
                await load(at: start, autoplay: true)
                await fetchArtwork(for: book)
            } catch {
                guard !Task.isCancelled, item?.id == book.id else { return }
                failed = true
                loading = false
            }
        }
    }

    /// Requests the stream of the file at `ms` and starts it there.
    private func load(at ms: Int64, autoplay: Bool) async {
        guard let book = item, let timeline, let loc = timeline.locate(ms) else {
            loading = false
            return
        }
        loading = true
        let part = timeline.parts[loc.part]
        do {
            let info = try await client.startPlayback(item: book.id, startMs: loc.offsetMs > 0 ? loc.offsetMs : nil,
                                                      fileId: part.fileId, profile: MediaClient.audioProfile)
            guard !Task.isCancelled, item?.id == book.id else { return }
            guard let url = client.absolute(info.url) else { throw APIError(status: 0, code: "bad_url") }
            let playerItem = AVPlayerItem(url: url)
            // Keeps voices natural at other speeds.
            playerItem.audioTimePitchAlgorithm = .timeDomain
            observeEnd(of: playerItem)
            partIndex = loc.part
            direct = info.method == "direct"
            streamOffset = direct ? 0 : info.startMs
            needsReload = false
            player.replaceCurrentItem(with: playerItem)
            if direct && loc.offsetMs > 0 {
                await player.seek(to: CMTime(value: loc.offsetMs, timescale: 1000), toleranceBefore: .zero, toleranceAfter: .zero)
            }
            guard !Task.isCancelled, item?.id == book.id else { return }
            position = timeline.bookMs(part: loc.part, offsetMs: loc.offsetMs)
            loading = false
            player.defaultRate = rate
            if autoplay {
                activateAudio()
                player.play()
                player.rate = rate
                playing = true
            } else {
                playing = false
            }
            updateNowPlaying()
        } catch {
            guard !Task.isCancelled, item?.id == book.id else { return }
            failed = true
            loading = false
            playing = false
        }
    }

    private func reload(at ms: Int64, autoplay: Bool) {
        loadTask?.cancel()
        loading = true
        position = ms
        loadTask = Task { await load(at: ms, autoplay: autoplay) }
    }

    private func observeEnd(of playerItem: AVPlayerItem) {
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        endObserver = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: playerItem, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.partEnded() }
        }
    }

    // MARK: Controls

    func play() {
        guard item != nil, let timeline else { return }
        if needsReload || player.currentItem == nil {
            // Finished books start over.
            reload(at: position >= timeline.totalMs - 1000 ? 0 : position, autoplay: true)
            return
        }
        activateAudio()
        player.defaultRate = rate
        player.play()
        player.rate = rate
        playing = true
        updateElapsed()
    }

    func pause() {
        guard item != nil else { return }
        player.pause()
        playing = false
        reportNow()
        updateElapsed()
    }

    func toggle() {
        if playing { pause() } else { play() }
    }

    func seek(_ ms: Int64) {
        guard let timeline, let loc = timeline.locate(ms) else { return }
        let target = timeline.bookMs(part: loc.part, offsetMs: loc.offsetMs)
        let relative = loc.offsetMs - streamOffset
        let canSeek = loc.part == partIndex && player.currentItem != nil && !needsReload && !loading
            && relative >= 0 && (direct || relative <= bufferedEnd() + 30_000)
        if canSeek {
            position = target
            player.seek(to: CMTime(value: relative, timescale: 1000), toleranceBefore: .zero, toleranceAfter: .zero)
        } else {
            reload(at: target, autoplay: playing)
        }
        updateElapsed()
    }

    func skip(seconds: Int) { seek(position + Int64(seconds) * 1000) }

    func nextChapter() {
        if let t = timeline?.nextChapterStart(from: position) { seek(t) }
    }

    func previousChapter() {
        if let t = timeline?.previousChapterStart(from: position) { seek(t) }
    }

    func jump(chapter index: Int) {
        guard let timeline, timeline.chapters.indices.contains(index) else { return }
        seek(timeline.chapters[index].startMs)
        if !playing { play() }
    }

    func setRate(_ value: Float) {
        rate = min(2, max(0.5, value))
        player.defaultRate = rate
        if playing { player.rate = rate }
        updateElapsed()
    }

    func setSleep(_ timer: SleepTimer) {
        sleep = timer
        guard let timeline else {
            sleepPlan = .none
            return
        }
        sleepPlan = timer.plan(now: Date(), positionMs: position, timeline: timeline)
    }

    func stop() {
        guard item != nil else { return }
        reportNow()
        stopPlayer()
        item = nil
        timeline = nil
        playing = false
        loading = false
        failed = false
        position = 0
        sleep = .off
        sleepPlan = .none
        artwork = nil
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
        Self.setSkipCommands(enabled: false)
    }

    private func stopPlayer() {
        loadTask?.cancel()
        player.pause()
        player.replaceCurrentItem(with: nil)
        partIndex = -1
        streamOffset = 0
        needsReload = false
        if let endObserver {
            NotificationCenter.default.removeObserver(endObserver)
            self.endObserver = nil
        }
    }

    // MARK: Clock

    private func tick() {
        guard item != nil, let timeline, !loading, partIndex >= 0, !needsReload else { return }
        let seconds = player.currentTime().seconds
        if seconds.isFinite {
            position = timeline.bookMs(part: partIndex, offsetMs: Int64(seconds * 1000) + streamOffset)
        }
        // Paused from outside (a call, unplugged headphones).
        if playing && player.timeControlStatus == .paused && player.rate == 0 {
            playing = false
            reportNow()
        }
        if playing && sleepPlan.isDue(now: Date(), positionMs: position) {
            setSleep(.off)
            pause()
            return
        }
        if playing && Date().timeIntervalSince(lastReport) >= 15 { reportNow() }
        updateElapsed()
    }

    private func partEnded() {
        guard item != nil, let timeline, timeline.parts.indices.contains(partIndex) else { return }
        let part = timeline.parts[partIndex]
        position = min(part.offsetMs + part.durationMs, timeline.totalMs)
        let isLast = partIndex + 1 >= timeline.parts.count
        if isLast || sleepPlan.isDue(now: Date(), positionMs: position) {
            if !isLast { setSleep(.off) }
            needsReload = true
            playing = false
            if isLast { position = timeline.totalMs }
            reportNow()
            updateElapsed()
            return
        }
        reload(at: timeline.parts[partIndex + 1].offsetMs, autoplay: true)
    }

    private func bufferedEnd() -> Int64 {
        let ranges = player.currentItem?.loadedTimeRanges.map(\.timeRangeValue) ?? []
        let end = ranges.map { CMTimeGetSeconds(CMTimeRangeGetEnd($0)) }.filter(\.isFinite).max() ?? 0
        return Int64(end * 1000)
    }

    private func reportNow() {
        guard let id = item?.id, let total = timeline?.totalMs, total > 0 else { return }
        lastReport = Date()
        let pos = position
        Task { [client] in await client.reportProgress(item: id, positionMs: pos, durationMs: total) }
    }

    private func activateAudio() {
        do {
            try AVAudioSession.sharedInstance().setCategory(.playback, mode: .spokenAudio)
            try AVAudioSession.sharedInstance().setActive(true)
        } catch {}
    }

    // MARK: Now playing

    private func fetchArtwork(for book: ItemSummary) async {
        guard book.hasPoster, let url = client.imageURL(item: book.id, type: "poster"),
              let image = await ImageCache.shared.image(url: url, token: client.token),
              item?.id == book.id else { return }
        artwork = Self.artwork(from: image)
        updateNowPlaying()
    }

    /// Nonisolated: the system calls the handler on a background queue.
    nonisolated private static func artwork(from image: UIImage) -> MPMediaItemArtwork {
        MPMediaItemArtwork(boundsSize: image.size) { _ in image }
    }

    private func updateNowPlaying() {
        guard let book = item else { return }
        var info: [String: Any] = [:]
        info[MPMediaItemPropertyTitle] = book.title
        info[MPMediaItemPropertyArtist] = (book.authors ?? []).joined(separator: ", ")
        info[MPNowPlayingInfoPropertyMediaType] = MPNowPlayingInfoMediaType.audio.rawValue
        if let artwork { info[MPMediaItemPropertyArtwork] = artwork }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
        updateElapsed()
    }

    private func updateElapsed() {
        guard item != nil, var info = MPNowPlayingInfoCenter.default().nowPlayingInfo else { return }
        info[MPMediaItemPropertyAlbumTitle] = chapterTitle ?? ""
        info[MPMediaItemPropertyPlaybackDuration] = Double(duration) / 1000
        info[MPNowPlayingInfoPropertyElapsedPlaybackTime] = Double(position) / 1000
        info[MPNowPlayingInfoPropertyPlaybackRate] = playing ? Double(rate) : 0.0
        info[MPNowPlayingInfoPropertyDefaultPlaybackRate] = Double(rate)
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
    }

    // MARK: Lock screen

    /// The player the lock-screen commands act on; the handlers stay registered for the app's lifetime.
    private static weak var active: AudiobookPlayer?
    private static var remoteRegistered = false

    private func setupRemoteIfNeeded() {
        Self.active = self
        Self.setSkipCommands(enabled: true)
        guard !Self.remoteRegistered else { return }
        Self.remoteRegistered = true
        Self.registerRemoteCommands()
    }

    /// A book shows skip buttons on the lock screen; music shows next and previous track.
    nonisolated private static func setSkipCommands(enabled: Bool) {
        let center = MPRemoteCommandCenter.shared()
        center.skipForwardCommand.isEnabled = enabled
        center.skipBackwardCommand.isEnabled = enabled
        center.nextTrackCommand.isEnabled = !enabled
        center.previousTrackCommand.isEnabled = !enabled
    }

    /// Nonisolated: command handlers must not capture main-actor isolation. Every handler checks
    /// that a book is loaded, because the music player's handlers run for the same commands.
    nonisolated private static func registerRemoteCommands() {
        let center = MPRemoteCommandCenter.shared()
        center.skipForwardCommand.preferredIntervals = [30]
        center.skipBackwardCommand.preferredIntervals = [15]
        _ = center.playCommand.addTarget { _ in
            Task { @MainActor in
                if let p = active, p.item != nil, !p.playing { p.play() }
            }
            return .success
        }
        _ = center.pauseCommand.addTarget { _ in
            Task { @MainActor in
                if let p = active, p.item != nil { p.pause() }
            }
            return .success
        }
        _ = center.togglePlayPauseCommand.addTarget { _ in
            Task { @MainActor in
                if let p = active, p.item != nil { p.toggle() }
            }
            return .success
        }
        _ = center.skipForwardCommand.addTarget { _ in
            Task { @MainActor in
                if let p = active, p.item != nil { p.skip(seconds: 30) }
            }
            return .success
        }
        _ = center.skipBackwardCommand.addTarget { _ in
            Task { @MainActor in
                if let p = active, p.item != nil { p.skip(seconds: -15) }
            }
            return .success
        }
        _ = center.changePlaybackPositionCommand.addTarget { event in
            guard let event = event as? MPChangePlaybackPositionCommandEvent else { return .commandFailed }
            let ms = Int64(event.positionTime * 1000)
            Task { @MainActor in
                if let p = active, p.item != nil { p.seek(ms) }
            }
            return .success
        }
    }
}
