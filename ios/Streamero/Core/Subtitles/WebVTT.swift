import Foundation

/// One timed line from a WebVTT file. AVPlayer cannot sideload subtitles for a plain media
/// URL, so the player fetches the server's VTT and draws the cues itself.
struct SubtitleCue: Equatable, Sendable {
    var startMs: Int64
    var endMs: Int64
    var text: String
}

enum WebVTT {
    /// "00:01:02.500" or "01:02.500" to milliseconds.
    static func parseTimestamp(_ s: String) -> Int64? {
        let parts = s.split(separator: ":").map(String.init)
        guard parts.count == 2 || parts.count == 3 else { return nil }
        let secParts = parts[parts.count - 1].split(separator: ".").map(String.init)
        guard let sec = Int64(secParts[0]) else { return nil }
        var ms: Int64 = 0
        if secParts.count > 1, let fraction = Int64(secParts[1].padding(toLength: 3, withPad: "0", startingAt: 0)) { ms = fraction }
        guard let min = Int64(parts[parts.count - 2]) else { return nil }
        let hours: Int64 = parts.count == 3 ? (Int64(parts[0]) ?? 0) : 0
        return (hours * 3600 + min * 60 + sec) * 1000 + ms
    }

    static func parse(_ text: String) -> [SubtitleCue] {
        var cues: [SubtitleCue] = []
        let normalized = text.replacingOccurrences(of: "\r\n", with: "\n").replacingOccurrences(of: "\r", with: "\n")
        for block in normalized.components(separatedBy: "\n\n") {
            let lines = block.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
            guard let timingIndex = lines.firstIndex(where: { $0.contains("-->") }) else { continue }
            let timing = lines[timingIndex].components(separatedBy: "-->")
            guard timing.count == 2 else { continue }
            let start = parseTimestamp(timing[0].trimmingCharacters(in: .whitespaces).components(separatedBy: " ")[0])
            let endField = timing[1].trimmingCharacters(in: .whitespaces).components(separatedBy: " ")[0]
            guard let start, let end = parseTimestamp(endField) else { continue }
            let body = lines[(timingIndex + 1)...].joined(separator: "\n")
            let clean = strippingTags(body).trimmingCharacters(in: .whitespacesAndNewlines)
            if !clean.isEmpty { cues.append(SubtitleCue(startMs: start, endMs: end, text: clean)) }
        }
        return cues
    }

    private static func strippingTags(_ s: String) -> String {
        var out = ""
        var inTag = false
        for ch in s {
            if ch == "<" { inTag = true } else if ch == ">" { inTag = false } else if !inTag { out.append(ch) }
        }
        return out
    }

    static func cue(in cues: [SubtitleCue], at ms: Int64) -> SubtitleCue? {
        cues.last { ms >= $0.startMs && ms < $0.endMs }
    }

    /// Where a subtitle file's clock starts in the film. The server may hand out WebVTT that is
    /// already shifted to a resumed stream (`offsetMs` in the signed URL); its cues then count from
    /// that position. The app does no shifting of its own, it only reads which clock the file uses.
    static func clockStart(of url: URL) -> Int64 {
        let items = URLComponents(url: url, resolvingAgainstBaseURL: true)?.queryItems ?? []
        return items.first { $0.name == "offsetMs" }?.value.flatMap { Int64($0) } ?? 0
    }
}
