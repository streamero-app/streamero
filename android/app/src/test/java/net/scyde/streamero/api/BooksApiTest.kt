package net.scyde.streamero.api

import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class BooksApiTest {
    private val server = MockWebServer()
    private lateinit var api: Api

    @Before fun start() { server.start(); api = Api(server.url("/").toString(), "tok") }
    @After fun stop() = server.shutdown()

    @Test fun decodesBookSummaryAndProgress() {
        val s = json.decodeFromString(ItemSummary.serializer(), """{"id":"1","kind":"book","title":"Dune","addedAt":"2024-01-01T00:00:00Z","hasPoster":true,
            "authors":["Frank Herbert"],"seriesName":"Dune","seriesIndex":1.5,"bookFormat":"epub","progression":0.42,"future":"ignored"}""")
        assertEquals(listOf("Frank Herbert"), s.authors)
        assertEquals(1.5, s.seriesIndex!!, 0.0)
        assertEquals(0.42f, s.progress, 1e-6f)
        val photo = json.decodeFromString(ItemSummary.serializer(), """{"id":"2","kind":"photoalbum","title":"Trip","addedAt":"","hasPoster":false,"childCount":12}""")
        assertEquals(12, photo.childCount)
        assertEquals(0f, photo.progress)
    }

    @Test fun decodesDetailWithBookAndPhoto() {
        val d = json.decodeFromString(ItemDetail.serializer(), """{"item":{"id":"1","kind":"photo","title":"IMG","addedAt":"","hasPoster":true},"libraryId":"L",
            "files":[],"children":[],"providerIds":{},"genres":[],"cast":[],"crew":[],"hasBackdrop":false,
            "book":{"format":"cbz","authors":["A"],"rightToLeft":true,"pageCount":20},
            "photo":{"camera":"X100V","fNumber":2.0,"iso":160,"latitude":45.8,"longitude":15.97}}""")
        assertTrue(d.book!!.rightToLeft)
        assertEquals(20, d.book!!.pageCount)
        assertEquals("X100V", d.photo!!.camera)
        assertEquals(160, d.photo!!.iso)
    }

    @Test fun opensAComicAndSavesProgress() = runTest {
        server.enqueue(MockResponse().setBody("""{"format":"cbz","fileUrl":"/stream/file/f/file.cbz?sig=x","pageCount":3,
            "pages":["/p/0?sig=a","/p/1?sig=b","/p/2?sig=c"],"rightToLeft":true,"progress":{"page":1,"progression":0.66,"finished":false}}"""))
        val m = api.openBook("b1")
        assertEquals("/api/v1/items/b1/read", server.takeRequest().path)
        assertEquals(3, m.pages.size)
        assertEquals(1, m.progress.page)
        assertTrue(api.resolve(m.pages[0]).startsWith("http"))

        server.enqueue(MockResponse().setResponseCode(204))
        api.saveReadingProgress("b1", ReadingProgressInput(progression = 1.0, page = 2))
        val r = server.takeRequest()
        assertEquals("PUT", r.method)
        assertEquals("/api/v1/items/b1/reading-progress", r.path)
        val body = r.body.readUtf8()
        assertTrue(body, "\"page\":2" in body)
        assertFalse(body, "locator" in body)
        assertFalse(body, "finished" in body)
    }

    @Test fun audiobookManifestAndNames() = runTest {
        server.enqueue(MockResponse().setBody("""{"durationMs":7200000,"positionMs":61000,"parts":[{"fileId":"f1","title":"01","offsetMs":0,"durationMs":3600000},
            {"fileId":"f2","title":"02","offsetMs":3600000,"durationMs":3600000}],"chapters":[{"title":"One","startMs":0,"endMs":3600000}]}"""))
        val m = api.listen("a1")
        assertEquals("/api/v1/items/a1/listen", server.takeRequest().path)
        assertEquals(2, m.parts.size)
        assertEquals(61000L, m.positionMs)

        server.enqueue(MockResponse().setBody("""[{"name":"Le Guin","count":4,"itemId":"x"}]"""))
        assertEquals("x", api.authors("L").single().itemId)
        assertEquals("/api/v1/libraries/L/authors", server.takeRequest().path)

        server.enqueue(MockResponse().setBody("""{"url":"/stream/photo/p?sig=1","mimeType":"image/jpeg","width":4000,"height":3000}"""))
        assertEquals("image/jpeg", api.photo("p").mimeType)
        assertEquals("/api/v1/items/p/photo", server.takeRequest().path)

        server.enqueue(MockResponse().setBody("[]"))
        assertTrue(api.continueReading().isEmpty())
        assertEquals("/api/v1/home/continue-reading", server.takeRequest().path)
    }
}
