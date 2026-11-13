import XCTest
@testable import Streamero

final class AudiobookTimelineTests: XCTestCase {
    /// Three files of 10 min, 20 min and 5 min; chapters: one per file, the second split in two.
    private let timeline = AudiobookTimeline(
        parts: [
            AudiobookPart(fileId: "b", title: "Part 2", offsetMs: 600_000, durationMs: 1_200_000),
            AudiobookPart(fileId: "a", title: "Part 1", offsetMs: 0, durationMs: 600_000),
            AudiobookPart(fileId: "c", title: "Part 3", offsetMs: 1_800_000, durationMs: 300_000),
        ],
        chapters: [
            AudiobookChapter(title: "One", startMs: 0, endMs: 600_000),
            AudiobookChapter(title: "Two", startMs: 600_000, endMs: 1_200_000),
            AudiobookChapter(title: "Three", startMs: 1_200_000, endMs: 0),
            AudiobookChapter(title: "Four", startMs: 1_800_000, endMs: 2_100_000),
        ],
        durationMs: 2_100_000)

    func testPartsAreSortedAndTotalKnown() {
        XCTAssertEqual(timeline.parts.map(\.fileId), ["a", "b", "c"])
        XCTAssertEqual(timeline.totalMs, 2_100_000)
        let derived = AudiobookTimeline(parts: timeline.parts, chapters: [], durationMs: 0)
        XCTAssertEqual(derived.totalMs, 2_100_000, "without a duration the files' end is used")
    }

    func testBookPositionToPartAndOffset() {
        XCTAssertEqual(timeline.locate(0), .init(part: 0, offsetMs: 0))
        XCTAssertEqual(timeline.locate(599_999), .init(part: 0, offsetMs: 599_999))
        XCTAssertEqual(timeline.locate(600_000), .init(part: 1, offsetMs: 0), "a boundary belongs to the next file")
        XCTAssertEqual(timeline.locate(1_000_000), .init(part: 1, offsetMs: 400_000))
        XCTAssertEqual(timeline.locate(2_000_000), .init(part: 2, offsetMs: 200_000))
        XCTAssertEqual(timeline.locate(-5), .init(part: 0, offsetMs: 0))
        XCTAssertEqual(timeline.locate(9_999_999), .init(part: 2, offsetMs: 300_000), "past the end clamps to the end")
        XCTAssertNil(AudiobookTimeline(parts: [], chapters: [], durationMs: 0).locate(0))
    }

    func testPartOffsetToBookPositionRoundTrips() {
        XCTAssertEqual(timeline.bookMs(part: 1, offsetMs: 400_000), 1_000_000)
        XCTAssertEqual(timeline.bookMs(part: 9, offsetMs: 1), 0)
        for ms: Int64 in [0, 1, 599_999, 600_000, 1_234_567, 2_099_999] {
            let loc = timeline.locate(ms)!
            XCTAssertEqual(timeline.bookMs(part: loc.part, offsetMs: loc.offsetMs), ms)
        }
    }

    func testChapterAtTime() {
        XCTAssertEqual(timeline.chapterIndex(at: 0), 0)
        XCTAssertEqual(timeline.chapterIndex(at: 700_000), 1)
        XCTAssertEqual(timeline.chapterIndex(at: 1_200_000), 2)
        XCTAssertEqual(timeline.chapterIndex(at: 2_050_000), 3)
        XCTAssertNil(AudiobookTimeline(parts: timeline.parts, chapters: [], durationMs: 0).chapterIndex(at: 5))
    }

    func testChapterEndFallsBackToNextStart() {
        XCTAssertEqual(timeline.chapterEnd(1), 1_200_000)
        XCTAssertEqual(timeline.chapterEnd(2), 1_800_000, "a missing end is the next chapter's start")
        XCTAssertEqual(timeline.chapterEnd(3), 2_100_000)
    }

    func testChapterNavigation() {
        XCTAssertEqual(timeline.nextChapterStart(from: 100), 600_000)
        XCTAssertNil(timeline.nextChapterStart(from: 2_000_000))
        XCTAssertEqual(timeline.previousChapterStart(from: 700_000), 600_000, "well into a chapter: back to its start")
        XCTAssertEqual(timeline.previousChapterStart(from: 601_000), 0, "just started: the chapter before")
        XCTAssertEqual(timeline.previousChapterStart(from: 1_000), 0)
    }

