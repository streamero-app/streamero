package net.scyde.streamero.manage

import kotlinx.coroutines.test.runTest
import net.scyde.streamero.api.Api
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test

class ArrClientTest {
    private val server = MockWebServer()
    private lateinit var api: Api

    @Before fun start() { server.start(); api = Api(server.url("/").toString(), "tok") }
    @After fun stop() = server.shutdown()

    private suspend fun failure(block: suspend () -> Unit): ArrException {
        try { block() } catch (e: ArrException) { return e }
        fail("expected an ArrException"); throw IllegalStateException()
    }

    @Test fun buildsProxyUrlsPerService() {
        assertEquals("/api/v1/arr/radarr/proxy/api/v3/movie/12", ArrClient(api, ArrService.Radarr).url("movie/12").encodedPath)
        assertEquals("/api/v1/arr/lidarr/proxy/api/v1/artist", ArrClient(api, ArrService.Lidarr).url("artist").encodedPath)
        assertEquals("/api/v1/arr/bazarr/proxy/api/movies/wanted", ArrClient(api, ArrService.Bazarr).url("movies/wanted").encodedPath)
        assertEquals("/api/v1/arr/seerr/proxy/api/v1/request/3/approve", ArrClient(api, ArrService.Seerr).url("request/3/approve").encodedPath)
        // The server's apiBase wins over the default.
        assertEquals("/api/v1/arr/sonarr/proxy/api/v5/series", ArrClient(api, ArrService.Sonarr, "api/v5").url("series").encodedPath)
    }

    @Test fun encodesQueryAndRepeatsLists() {
        val u = ArrClient(api, ArrService.Prowlarr).url("search?type=search", mapOf("query" to "a b&c", "indexerIds" to listOf(1, 2), "skip" to null))
        assertEquals("search", u.queryParameter("type"))
        assertEquals("a b&c", u.queryParameter("query"))
        assertEquals(listOf("1", "2"), u.queryParameterValues("indexerIds"))
        assertNull(u.queryParameter("skip"))
        // A path segment cannot escape the proxy prefix.
        val evil = ArrClient(api, ArrService.Radarr).url("movie/..%2F..%2Fadmin")
        assertTrue(evil.encodedPath, evil.encodedPath.startsWith("/api/v1/arr/radarr/proxy/api/v3/movie/"))
        assertEquals("/api/v1/arr/radarr/proxy/api/v3/admin", ArrClient(api, ArrService.Radarr).url("../../admin").encodedPath)
    }

    @Test fun getsJsonWithTheSessionToken() = runTest {
        server.enqueue(MockResponse().setBody("""[{"id":1,"title":"Alien","monitored":true}]"""))
        val list = ArrClient(api, ArrService.Radarr).list("movie")
        assertEquals("Alien", list.single().s("title"))
        val r = server.takeRequest()
        assertEquals("GET", r.method)
        assertEquals("Bearer tok", r.getHeader("Authorization"))
        assertEquals("/api/v1/arr/radarr/proxy/api/v3/movie", r.path)
    }

    @Test fun listsReadPagedAnswers() = runTest {
        server.enqueue(MockResponse().setBody("""{"page":1,"totalRecords":2,"records":[{"id":1},{"id":2}]}"""))
        server.enqueue(MockResponse().setBody("""{"pageInfo":{"results":1},"results":[{"id":9}]}"""))
        server.enqueue(MockResponse().setBody("""{"data":[{"radarrId":4}],"total":1}"""))
        assertEquals(2, ArrClient(api, ArrService.Sonarr).list("queue").size)
        assertEquals(9, ArrClient(api, ArrService.Seerr).list("request").single().i("id"))
        assertEquals(4, ArrClient(api, ArrService.Bazarr).list("movies/wanted").single().i("radarrId"))
    }

    @Test fun sendsCommandsAndPutsWholeResources() = runTest {
        server.enqueue(MockResponse().setBody("""{"id":5,"name":"MoviesSearch"}"""))
        server.enqueue(MockResponse().setBody("""{"id":1}"""))
        server.enqueue(MockResponse().setResponseCode(200))
        val c = ArrClient(api, ArrService.Radarr)
        c.command("MoviesSearch", "movieIds" to listOf(1, 2))
        c.put("movie/1", jsonOf("id" to 1, "monitored" to false, "unknownField" to mapOf("x" to 1)))
        c.patch("x")
        val cmd = server.takeRequest()
        assertEquals("POST", cmd.method)
        assertEquals("""{"name":"MoviesSearch","movieIds":[1,2]}""", cmd.body.readUtf8())
        val put = server.takeRequest()
        assertEquals("PUT", put.method)
        assertTrue(put.body.readUtf8().contains("\"unknownField\":{\"x\":1}"))
        assertEquals("PATCH", server.takeRequest().method)
    }

