package net.scyde.streamero.api

import kotlinx.coroutines.test.runTest
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ApiOriginTest {
    @Test fun sameOriginComparesSchemeHostAndPort() {
        val base = "https://media.example.org/".toHttpUrl()
        assertTrue(Api.sameOrigin("https://MEDIA.example.org/api/v1/x".toHttpUrl(), base))
        assertFalse(Api.sameOrigin("http://media.example.org/x".toHttpUrl(), base))
        assertFalse(Api.sameOrigin("https://media.example.org:8443/x".toHttpUrl(), base))
        assertFalse(Api.sameOrigin("https://other.example.org/x".toHttpUrl(), base))
    }

    @Test fun tokenIsNotSentToAnotherPortOnTheSameHost() {
        val server = MockWebServer().apply { start() }
        val other = MockWebServer().apply { start() }
        try {
            val api = Api(server.url("/").toString(), "tok")
            other.enqueue(MockResponse().setBody("x"))
            api.http.newCall(Request.Builder().url(other.url("/file")).build()).execute().close()
            assertNull(other.takeRequest().getHeader("Authorization"))
        } finally {
            server.shutdown(); other.shutdown()
        }
    }

    @Test fun emptySuccessIsAnApiErrorNotACrash() = runTest {
        val server = MockWebServer().apply { start() }
        try {
            val api = Api(server.url("/").toString(), "tok")
            server.enqueue(MockResponse().setResponseCode(204))
            val e = runCatching { api.libraries() }.exceptionOrNull()
            assertTrue(e is ApiException)
            assertEquals("empty", (e as ApiException).code)
        } finally {
            server.shutdown()
        }
    }

    @Test fun htmlErrorPageGivesGenericCode() = runTest {
        val server = MockWebServer().apply { start() }
        try {
            val api = Api(server.url("/").toString(), "tok")
            server.enqueue(MockResponse().setResponseCode(502).setBody("<html><body>Bad gateway</body></html>"))
            val e = runCatching { api.libraries() }.exceptionOrNull() as ApiException
            assertEquals(502, e.status)
            assertEquals("generic", e.code)
        } finally {
            server.shutdown()
        }
    }
}
