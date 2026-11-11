import Foundation

/// One timeline over an audiobook's files: positions in the whole book map to a file and an
/// offset in it, and chapters are looked up by book position. Pure so it can be tested.
struct AudiobookTimeline: Equatable, Sendable {
    let parts: [AudiobookPart]
    let chapters: [AudiobookChapter]
    let totalMs: Int64

    struct Location: Equatable, Sendable {
        let part: Int
        let offsetMs: Int64
    }

    init(parts: [AudiobookPart], chapters: [AudiobookChapter], durationMs: Int64) {
        self.parts = parts.sorted { $0.offsetMs < $1.offsetMs }
        self.chapters = chapters.sorted { $0.startMs < $1.startMs }
        let end = self.parts.last.map { $0.offsetMs + $0.durationMs } ?? 0
        totalMs = durationMs > 0 ? durationMs : end
    }

    init(_ manifest: AudiobookManifest) {
        self.init(parts: manifest.parts, chapters: manifest.chapters, durationMs: manifest.durationMs)
    }

    private func clamp(_ ms: Int64) -> Int64 { min(max(0, ms), totalMs) }

    /// The file playing at a book position and the offset in it.
    func locate(_ ms: Int64) -> Location? {
        guard !parts.isEmpty else { return nil }
        let t = clamp(ms)
        let index = parts.lastIndex { $0.offsetMs <= t } ?? 0
        let part = parts[index]
        return Location(part: index, offsetMs: min(max(0, t - part.offsetMs), max(0, part.durationMs)))
    }

    /// The book position of an offset in a file.
    func bookMs(part: Int, offsetMs: Int64) -> Int64 {
        guard parts.indices.contains(part) else { return 0 }
        return clamp(parts[part].offsetMs + max(0, offsetMs))
    }

    func chapterIndex(at ms: Int64) -> Int? {
        guard !chapters.isEmpty else { return nil }
        return chapters.lastIndex { $0.startMs <= ms } ?? 0
    }

    /// Where a chapter ends: its own end, else the next chapter's start, else the book's end.
    func chapterEnd(_ index: Int) -> Int64 {
        guard chapters.indices.contains(index) else { return totalMs }
        let c = chapters[index]
        if c.endMs > c.startMs { return min(c.endMs, totalMs) }
        if chapters.indices.contains(index + 1) { return chapters[index + 1].startMs }
        return totalMs
    }

    /// The stop position for "sleep at the end of this chapter" (the book's end without chapters).
    func endOfChapter(at ms: Int64) -> Int64 {
        guard let i = chapterIndex(at: ms) else { return totalMs }
        return chapterEnd(i)
    }

    /// Previous chapter: back to the start of this one unless it began a moment ago.
    func previousChapterStart(from ms: Int64, grace: Int64 = 3000) -> Int64 {
        guard let i = chapterIndex(at: ms) else { return 0 }
        let start = chapters[i].startMs
        if ms - start > grace || i == 0 { return start }
        return chapters[i - 1].startMs
    }

    /// Next chapter's start, or nil in the last one.
    func nextChapterStart(from ms: Int64) -> Int64? {
        guard let i = chapterIndex(at: ms), chapters.indices.contains(i + 1) else { return nil }
        return chapters[i + 1].startMs
    }

    /// Saved positions within the last few seconds start the book over.
    func resumePosition(_ saved: Int64) -> Int64 {
        let t = clamp(saved)
        return totalMs > 0 && totalMs - t < 5000 ? 0 : t
    }
}

/// Sleep timer choices.
enum SleepTimer: Equatable, Sendable {
    case off
    case minutes(Int)
    case endOfChapter

    /// When playback should stop: at a wall-clock deadline, at a book position, or both nil.
    struct Plan: Equatable, Sendable {
        var deadline: Date?
        var stopAtMs: Int64?

        static let none = Plan()

        func isDue(now: Date, positionMs: Int64) -> Bool {
            if let deadline, now >= deadline { return true }
            if let stopAtMs, positionMs >= stopAtMs { return true }
            return false
        }
    }

    func plan(now: Date, positionMs: Int64, timeline: AudiobookTimeline) -> Plan {
        switch self {
        case .off: return .none
        case .minutes(let m): return Plan(deadline: now.addingTimeInterval(TimeInterval(max(0, m) * 60)))
        case .endOfChapter: return Plan(stopAtMs: timeline.endOfChapter(at: positionMs))
        }
    }
}