    @Test fun postsBazarrForms() = runTest {
        server.enqueue(MockResponse().setResponseCode(204))
        ArrClient(api, ArrService.Bazarr).postForm("system/settings", listOf("settings-general-ip" to "0.0.0.0", "languages-enabled" to "en", "languages-enabled" to "hr"))
        val r = server.takeRequest()
        assertTrue(r.getHeader("Content-Type")!!.startsWith("application/x-www-form-urlencoded"))
        assertEquals("settings-general-ip=0.0.0.0&languages-enabled=en&languages-enabled=hr", r.body.readUtf8())
    }

    @Test fun mapsServerErrors() = runTest {
        val c = ArrClient(api, ArrService.Lidarr)
        server.enqueue(MockResponse().setResponseCode(409).setBody("""{"code":"arr_not_configured","message":"lidarr is not configured"}"""))
        assertEquals("arr_not_configured", failure { c.get("artist") }.code)
        server.enqueue(MockResponse().setResponseCode(502).setBody("""{"code":"arr_unreachable","message":"dial tcp: refused"}"""))
        assertEquals("arr_unreachable", failure { c.get("artist") }.code)
        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"code":"invalid_path","message":"bad"}"""))
        assertEquals("invalid_path", failure { c.get("../x") }.code)
        server.enqueue(MockResponse().setResponseCode(413).setBody(""))
        assertEquals("request_too_large", failure { c.get("x") }.code)
        server.enqueue(MockResponse().setResponseCode(502).setBody("<html>Bad gateway</html>"))
        assertEquals("arr_unreachable", failure { c.get("x") }.code)
    }

    @Test fun mapsServiceErrorsAndValidation() = runTest {
        val c = ArrClient(api, ArrService.Radarr)
        server.enqueue(MockResponse().setResponseCode(400).setBody(
            """[{"propertyName":"Path","errorMessage":"Path does not exist","severity":"error"},{"propertyName":"Fields[2].BaseUrl","errorMessage":"Invalid URL","isWarning":true},{"propertyName":"","errorMessage":"Other"}]"""))
        val v = failure { c.post("rootfolder", jsonOf("path" to "/nope")) }
        assertEquals("validation", v.code)
        assertEquals("Path does not exist", v.messageFor("path"))
        assertEquals("Invalid URL", v.messageFor("baseUrl"))
        assertTrue(v.validation[1].warning)
        assertEquals(listOf("Other"), v.unmatched(listOf("path", "baseUrl")).map { it.message })

        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"message":"NotFound","description":"Movie with ID 9 does not exist"}"""))
        val nf = failure { c.get("movie/9") }
        assertEquals("upstream", nf.code)
        assertEquals("NotFound — Movie with ID 9 does not exist", nf.message)
    }

    @Test fun rejectedSessionSignsOut() = runTest {
        var called = false
        api.onUnauthorized = { called = true }
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"code":"unauthenticated","message":"no session"}"""))
        failure { ArrClient(api, ArrService.Sonarr).get("series") }
        assertTrue(called)
    }

    @Test fun imagesGoThroughTheProxy() {
        val c = ArrClient(api, ArrService.Radarr)
        val base = server.url("/").toString().trimEnd('/')
        assertEquals("$base/api/v1/arr/radarr/proxy/MediaCover/1/poster.jpg?lastWrite=1", c.imageUrl("/MediaCover/1/poster.jpg?lastWrite=1"))
        assertEquals("$base/api/v1/arr/radarr/proxy/MediaCover/1/poster.jpg", c.imageUrl("/radarr/MediaCover/1/poster.jpg"))
        assertEquals("https://image.tmdb.org/t/p/a.jpg", c.imageUrl("https://image.tmdb.org/t/p/a.jpg"))
        assertNull(c.imageUrl(null))
    }

    @Test fun readsServiceStatus() = runTest {
        server.enqueue(MockResponse().setBody("""{"services":[{"id":"radarr","configured":true,"reachable":true,"version":"5.2.6","apiBase":"api/v3"},{"id":"seerr","configured":false,"reachable":false,"apiBase":"api/v1"}]}"""))
        val s = ArrClient.services(api)
        assertEquals("/api/v1/arr/services", server.takeRequest().path)
        assertEquals("5.2.6", s[0].version)
        assertEquals(false, s[1].configured)
        assertTrue(ArrService.of("prowlarr")!!.isArr)
        assertNull(ArrService.of("jellyseerr"))
    }
}
