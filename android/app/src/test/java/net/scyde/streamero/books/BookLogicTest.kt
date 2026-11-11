package net.scyde.streamero.books

import net.scyde.streamero.api.AudiobookChapter
import net.scyde.streamero.api.AudiobookPart
import net.scyde.streamero.ui.homeOrder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class BookLogicTest {
    private val parts = listOf(
        AudiobookPart("a", "Part 1", 0, 60_000),
        AudiobookPart("b", "Part 2", 60_000, 120_000),
        AudiobookPart("c", "Part 3", 180_000, 20_000),
    )
    private val chapters = listOf(
        AudiobookChapter("One", 0, 50_000),
        AudiobookChapter("Two", 50_000, 150_000),
        AudiobookChapter("Three", 150_000, 200_000),
    )
    private val t = AudiobookTimeline(parts, chapters, 200_000)

    @Test fun mapsBookPositionsToPartsAndBack() {
        assertEquals(0 to 0L, t.locate(0))
        assertEquals(0 to 59_999L, t.locate(59_999))
        assertEquals(1 to 0L, t.locate(60_000))
        assertEquals(1 to 100_000L, t.locate(160_000))
        assertEquals(2 to 20_000L, t.locate(999_999)) // clamped to the end
        assertEquals(0 to 0L, t.locate(-5))
        assertEquals(160_000L, t.global(1, 100_000))
        assertEquals(185_000L, t.global(2, 5_000))
        for (ms in listOf(0L, 12_345L, 60_000L, 179_999L, 190_000L)) {
            val (i, local) = t.locate(ms)
            assertEquals(ms, t.global(i, local))
        }
    }

    @Test fun durationFallsBackToTheParts() {
        assertEquals(200_000L, AudiobookTimeline(parts, emptyList(), 0).durationMs)
    }

    @Test fun findsTheChapterAndItsEnd() {
        assertEquals(0, t.chapterAt(0))
        assertEquals(0, t.chapterAt(49_999))
        assertEquals(1, t.chapterAt(50_000))
        assertEquals(2, t.chapterAt(199_000))
        assertEquals(150_000L, t.chapterEnd(70_000))
        val none = AudiobookTimeline(parts, emptyList(), 200_000)
        assertEquals(-1, none.chapterAt(10))
        assertEquals(200_000L, none.chapterEnd(10))
        // A chapter without an end runs to the next one.
        val open = AudiobookTimeline(parts, listOf(AudiobookChapter("A", 0, 0), AudiobookChapter("B", 90_000, 0)), 200_000)
        assertEquals(90_000L, open.chapterEnd(10_000))
        assertEquals(200_000L, open.chapterEnd(100_000))
    }

    @Test fun sleepTimerLength() {
        assertEquals(15 * 60_000L, sleepAfterMs(15, t, 0, 1f))
        assertEquals(80_000L, sleepAfterMs(SLEEP_END_OF_CHAPTER, t, 70_000, 1f))
        assertEquals(40_000L, sleepAfterMs(SLEEP_END_OF_CHAPTER, t, 70_000, 2f))
    }

    @Test fun progressionAndResume() {
        assertEquals(0.0, progressionOf(0, 0), 0.0)
        assertEquals(0.1, progressionOf(0, 10), 1e-9)
        assertEquals(1.0, progressionOf(9, 10), 1e-9)
        assertEquals(1.0, progressionOf(20, 10), 1e-9)
        assertEquals(0, resumePage(null, 10))
        assertEquals(4, resumePage(4, 10))
        assertEquals(9, resumePage(42, 10))
        assertEquals(0, resumePage(3, 0))
    }

    @Test fun tapsTurnPagesTheWayTheBookReads() {
        assertEquals(TapZone.Left, tapZone(10f, 1000f))
        assertEquals(TapZone.Middle, tapZone(500f, 1000f))
        assertEquals(TapZone.Right, tapZone(900f, 1000f))
        // Left to right: the right edge goes forward.
        assertEquals(6, pageAfterTap(TapZone.Right, false, 5, 10))
        assertEquals(4, pageAfterTap(TapZone.Left, false, 5, 10))
        // Right to left (manga): the left edge goes forward.
        assertEquals(6, pageAfterTap(TapZone.Left, true, 5, 10))
        assertEquals(4, pageAfterTap(TapZone.Right, true, 5, 10))
        assertNull(pageAfterTap(TapZone.Middle, true, 5, 10))
        assertEquals(9, pageAfterTap(TapZone.Right, false, 9, 10))
        assertEquals(0, pageAfterTap(TapZone.Left, false, 0, 10))
    }

    @Test fun formats() {
        assertTrue(readableInApp("CBZ"))
        assertTrue(readableInApp("pdf"))
        assertFalse(readableInApp("epub"))
        assertFalse(readableInApp(null))
        assertEquals("3", formatIndex(3.0))
        assertEquals("2.5", formatIndex(2.5))
        assertEquals("f/1.8 · 1/250 s · ISO 100 · 50 mm", exposureLine(1.8, "1/250", 100, 50.0))
        assertEquals("", exposureLine(null, null, null, null))
    }

    @Test fun readingRowFollowsContinueWatching() {
        assertEquals(listOf("continue", "reading", "nextUp", "latest", "favorites", "collections", "libraries"), homeOrder(null))
        assertEquals(listOf("latest", "continue", "reading"), homeOrder(listOf("latest", "continue")))
        assertEquals(listOf("reading", "latest"), homeOrder(listOf("latest")))
        assertEquals(listOf("reading", "latest"), homeOrder(listOf("reading", "latest")))
    }
}
