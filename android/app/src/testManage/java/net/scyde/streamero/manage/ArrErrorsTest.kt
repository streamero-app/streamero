package net.scyde.streamero.manage

import kotlinx.coroutines.test.runTest
import net.scyde.streamero.api.Api
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test

/** How the proxy's and the services' error answers are read (D-049, D-052). */
class ArrErrorsTest {
    private val server = MockWebServer()
    private lateinit var api: Api

    @Before fun start() { server.start(); api = Api(server.url("/").toString(), "tok") }
    @After fun stop() = server.shutdown()

    private suspend fun failure(block: suspend () -> Unit): ArrException {
        try { block() } catch (e: ArrException) { return e }
        fail("expected an ArrException"); throw IllegalStateException()
    }

    @Test fun everyProxyCodeIsKept() {
        val codes = mapOf(
            401 to "unauthenticated", 403 to "forbidden", 400 to "invalid_path", 405 to "method_not_allowed",
            409 to "arr_not_configured", 413 to "request_too_large", 429 to "rate_limited", 502 to "arr_unreachable",
        )
        for ((status, code) in codes) {
            val e = ArrException.parse(status, """{"code":"$code","message":"m"}""")
            assertEquals(code, e.code)
            assertEquals(status, e.status)
        }
        // D-052: a read-only API key is refused on writes.
        assertEquals("read_only_key", ArrException.parse(403, """{"code":"read_only_key","message":"read-only API key"}""").code)
        // A code without a message still counts.
        assertEquals("rate_limited", ArrException.parse(429, """{"code":"rate_limited"}""").code)
    }

    @Test fun everyProxyCodeHasItsOwnMessage() {
        val codes = listOf("unauthenticated", "forbidden", "read_only_key", "invalid_path", "method_not_allowed", "arr_not_configured",
            "request_too_large", "rate_limited", "arr_unreachable", "arr_auth", "not_found")
        val res = codes.map { assertNotNull(it, arrErrorRes(it)); arrErrorRes(it) }
        assertEquals("one message per code", codes.size, res.toSet().size)
        // The services' own words are shown for these.
        assertNull(arrErrorRes("upstream"))
        assertNull(arrErrorRes("validation"))
    }

    @Test fun htmlAndEmptyBodiesNeverShowMarkup() {
        val html = ArrException.parse(500, "<!DOCTYPE html><html><body><h1>Internal error</h1></body></html>")
        assertEquals("upstream", html.code)
        assertNull(html.detail)
        assertFalse(html.message!!.contains("<"))
        val gateway = ArrException.parse(504, "<html>Gateway Timeout</html>")
        assertEquals("arr_unreachable", gateway.code)
        assertEquals("not_found", ArrException.parse(404, "").code)
        assertEquals("request_too_large", ArrException.parse(413, "<html>413 Request Entity Too Large</html>").code)
        assertEquals("rate_limited", ArrException.parse(429, "").code)
        // Broken JSON is not a crash.
        assertEquals("upstream", ArrException.parse(500, """{"message": "cut off""").code)
        assertEquals("upstream", ArrException.parse(500, "[1, 2").code)
    }

    @Test fun servicesOwnShapesSurfaceTheirMessage() {
        assertEquals("Movie exists", ArrException.parse(400, """{"error":"Movie exists"}""").detail)
        assertEquals("Bad key", ArrException.parse(400, """{"error":{"message":"Bad key"}}""").detail)
        assertEquals("Not allowed", ArrException.parse(400, "\"Not allowed\"").detail)
        assertEquals("plain words", ArrException.parse(500, "plain words").detail)
        val seerr = ArrException.parse(400, """{"message":"Validation failed","errors":["hostname is required"]}""")
        assertEquals("Validation failed\nhostname is required", seerr.detail)
        // ASP.NET problem details: field errors go under their fields.
        val pd = ArrException.parse(400, """{"title":"One or more validation errors occurred.","status":400,"errors":{"Path":["The Path field is required."]}}""")
        assertEquals("validation", pd.code)
        assertEquals("The Path field is required.", pd.messageFor("path"))
        // Validation arrays with odd entries do not crash.
        val v = ArrException.parse(400, """[null, 3, {"propertyName":"Name","errorMessage":"Should be unique"}, {"x":1}]""")
        assertEquals("Should be unique", v.messageFor("name"))
        val numbers = ArrException.parse(500, """{"message": 12}""")
        assertEquals("12", numbers.detail)
    }

    @Test fun aServiceRefusingItsKeyDoesNotSignOut() = runTest {
        var signedOut = false
        api.onUnauthorized = { signedOut = true }
        val c = ArrClient(api, ArrService.Radarr)
        // Radarr with a wrong stored key answers 401 with an empty body; the proxy passes it on.
        server.enqueue(MockResponse().setResponseCode(401).setBody(""))
        assertEquals("arr_auth", failure { c.get("movie") }.code)
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"message":"Unauthorized"}"""))
        assertEquals("arr_auth", failure { c.get("movie") }.code)
        assertFalse("only the Streamero server's own 401 signs out", signedOut)
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"code":"unauthenticated","message":"no session"}"""))
        assertEquals("unauthenticated", failure { c.get("movie") }.code)
        assertTrue(signedOut)
    }

    @Test fun conflictsFromAServiceAreNotTakenForAMissingSetup() {
        val e = ArrException.parse(409, """{"message":"This movie has already been added"}""")
        assertEquals("upstream", e.code)
        assertEquals("This movie has already been added", e.detail)
    }
}
