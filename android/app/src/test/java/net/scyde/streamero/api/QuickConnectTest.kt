package net.scyde.streamero.api

import java.io.IOException
import java.time.Instant
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.currentTime
import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class QuickConnectTest {
    private val user = User(id = "u", username = "ana")
    private val signedIn = AuthResult(token = "t", sessionId = "s", user = user)

    /** A fake server: [polls] answers each poll in order (null = still waiting). */
    private class Fake(val polls: MutableList<() -> AuthResult?>, val ttlSeconds: Long = 300, val clock: () -> Instant) : QuickConnectApi {
        var starts = 0
        val secrets = mutableListOf<String>()
        override suspend fun start(): QuickConnectStart {
            starts++
            return QuickConnectStart("12345$starts", "secret$starts", clock().plusSeconds(ttlSeconds).toString())
        }
        override suspend fun poll(secret: String): AuthResult? { secrets += secret; return if (polls.isEmpty()) null else polls.removeAt(0)() }
    }

    private fun TestScope.clock(): () -> Instant = { Instant.ofEpochMilli(currentTime) }

    @Test fun showsTheCodeThenSignsInWhenApproved() = runTest {
        val fake = Fake(mutableListOf({ null }, { null }, { signedIn }), clock = clock())
        val states = quickConnectFlow(fake, clock()).toList()
        assertEquals(QuickConnectState.Code("123451", Instant.ofEpochMilli(300_000)), states[0])
        assertEquals(QuickConnectState.Approved(signedIn), states[1])
        assertEquals(listOf("secret1", "secret1", "secret1"), fake.secrets)
        assertEquals(9_000L, currentTime) // three polls, three seconds apart
    }

    @Test fun anExpiredCodeIsReplacedByANewOne() = runTest {
        val fake = Fake(mutableListOf({ throw ApiException(404, "not_found", "gone") }, { signedIn }), clock = clock())
        val states = quickConnectFlow(fake, clock()).toList()
        assertEquals(2, fake.starts)
        assertEquals("123452", (states[1] as QuickConnectState.Code).code)
        assertTrue(states.last() is QuickConnectState.Approved)
    }

    @Test fun networkHiccupsAreRetried() = runTest {
        val fake = Fake(mutableListOf({ throw IOException("down") }, { throw IOException("down") }, { signedIn }), clock = clock())
        val states = quickConnectFlow(fake, clock()).toList()
        assertTrue(states.last() is QuickConnectState.Approved)
    }

    @Test fun givesUpAfterTooManyFailures() = runTest {
        val fake = Fake(MutableList(20) { { throw IOException("down") } }, clock = clock())
        val states = quickConnectFlow(fake, clock(), maxFailures = 3).toList()
        assertTrue(states.last() is QuickConnectState.Failed)
    }

    @Test fun stopsAfterSeveralUnusedCodes() = runTest {
        val fake = Fake(mutableListOf(), ttlSeconds = 10, clock = clock())
        val states = quickConnectFlow(fake, clock(), maxCodes = 2).toList()
        assertEquals(2, fake.starts)
        assertEquals(QuickConnectState.Expired, states.last())
    }

    @Test fun pollReadsWaitingApprovedAndErrors() = runTest {
        val server = MockWebServer().apply { start() }
        try {
            val api = Api(server.url("/").toString())
            server.enqueue(MockResponse().setResponseCode(202))
            assertNull(api.quickConnectPoll("abc"))
            assertEquals("/api/v1/quickconnect/poll", server.takeRequest().path)
            server.enqueue(MockResponse().setBody("""{"token":"tok","sessionId":"s","user":{"id":"u","username":"ana"}}"""))
            assertEquals("tok", api.quickConnectPoll("abc")?.token)
            server.takeRequest()
            server.enqueue(MockResponse().setResponseCode(404).setBody("""{"code":"not_found","message":"gone"}"""))
            val e = runCatching { api.quickConnectPoll("abc") }.exceptionOrNull() as ApiException
            assertEquals(404, e.status)
            server.takeRequest()
            server.enqueue(MockResponse().setBody("""{"deviceName":"Living room TV"}"""))
            assertEquals("Living room TV", api.quickConnectAuthorize("123 456").deviceName)
            assertEquals("""{"code":"123456"}""", server.takeRequest().body.readUtf8())
        } finally { server.shutdown() }
    }
}
