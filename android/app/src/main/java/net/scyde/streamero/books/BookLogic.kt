package net.scyde.streamero.books

import net.scyde.streamero.api.AudiobookChapter
import net.scyde.streamero.api.AudiobookPart

/** Formats read page by page from images the server serves. */
val COMIC_FORMATS = setOf("cbz", "cbr", "cb7", "cbt")

/** Formats the app can read itself; EPUB, MOBI and AZW3 are read in the web app. */
fun readableInApp(format: String?): Boolean = format != null && (format.lowercase() in COMIC_FORMATS || format.lowercase() == "pdf")

/** The fraction read when [page] (0-based) of [count] pages is on screen; the last page is all of it. */
fun progressionOf(page: Int, count: Int): Double = if (count <= 0) 0.0 else ((page + 1).toDouble() / count).coerceIn(0.0, 1.0)

/** Where to open a book: the saved page, kept inside the book. */
fun resumePage(saved: Int?, count: Int): Int = if (count <= 0) 0 else (saved ?: 0).coerceIn(0, count - 1)

/** A tap on the left, middle or right of a reader page. */
enum class TapZone { Left, Middle, Right }

fun tapZone(x: Float, width: Float): TapZone = when {
    width <= 0f -> TapZone.Middle
    x < width * 0.3f -> TapZone.Left
    x > width * 0.7f -> TapZone.Right
    else -> TapZone.Middle
}

/**
 * The page a tap turns to, or null for the middle (show or hide the controls). Left to right,
 * the right edge goes forward; right to left (manga) the left edge does.
 */
fun pageAfterTap(zone: TapZone, rightToLeft: Boolean, page: Int, count: Int): Int? {
    val forward = when (zone) {
        TapZone.Middle -> return null
        TapZone.Right -> !rightToLeft
        TapZone.Left -> rightToLeft
    }
    return (if (forward) page + 1 else page - 1).coerceIn(0, (count - 1).coerceAtLeast(0))
}

/**
 * The audio files of an audiobook laid out on one timeline: the player plays the parts as a
 * queue, the screens and the server speak in positions in the whole book.
 */
class AudiobookTimeline(val parts: List<AudiobookPart>, val chapters: List<AudiobookChapter>, durationMs: Long) {
    val durationMs: Long = if (durationMs > 0) durationMs else parts.lastOrNull()?.let { it.offsetMs + it.durationMs } ?: 0L

    /** The book position of [local] ms into part [index]. */
    fun global(index: Int, local: Long): Long {
        val p = parts.getOrNull(index) ?: return 0L
        return (p.offsetMs + local.coerceAtLeast(0)).coerceIn(0, durationMs)
    }

    /** The part that plays at book position [ms] and the offset in it. */
    fun locate(ms: Long): Pair<Int, Long> {
        if (parts.isEmpty()) return 0 to 0L
        val pos = ms.coerceIn(0, durationMs)
        val i = parts.indexOfLast { it.offsetMs <= pos }.coerceAtLeast(0)
        return i to (pos - parts[i].offsetMs).coerceIn(0, parts[i].durationMs.coerceAtLeast(0))
    }

    /** The index of the chapter at [ms], -1 when there are no chapters. */
    fun chapterAt(ms: Long): Int {
        if (chapters.isEmpty()) return -1
        val i = chapters.indexOfLast { it.startMs <= ms }
        return i.coerceAtLeast(0)
    }

    /** Where the chapter playing at [ms] ends (the end of the book without chapters). */
    fun chapterEnd(ms: Long): Long {
        val c = chapters.getOrNull(chapterAt(ms)) ?: return durationMs
        val next = chapters.getOrNull(chapterAt(ms) + 1)?.startMs
        val end = if (c.endMs > c.startMs) c.endMs else next ?: durationMs
        return end.coerceAtMost(durationMs)
    }
}

/** A sleep timer choice: a number of minutes, or the end of the current chapter (0). */
const val SLEEP_END_OF_CHAPTER = 0
val SLEEP_CHOICES = listOf(15, 30, 45, 60, SLEEP_END_OF_CHAPTER)

/** Playback speeds offered for audiobooks. */
val SPEEDS = listOf(0.75f, 1.0f, 1.25f, 1.5f, 1.75f, 2.0f)

/** A position in a series: "3", or "2.5" for an in-between volume. */
fun formatIndex(i: Double): String = if (i == Math.floor(i) && !i.isInfinite()) i.toLong().toString() else i.toString()

/** Aperture, shutter, ISO and focal length on one line ("f/1.8 · 1/250 s · ISO 100 · 50 mm"). */
fun exposureLine(fNumber: Double?, exposureTime: String?, iso: Int?, focalLength: Double?): String = listOfNotNull(
    fNumber?.let { "f/" + formatIndex(Math.round(it * 10) / 10.0) },
    exposureTime?.takeIf { it.isNotBlank() }?.let { "$it s" },
    iso?.let { "ISO $it" },
    focalLength?.let { formatIndex(Math.round(it * 10) / 10.0) + " mm" },
).joinToString(" · ")

/** How long a sleep timer of [minutes] (or to the end of the chapter, at [speed]) lets the book play. */
fun sleepAfterMs(minutes: Int, timeline: AudiobookTimeline, positionMs: Long, speed: Float): Long =
    if (minutes == SLEEP_END_OF_CHAPTER) ((timeline.chapterEnd(positionMs) - positionMs).coerceAtLeast(0) / speed.coerceAtLeast(0.1f)).toLong()
    else minutes * 60_000L
