package net.scyde.streamero.ui

import kotlinx.coroutines.CancellationException
import net.scyde.streamero.api.ItemPage
import net.scyde.streamero.api.ItemSummary
import net.scyde.streamero.api.TrickplayInfo
import java.io.ByteArrayOutputStream
import java.io.InputStream

// Small pure helpers the screens share. Nothing here touches Android, so it is unit tested on the JVM.

/**
 * `runCatching` for coroutines: a cancelled call (the screen was left, the query changed) is
 * passed on instead of being reported as a failure, so it never shows up as an error line or
 * as an empty result.
 */
inline fun <T> attempt(block: () -> T): Result<T> = try {
    Result.success(block())
} catch (e: CancellationException) {
    throw e
} catch (e: Throwable) {
    Result.failure(e)
}

private const val UNRESERVED = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"

/**
 * Percent-encodes a value put into a navigation route, so names with `&`, `/`, `#`, `?` or
 * spaces arrive whole (the navigation library decodes arguments again).
 */
fun encodeRouteArg(value: String): String {
    val out = StringBuilder(value.length)
    for (b in value.toByteArray(Charsets.UTF_8)) {
        val c = (b.toInt() and 0xFF).toChar()
        if (c in UNRESERVED) out.append(c) else out.append('%').append("%02X".format(b.toInt() and 0xFF))
    }
    return out.toString()
}

/** The route of the player for an item, with the optional start, version and tracks. */
fun watchRoute(id: String, start: Long? = null, file: String? = null, audio: Int? = null, sub: Int? = null): String {
    val q = buildList {
        if (start != null) add("start=$start")
        if (file != null) add("file=" + encodeRouteArg(file))
        if (audio != null) add("audio=$audio")
        if (sub != null) add("sub=$sub")
    }.joinToString("&")
    val path = "watch/" + encodeRouteArg(id)
    return if (q.isEmpty()) path else "$path?$q"
}

/** The route of the titles of one author or series of a library. */
fun shelfRoute(libraryId: String, author: String? = null, series: String? = null): String {
    val q = listOfNotNull(author?.let { "author=" + encodeRouteArg(it) }, series?.let { "series=" + encodeRouteArg(it) }).joinToString("&")
    val path = "shelf/" + encodeRouteArg(libraryId)
    return if (q.isEmpty()) path else "$path?$q"
}

/**
 * Appends the next page of a list. An item already shown is left out: the library can change
 * between two pages (a scan adds a title and everything shifts by one), and a grid must never
 * get the same key twice.
 */
fun mergePage(shown: List<ItemSummary>, page: List<ItemSummary>): List<ItemSummary> {
    val seen = HashSet<String>(shown.size + page.size)
    val out = ArrayList<ItemSummary>(shown.size + page.size)
    for (i in shown) if (seen.add(i.id)) out.add(i)
    for (i in page) if (seen.add(i.id)) out.add(i)
    return out
}

/**
 * A list read from the server page by page. [offset] counts what the server has handed out,
 * which can differ from what is shown when [mergePage] left a repeated title out.
 */
data class Paging(val items: List<ItemSummary> = emptyList(), val offset: Int = 0, val total: Int = -1) {
    /** Loaded at least once and the server has more. */
    val hasMore: Boolean get() = total >= 0 && offset < total

    /** After [page] arrived: the first page replaces everything ([reset]), later ones are appended. */
    fun next(page: ItemPage, reset: Boolean): Paging {
        val base = if (reset) Paging() else this
        val offset = base.offset + page.items.size
        // An empty page ends the list even if the total said otherwise (titles removed meanwhile),
        // so the grid does not ask for the same page again and again.
        val total = if (page.items.isEmpty()) offset else page.total.coerceAtLeast(offset)
        return Paging(mergePage(base.items, page.items), offset, total)
    }
}

/** Where one frame is on the trickplay sheets: which sheet, and its column and row there. */
data class TrickCell(val sheet: Int, val column: Int, val row: Int)

/** The frame shown at [ms]; null when the server's numbers describe no usable sheet. */
fun trickCell(tp: TrickplayInfo, ms: Long): TrickCell? {
    if (tp.intervalMs <= 0 || tp.count <= 0 || tp.columns <= 0 || tp.rows <= 0) return null
    val idx = (ms.coerceAtLeast(0) / tp.intervalMs).coerceIn(0L, (tp.count - 1).toLong()).toInt()
    val per = tp.columns * tp.rows
    val local = idx % per
    return TrickCell(idx / per, local % tp.columns, local / tp.columns)
}

/** A progress fraction that is safe to lay out: 0 for anything that is not a number above 0. */
fun safeFraction(value: Float): Float = if (value > 0f) value.coerceAtMost(1f) else 0f

/** A file name made from a server id: only letters, digits, dashes and underscores survive. */
fun safeFileName(id: String): String = id.filter { it.isLetterOrDigit() || it == '-' || it == '_' }.take(80).ifEmpty { "file" }

private val PROVIDER_ID = Regex("^[A-Za-z0-9]{1,32}$")

/** The IMDb page of a title; null when the id the server has is not a plain IMDb id. */
fun imdbUrl(id: String?): String? = id?.trim()?.takeIf { PROVIDER_ID.matches(it) }?.let { "https://www.imdb.com/title/$it/" }

/** The TMDb page of a film or series ([kind] "series" is a TV show there). */
fun tmdbUrl(id: String?, kind: String): String? =
    id?.trim()?.takeIf { PROVIDER_ID.matches(it) }?.let { "https://www.themoviedb.org/${if (kind == "series") "tv" else "movie"}/$it" }

/** The largest picture the server takes as an avatar. */
const val MAX_AVATAR_BYTES = 8 shl 20

/** Reads the whole stream, or returns null as soon as it turns out to be larger than [max] bytes. */
fun readLimited(input: InputStream, max: Int): ByteArray? {
    val out = ByteArrayOutputStream()
    val buf = ByteArray(16 * 1024)
    while (true) {
        val n = input.read(buf)
        if (n < 0) break
        if (out.size() + n > max) return null
        out.write(buf, 0, n)
    }
    return out.toByteArray()
}