    func testResumeNearTheEndStartsOver() {
        XCTAssertEqual(timeline.resumePosition(1_000_000), 1_000_000)
        XCTAssertEqual(timeline.resumePosition(2_098_000), 0)
        XCTAssertEqual(timeline.resumePosition(-10), 0)
    }

    func testSleepAtEndOfChapter() {
        let now = Date(timeIntervalSince1970: 1_000)
        let plan = SleepTimer.endOfChapter.plan(now: now, positionMs: 1_300_000, timeline: timeline)
        XCTAssertEqual(plan.stopAtMs, 1_800_000)
        XCTAssertNil(plan.deadline)
        XCTAssertFalse(plan.isDue(now: now, positionMs: 1_799_000))
        XCTAssertTrue(plan.isDue(now: now, positionMs: 1_800_000))
    }

    func testSleepAfterMinutes() {
        let now = Date(timeIntervalSince1970: 1_000)
        let plan = SleepTimer.minutes(15).plan(now: now, positionMs: 0, timeline: timeline)
        XCTAssertEqual(plan.deadline, now.addingTimeInterval(900))
        XCTAssertFalse(plan.isDue(now: now.addingTimeInterval(899), positionMs: 9_999_999))
        XCTAssertTrue(plan.isDue(now: now.addingTimeInterval(900), positionMs: 0))
        XCTAssertEqual(SleepTimer.off.plan(now: now, positionMs: 0, timeline: timeline), SleepTimer.Plan.none)
        XCTAssertFalse(SleepTimer.Plan.none.isDue(now: now, positionMs: 1))
    }

    func testSleepWithoutChaptersStopsAtTheEnd() {
        let plain = AudiobookTimeline(parts: timeline.parts, chapters: [], durationMs: 0)
        XCTAssertEqual(plain.endOfChapter(at: 5), 2_100_000)
    }
}

final class ComicPagingTests: XCTestCase {
    func testLeftToRightKeepsOrder() {
        let paging = ComicPaging(pageCount: 5, rightToLeft: false)
        XCTAssertEqual((0..<5).map(paging.slot(forPage:)), [0, 1, 2, 3, 4])
    }

    func testRightToLeftMirrorsSlots() {
        let paging = ComicPaging(pageCount: 5, rightToLeft: true)
        XCTAssertEqual((0..<5).map(paging.slot(forPage:)), [4, 3, 2, 1, 0])
        for page in 0..<5 { XCTAssertEqual(paging.page(forSlot: paging.slot(forPage: page)), page) }
        XCTAssertEqual(paging.slot(forPage: 99), 0, "out of range clamps to the last page")
        XCTAssertEqual(ComicPaging(pageCount: 0, rightToLeft: true).slot(forPage: 3), 0)
    }

    func testTapZones() {
        XCTAssertEqual(ComicPaging.zone(x: 10, width: 300), .left)
        XCTAssertEqual(ComicPaging.zone(x: 150, width: 300), .center)
        XCTAssertEqual(ComicPaging.zone(x: 290, width: 300), .right)
        XCTAssertEqual(ComicPaging.zone(x: 10, width: 0), .center)
    }

    func testTapsTurnTheRightWay() {
        let ltr = ComicPaging(pageCount: 3, rightToLeft: false)
        XCTAssertEqual(ltr.page(after: .right, from: 0), 1)
        XCTAssertEqual(ltr.page(after: .left, from: 1), 0)
        XCTAssertNil(ltr.page(after: .left, from: 0))
        XCTAssertNil(ltr.page(after: .right, from: 2))
        XCTAssertNil(ltr.page(after: .center, from: 1))
        let rtl = ComicPaging(pageCount: 3, rightToLeft: true)
        XCTAssertEqual(rtl.page(after: .left, from: 0), 1, "manga: the left side advances")
        XCTAssertEqual(rtl.page(after: .right, from: 1), 0)
        XCTAssertNil(rtl.page(after: .left, from: 2))
    }
}

