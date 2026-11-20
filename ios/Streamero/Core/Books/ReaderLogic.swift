import Foundation

/// Which formats the app reads itself; EPUB and MOBI open in the web app.
enum BookFormat {
    static let comics: Set<String> = ["cbz", "cbr", "cb7", "cbt"]

    static func isComic(_ format: String?) -> Bool { comics.contains((format ?? "").lowercased()) }
    static func isPDF(_ format: String?) -> Bool { (format ?? "").lowercased() == "pdf" }
    static func readsInApp(_ format: String?) -> Bool { isComic(format) || isPDF(format) }
}

/// Reading position math shared by the comic and PDF readers.
enum ReadingMath {
    /// Fraction read when `page` (zero-based) is on screen: the last page counts as all of it.
    static func progression(page: Int, pageCount: Int) -> Double {
        guard pageCount > 0 else { return 0 }
        let p = min(max(0, page), pageCount - 1)
        return Double(p + 1) / Double(pageCount)
    }

    static func isFinished(page: Int, pageCount: Int) -> Bool {
        pageCount > 0 && page >= pageCount - 1
    }

    /// The page to open at: the saved page, else one derived from the fraction; a finished
    /// book starts over.
    static func resumePage(_ progress: ReadingProgress?, pageCount: Int) -> Int {
        guard let progress, pageCount > 0, !progress.finished else { return 0 }
        if let page = progress.page { return min(max(0, page), pageCount - 1) }
        let derived = Int((progress.progression * Double(pageCount)).rounded()) - 1
        return min(max(0, derived), pageCount - 1)
    }

    /// The save body for a page.
    static func input(page: Int, pageCount: Int) -> ReadingProgressInput {
        ReadingProgressInput(page: page, progression: progression(page: page, pageCount: pageCount),
                             finished: isFinished(page: page, pageCount: pageCount) ? true : nil)
    }

    /// Progress to draw on a cover: books by fraction read, audiobooks by position; nil when
    /// nothing was started.
    static func coverProgress(_ item: ItemSummary) -> Double? {
        if item.kind == "audiobook" {
            guard let d = item.durationMs, d > 0, let p = item.positionMs, p > 0 else { return nil }
            return min(1, Double(p) / Double(d))
        }
        guard let value = item.progression, value > 0 else { return nil }
        return min(1, value)
    }
}

/// Maps reading pages to pager slots. The pager lays slots out left to right; read right to
/// left (manga), the first page sits in the rightmost slot so swiping toward the right advances.
struct ComicPaging: Equatable, Sendable {
    let pageCount: Int
    var rightToLeft: Bool

    enum Zone: Equatable, Sendable { case left, center, right }

    func slot(forPage page: Int) -> Int {
        guard pageCount > 0 else { return 0 }
        let p = min(max(0, page), pageCount - 1)
        return rightToLeft ? pageCount - 1 - p : p
    }

    /// The mapping is its own inverse.
    func page(forSlot slot: Int) -> Int { self.slot(forPage: slot) }

    /// Left and right thirds turn pages, the middle shows or hides the controls.
    static func zone(x: Double, width: Double) -> Zone {
        guard width > 0 else { return .center }
        if x < width / 3 { return .left }
        if x > width * 2 / 3 { return .right }
        return .center
    }

    /// The page after tapping a side, or nil when the tap does not turn (middle, past either end).
    func page(after zone: Zone, from page: Int) -> Int? {
        let step: Int
        switch zone {
        case .center: return nil
        case .left: step = rightToLeft ? 1 : -1
        case .right: step = rightToLeft ? -1 : 1
        }
        let next = page + step
        return (0..<pageCount).contains(next) ? next : nil
    }
}

/// Server timestamps ("2024-05-01T12:30:00Z", with or without fractions).
enum ServerDate {
    private static let formatters = ISODates()

    /// A server timestamp (RFC 3339, with or without fractions); also a plain "2024-05-01".
    static func parse(_ text: String?) -> Date? {
        guard let text, !text.isEmpty else { return nil }
        return formatters.parse(text)
    }

    /// Date (and time when the server sent one) in the device's time zone and the account's
    /// language; the raw text when it cannot be read.
    static func short(_ text: String, time: Bool = true, seconds: Bool = false, locale: Locale = Loc.locale) -> String {
        guard let date = parse(text) else { return String(text.prefix(time ? (seconds ? 19 : 16) : 10)).replacingOccurrences(of: "T", with: " ") }
        let dateOnly = text.count <= 10
        let clock: Date.FormatStyle.TimeStyle = time && !dateOnly ? (seconds ? .standard : .shortened) : .omitted
        var style = Date.FormatStyle(date: .abbreviated, time: clock).locale(locale)
        if dateOnly { style.timeZone = TimeZone(identifier: "UTC") ?? .current }
        return date.formatted(style)
    }

    /// Formatters are expensive to build; these are made once and used under a lock.
    private final class ISODates: @unchecked Sendable {
        // @unchecked: the formatters are only used under the lock.
        private let lock = NSLock()
        private let precise: ISO8601DateFormatter
        private let plain: ISO8601DateFormatter
        private let day: ISO8601DateFormatter

        init() {
            precise = ISO8601DateFormatter()
            precise.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            plain = ISO8601DateFormatter()
            plain.formatOptions = [.withInternetDateTime]
            day = ISO8601DateFormatter()
            day.formatOptions = [.withFullDate]
        }

        func parse(_ text: String) -> Date? {
            lock.withLock { precise.date(from: text) ?? plain.date(from: text) ?? day.date(from: text) }
        }
    }
}
