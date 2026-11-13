import CoreGraphics
import Foundation

/// Subtitle choices in the player. Every choice is an index into the file's subtitle tracks
/// (the same numbering the item page and the playback request use); -1 means none.
/// Text tracks arrive as WebVTT and are drawn by the app; bitmap tracks are burned in by the server.
enum SubtitleTracks {
    /// The track this stream shows: the burned-in one, else the server's default text track.
    static func current(in info: PlaybackInfo) -> Int {
        if let burned = info.burnedSubtitle, burned >= 0 { return burned }
        if let pos = info.subtitleDefault, info.subtitles.indices.contains(pos) { return info.subtitles[pos].index }
        return -1
    }

    /// The WebVTT track for a file subtitle index, if it is a text track.
    static func text(_ index: Int, in info: PlaybackInfo) -> PlaybackSubtitle? {
        info.subtitles.first { $0.index == index }
    }

    static func isBitmap(_ index: Int, in info: PlaybackInfo) -> Bool {
        info.bitmapSubtitles?.contains { $0.index == index } == true
    }

    /// Whether choosing `index` needs a new stream: a bitmap track has to be burned in, and a
    /// burned-in track can only be removed by restarting without it.
    static func needsReload(choosing index: Int, in info: PlaybackInfo) -> Bool {
        let burned = info.burnedSubtitle ?? -1
        if index == burned { return false }
        return burned >= 0 || isBitmap(index, in: info)
    }
}

/// When the player tells the server where it is: every 10 s of playback, and right away
/// after a pause or a seek (a jump the clock cannot explain).
struct ProgressTrigger {
    var interval = 10
    /// A jump larger than this (beyond normal playback) counts as a seek.
    var seekToleranceMs: Int64 = 2500
    private var wasPlaying = false
    private var lastPosition: Int64?
    private var playingTicks = 0

    init(interval: Int = 10) { self.interval = interval }

    /// One call per second; true when a report is due.
    mutating func tick(playing: Bool, positionMs: Int64) -> Bool {
        defer {
            wasPlaying = playing
            lastPosition = positionMs
        }
        var due = false
        if let last = lastPosition {
            let expected = last + (wasPlaying ? 1000 : 0)
            if abs(positionMs - expected) > seekToleranceMs { due = true }
        }
        if wasPlaying && !playing { due = true }
        if playing {
            playingTicks += 1
            if playingTicks >= interval { due = true }
        }
        if due { playingTicks = 0 }
        return due
    }
}

/// How a tap on the video affects the chrome drawn over AVKit's controls. AVKit does not
/// say when its controls show, so the app mirrors them: a tap on open video toggles them,
/// a tap on AVKit's buttons (centre transport, bottom bar) keeps them up.
enum ControlsTap {
    enum Action: Equatable { case toggle, keep }

    static func action(at point: CGPoint, in size: CGSize) -> Action {
        guard size.width > 0, size.height > 0 else { return .toggle }
        if point.y > size.height - 130 || point.y < 110 { return .keep }
        let center = CGRect(x: size.width / 2 - 130, y: size.height / 2 - 60, width: 260, height: 120)
        return center.contains(point) ? .keep : .toggle
    }
}
