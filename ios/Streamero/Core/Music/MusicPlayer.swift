import AVFoundation
import MediaPlayer
import Observation
import UIKit

enum RepeatMode: Sendable {
    case off, all, one
}

/// Music playback with a queue: one AVPlayer that keeps playing in the background, with
/// now-playing info and lock-screen controls (UIBackgroundModes audio is in the Info.plist).
@MainActor
@Observable
final class MusicPlayer {
    private(set) var queue: [ItemSummary] = []
    private(set) var index = -1
    private(set) var playing = false
    private(set) var position: Int64 = 0
    private(set) var duration: Int64 = 0
    private(set) var shuffle = false
    private(set) var repeatMode: RepeatMode = .off

    var current: ItemSummary? { queue.indices.contains(index) ? queue[index] : nil }

    private let client: MediaClient
    private let player = AVPlayer()
    private var loadTask: Task<Void, Never>?
    private var endObserver: (any NSObjectProtocol)?
    private var timeObserver: Any?

    init(client: MediaClient) {
        self.client = client
        timeObserver = player.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.5, preferredTimescale: 600), queue: .main) { [weak self] _ in
            Task { @MainActor in self?.syncFromPlayer() }
        }
    }

    private func syncFromPlayer() {
        playing = player.timeControlStatus == .playing
        let seconds = player.currentTime().seconds
        if seconds.isFinite { position = Int64(seconds * 1000) }
        if let item = player.currentItem, item.duration.isNumeric, item.duration.seconds.isFinite, item.duration.seconds > 0 {
            duration = Int64(item.duration.seconds * 1000)
        }
        // Keep the lock screen's clock honest while a track plays.
        if current != nil { updateElapsed() }
    }

    // MARK: Queue

    /// Plays `tracks` from `start`; the stream URL of each track is requested when it starts.
    func play(_ tracks: [ItemSummary], start: Int = 0) {
        guard !tracks.isEmpty else { return }
        let from = min(max(0, start), tracks.count - 1)
        if shuffle {
            var rest = tracks
            let first = rest.remove(at: from)
            queue = [first] + rest.shuffled()
            index = 0
        } else {
            queue = tracks
            index = from
        }
        loadCurrent()
    }

    func setShuffleOn() { shuffle = true }

    func toggle() {
        guard current != nil else { return }
        if player.timeControlStatus == .playing { player.pause() } else { activateAudio(); player.play() }
        playing.toggle()
        updateElapsed()
    }

    func pause() {
        player.pause()
        playing = false
        updateElapsed()
    }

    func next() { advance(userInitiated: true) }

    func prev() {
        guard !queue.isEmpty else { return }
        if position > 3000 { seek(0); return }
        index = index > 0 ? index - 1 : (repeatMode == .all ? queue.count - 1 : 0)
        loadCurrent()
    }

    func seek(_ ms: Int64) {
        position = ms
        player.seek(to: CMTime(value: ms, timescale: 1000))
        updateElapsed()
    }

    func jump(_ i: Int) {
        guard queue.indices.contains(i) else { return }
        index = i
        loadCurrent()
    }

    func cycleRepeat() {
        switch repeatMode {
        case .off: repeatMode = .all
        case .all: repeatMode = .one
        case .one: repeatMode = .off
        }
    }

    func toggleShuffle() {
        shuffle.toggle()
        guard shuffle, let c = current else { return }
        var rest = queue
        rest.remove(at: index)
        queue = [c] + rest.shuffled()
        index = 0
    }

    func stop() {
        loadTask?.cancel()
        player.pause()
        player.replaceCurrentItem(with: nil)
        queue = []
        index = -1
        playing = false
        position = 0
        duration = 0
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
    }

    private func advance(userInitiated: Bool) {
        guard !queue.isEmpty else { return }
        if !userInitiated && repeatMode == .one {
            seek(0)
            activateAudio()
            player.play()
            return
        }
        if index + 1 < queue.count {
            index += 1
        } else if repeatMode == .all {
            index = 0
        } else if userInitiated {
            return
        } else {
            playing = false
            return
        }
        loadCurrent()
    }

    // MARK: Playback

    private func activateAudio() {
        do {
            try AVAudioSession.sharedInstance().setCategory(.playback, mode: .default)
            try AVAudioSession.sharedInstance().setActive(true)
        } catch {}
    }

    private func loadCurrent() {
        guard let track = current else { return }
        setupRemoteIfNeeded()
        activateAudio()
        loadTask?.cancel()
        position = 0
        duration = track.durationMs ?? 0
        updateNowPlaying(track, artwork: nil)
        loadTask = Task { [client] in
            guard let info = try? await client.startPlayback(item: track.id, profile: MediaClient.audioProfile),
                  let url = client.absolute(info.url), !Task.isCancelled, current?.id == track.id else { return }
            let item = AVPlayerItem(url: url)
            if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
            endObserver = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: item, queue: .main) { [weak self] _ in
                Task { @MainActor in self?.advance(userInitiated: false) }
            }
            player.replaceCurrentItem(with: item)
            player.play()
            playing = true
            await fetchArtwork(for: track)
        }
    }

    private func fetchArtwork(for track: ItemSummary) async {
        let id = track.hasPoster ? track.id : (track.parentId ?? track.id)
        guard let url = client.imageURL(item: id, type: "poster"),
              let image = await ImageCache.shared.image(url: url, token: client.token),
              current?.id == track.id else { return }
        updateNowPlaying(track, artwork: Self.artwork(from: image))
    }

    /// Nonisolated: the system calls the handler on a background queue.
    nonisolated private static func artwork(from image: UIImage) -> MPMediaItemArtwork {
        MPMediaItemArtwork(boundsSize: image.size) { _ in image }
    }

    // MARK: Now playing

    private func updateNowPlaying(_ track: ItemSummary, artwork: MPMediaItemArtwork?) {
        var info: [String: Any] = MPNowPlayingInfoCenter.default().nowPlayingInfo ?? [:]
        info[MPMediaItemPropertyTitle] = track.title
        info[MPMediaItemPropertyArtist] = track.artist ?? ""
        info[MPMediaItemPropertyAlbumTitle] = track.album ?? ""
        info[MPMediaItemPropertyPlaybackDuration] = Double(duration) / 1000
        info[MPNowPlayingInfoPropertyElapsedPlaybackTime] = Double(position) / 1000
        info[MPNowPlayingInfoPropertyPlaybackRate] = playing ? 1.0 : 0.0
        if let artwork { info[MPMediaItemPropertyArtwork] = artwork }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
    }

    private func updateElapsed() {
        guard var info = MPNowPlayingInfoCenter.default().nowPlayingInfo else { return }
        info[MPMediaItemPropertyPlaybackDuration] = Double(duration) / 1000
        info[MPNowPlayingInfoPropertyElapsedPlaybackTime] = Double(position) / 1000
        info[MPNowPlayingInfoPropertyPlaybackRate] = playing ? 1.0 : 0.0
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
    }

    /// The player the lock-screen commands act on; the handlers stay registered for the app's lifetime.
    private static weak var active: MusicPlayer?
    private static var remoteRegistered = false

    private func setupRemoteIfNeeded() {
        Self.active = self
        guard !Self.remoteRegistered else { return }
        Self.remoteRegistered = true
        Self.registerRemoteCommands()
    }

    /// Nonisolated: command handlers must not capture main-actor isolation.
    nonisolated private static func registerRemoteCommands() {
        let center = MPRemoteCommandCenter.shared()
        _ = center.playCommand.addTarget { _ in
            Task { @MainActor in if active?.playing == false { active?.toggle() } }
            return .success
        }
        _ = center.pauseCommand.addTarget { _ in
            Task { @MainActor in active?.pause() }
            return .success
        }
        _ = center.togglePlayPauseCommand.addTarget { _ in
            Task { @MainActor in active?.toggle() }
            return .success
        }
        _ = center.nextTrackCommand.addTarget { _ in
            Task { @MainActor in active?.next() }
            return .success
        }
        _ = center.previousTrackCommand.addTarget { _ in
            Task { @MainActor in active?.prev() }
            return .success
        }
        _ = center.changePlaybackPositionCommand.addTarget { event in
            guard let event = event as? MPChangePlaybackPositionCommandEvent else { return .commandFailed }
            let ms = Int64(event.positionTime * 1000)
            Task { @MainActor in active?.seek(ms) }
            return .success
        }
    }
}
