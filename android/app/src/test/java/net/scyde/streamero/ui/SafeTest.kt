package net.scyde.streamero.ui

import kotlinx.coroutines.CancellationException
import net.scyde.streamero.api.ItemPage
import net.scyde.streamero.api.ItemSummary
import net.scyde.streamero.api.TrickplayInfo
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.io.ByteArrayInputStream

class SafeTest {
    private fun item(id: String) = ItemSummary(id = id, kind = "movie", title = id)
    private fun page(vararg ids: String, total: Int) = ItemPage(ids.map(::item), total)

    @Test fun attemptPassesCancellationOn() {
        try {
            attempt { throw CancellationException("left") }
            fail("a cancelled call must not become a failure")
        } catch (e: CancellationException) {
            assertEquals("left", e.message)
        }
    }

    @Test fun attemptKeepsValuesAndFailures() {
        assertEquals(3, attempt { 1 + 2 }.getOrNull())
        val r = attempt { throw java.io.IOException("offline") }
        assertTrue(r.exceptionOrNull() is java.io.IOException)
    }

    @Test fun routeArgumentsAreEncoded() {
        assertEquals("a%26b%2Fc%23d%3Fe%20f", encodeRouteArg("a&b/c#d?e f"))
        assertEquals("%C5%A0ime%20%26%20sin", encodeRouteArg("Šime & sin"))
        assertEquals("Tolkien_J.R.R-1~", encodeRouteArg("Tolkien_J.R.R-1~"))
        assertEquals("100%25", encodeRouteArg("100%"))
    }

    @Test fun watchRouteKeepsEveryPart() {
        assertEquals("watch/abc", watchRoute("abc"))
        assertEquals("watch/abc?start=0", watchRoute("abc", start = 0))
        assertEquals("watch/abc?start=5&file=f%26x%3Fy&audio=1&sub=-1", watchRoute("abc", 5, "f&x?y", 1, -1))
        assertEquals("watch/abc?sub=2", watchRoute("abc", sub = 2))
    }

    @Test fun shelfRouteEncodesNames() {
        assertEquals("shelf/lib", shelfRoute("lib"))
        assertEquals("shelf/lib?author=Ivana%20Bro%C5%BEi%C4%87-Ma%C5%BEurani%C4%87", shelfRoute("lib", author = "Ivana Brožić-Mažuranić"))
        assertEquals("shelf/lib?series=Dune%20%2F%20Part%20%231%20%26%20more%3F", shelfRoute("lib", series = "Dune / Part #1 & more?"))
        assertEquals("shelf/lib?author=a&series=b", shelfRoute("lib", "a", "b"))
    }

    @Test fun mergePageLeavesRepeatsOut() {
        val merged = mergePage(listOf(item("a"), item("b")), listOf(item("b"), item("c"), item("c")))
        assertEquals(listOf("a", "b", "c"), merged.map { it.id })
        assertEquals(listOf("x"), mergePage(listOf(item("x"), item("x")), emptyList()).map { it.id })
    }

    @Test fun pagingCountsWhatTheServerHandedOut() {
        val first = Paging().next(page("a", "b", total = 5), reset = true)
        assertEquals(2, first.offset); assertTrue(first.hasMore)
        // The library shifted by one: "b" comes again and is shown once, but the server offset still moves on.
        val second = first.next(page("b", "c", total = 5), reset = false)
        assertEquals(listOf("a", "b", "c"), second.items.map { it.id })
        assertEquals(4, second.offset); assertTrue(second.hasMore)
        val third = second.next(page("d", total = 5), reset = false)
        assertEquals(5, third.offset); assertFalse(third.hasMore)
    }

    @Test fun pagingStopsOnAnEmptyPage() {
        val p = Paging().next(page("a", total = 10), reset = true).next(page(total = 10), reset = false)
        assertEquals(1, p.total)
        assertFalse(p.hasMore)
    }

    @Test fun pagingResetStartsOver() {
        val old = Paging().next(page("a", "b", total = 2), reset = true)
        val fresh = old.next(page("z", total = 1), reset = true)
        assertEquals(listOf("z"), fresh.items.map { it.id }); assertEquals(1, fresh.offset)
        assertFalse(Paging().hasMore)
        assertEquals(0, Paging().next(page(total = 0), reset = true).total)
    }

    @Test fun trickCellFindsTheFrame() {
        val tp = TrickplayInfo(intervalMs = 10_000, width = 320, height = 180, columns = 10, rows = 10, count = 250, sheetUrls = listOf("a", "b", "c"))
        assertEquals(TrickCell(0, 0, 0), trickCell(tp, 0))
        assertEquals(TrickCell(0, 2, 1), trickCell(tp, 125_000))
        assertEquals(TrickCell(1, 0, 0), trickCell(tp, 1_000_000))
        // Past the end and before the start stay on the last and first frame.
        assertEquals(TrickCell(2, 9, 4), trickCell(tp, 99_000_000))
        assertEquals(TrickCell(0, 0, 0), trickCell(tp, -5))
    }

    @Test fun trickCellRefusesBrokenNumbers() {
        val ok = TrickplayInfo(10_000, 320, 180, 10, 10, 100, listOf("a"))
        assertNull(trickCell(ok.copy(intervalMs = 0), 5_000))
        assertNull(trickCell(ok.copy(count = 0), 5_000))
        assertNull(trickCell(ok.copy(columns = 0), 5_000))
        assertNull(trickCell(ok.copy(rows = -1), 5_000))
    }

    @Test fun safeFractionIsAlwaysLayable() {
        assertEquals(0f, safeFraction(Float.NaN))
        assertEquals(0f, safeFraction(-0.5f))
        assertEquals(1f, safeFraction(3f))
        assertEquals(1f, safeFraction(Float.POSITIVE_INFINITY))
        assertEquals(0.25f, safeFraction(0.25f))
    }

    @Test fun safeFileNameKeepsOnlyAPlainName() {
        assertEquals("etcpasswd", safeFileName("../../etc/passwd"))
        assertEquals("3f2a-91_b", safeFileName("3f2a-91_b"))
        assertEquals("file", safeFileName("/../"))
        assertEquals(80, safeFileName("a".repeat(200)).length)
    }

    @Test fun readLimitedStopsAtTheLimit() {
        val bytes = ByteArray(40_000) { it.toByte() }
        assertArrayEquals(bytes, readLimited(ByteArrayInputStream(bytes), 40_000))
        assertNull(readLimited(ByteArrayInputStream(bytes), 39_999))
        assertArrayEquals(ByteArray(0), readLimited(ByteArrayInputStream(ByteArray(0)), 10))
    }

    @Test fun missingBrowserHasItsOwnMessage() {
        assertEquals(net.scyde.streamero.R.string.sso_no_browser, errorMessageRes("no_browser"))
        assertNull(errorMessageRes("something_new"))
    }

    @Test fun providerLinksOnlyForPlainIds() {
        assertEquals("https://www.imdb.com/title/tt0133093/", imdbUrl("tt0133093"))
        assertEquals("https://www.themoviedb.org/tv/1399", tmdbUrl("1399", "series"))
        assertEquals("https://www.themoviedb.org/movie/603", tmdbUrl(" 603 ", "movie"))
        assertNull(imdbUrl(null))
        assertNull(imdbUrl("tt1/../../evil"))
        assertNull(tmdbUrl("1?x=1", "movie"))
        assertNull(imdbUrl(""))
    }
}