final class ReadingMathTests: XCTestCase {
    func testProgression() {
        XCTAssertEqual(ReadingMath.progression(page: 0, pageCount: 4), 0.25)
        XCTAssertEqual(ReadingMath.progression(page: 3, pageCount: 4), 1)
        XCTAssertEqual(ReadingMath.progression(page: 10, pageCount: 4), 1)
        XCTAssertEqual(ReadingMath.progression(page: 0, pageCount: 0), 0)
        XCTAssertTrue(ReadingMath.isFinished(page: 3, pageCount: 4))
        XCTAssertFalse(ReadingMath.isFinished(page: 2, pageCount: 4))
    }

    func testSaveBodyMarksTheLastPageFinished() throws {
        let last = ReadingMath.input(page: 9, pageCount: 10)
        XCTAssertEqual(last.finished, true)
        XCTAssertEqual(last.progression, 1)
        let middle = ReadingMath.input(page: 4, pageCount: 10)
        XCTAssertNil(middle.finished)
        let body = try XCTUnwrap(JSONSerialization.jsonObject(with: MediaClient.encode(middle)) as? [String: Any])
        XCTAssertEqual(body["page"] as? Int, 4)
        XCTAssertEqual(body["progression"] as? Double, 0.5)
        XCTAssertNil(body["finished"])
    }

    func testResumePage() {
        XCTAssertEqual(ReadingMath.resumePage(nil, pageCount: 10), 0)
        XCTAssertEqual(ReadingMath.resumePage(ReadingProgress(page: 7, progression: 0.8, finished: false), pageCount: 10), 7)
        XCTAssertEqual(ReadingMath.resumePage(ReadingProgress(page: 70, progression: 0.8, finished: false), pageCount: 10), 9)
        XCTAssertEqual(ReadingMath.resumePage(ReadingProgress(progression: 0.5, finished: false), pageCount: 10), 4)
        XCTAssertEqual(ReadingMath.resumePage(ReadingProgress(page: 9, progression: 1, finished: true), pageCount: 10), 0, "finished books start over")
    }

    func testCoverProgress() {
        var book = ItemSummary(id: "1", kind: "book", title: "B", addedAt: "", hasPoster: false)
        XCTAssertNil(ReadingMath.coverProgress(book))
        book.progression = 0.4
        XCTAssertEqual(ReadingMath.coverProgress(book), 0.4)
        var audio = ItemSummary(id: "2", kind: "audiobook", title: "A", addedAt: "", hasPoster: false)
        audio.durationMs = 1000
        audio.positionMs = 250
        audio.progression = 0.9
        XCTAssertEqual(ReadingMath.coverProgress(audio), 0.25, "audiobooks use the listening position")
    }

    func testFormats() {
        XCTAssertTrue(BookFormat.readsInApp("CBZ"))
        XCTAssertTrue(BookFormat.readsInApp("pdf"))
        XCTAssertFalse(BookFormat.readsInApp("epub"))
        XCTAssertFalse(BookFormat.readsInApp(nil))
    }

    func testSeriesLabel() {
        XCTAssertEqual(seriesLabel("Saga", 2), "Saga · #2")
        XCTAssertEqual(seriesLabel("Saga", 2.5), "Saga · #2.5")
        XCTAssertEqual(seriesLabel("Saga", nil), "Saga")
    }

    func testServerDates() {
        XCTAssertNotNil(ServerDate.parse("2024-05-01T12:30:00Z"))
        XCTAssertNotNil(ServerDate.parse("2024-05-01T12:30:00.123+02:00"))
        XCTAssertNil(ServerDate.parse("yesterday"))
        XCTAssertNil(ServerDate.parse(nil))
    }

    func testNewSummaryFieldsDecode() throws {
        let json = """
        {"id":"1","kind":"book","title":"T","addedAt":"x","hasPoster":true,"authors":["A","B"],
         "seriesName":"S","seriesIndex":1.5,"bookFormat":"cbz","progression":0.3,"childCount":4}
        """
        let item = try JSONDecoder().decode(ItemSummary.self, from: Data(json.utf8))
        XCTAssertEqual(item.authors, ["A", "B"])
        XCTAssertEqual(item.seriesIndex, 1.5)
        XCTAssertEqual(item.bookFormat, "cbz")
        XCTAssertEqual(item.childCount, 4)
    }
}
