package net.scyde.streamero.api

import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class SsoTest {
    private val server = MockWebServer()
    private lateinit var api: Api

    @Before fun start() { server.start(); api = Api(server.url("/").toString()) }
    @After fun stop() = server.shutdown()

    @Test fun challengeIsBase64UrlSha256OfTheVerifier() {
        // Expected value computed independently: base64url(sha256(v)) without padding (Python hashlib).
        assertEquals("IB3LGqhE3YEn1yejQ2Ky3PGP-zyDyKSyUWSvnQFAKiA", Pkce.challenge("dBjftJeZ4CVP-mJ0kXwOz2u9eiU0n3yhfPkXgPYgEDvmkoUOuIqI9Bu0WQe"))
    }

    @Test fun verifiersAreRandomAndWellFormed() {
        val a = Pkce.verifier()
        val b = Pkce.verifier()
        assertNotEquals(a, b)
        assertTrue(a, Regex("^[A-Za-z0-9_-]{43}$").matches(a))
        assertTrue(Regex("^[A-Za-z0-9_-]{43}$").matches(Pkce.challenge(a)))
    }

    @Test fun authorizeUrlAsksForTheAppFlow() {
        assertEquals("https://m.example/api/v1/auth/oidc/kc/authorize?client=app&redirectUri=streamero%3A%2F%2Foidc&codeChallenge=abc",
            ssoAuthorizeUrl("https://m.example/api/v1/auth/oidc/kc/authorize", "abc"))
    }

    @Test fun parsesTheRedirect() {
        assertEquals(SsoResult.Code("c1"), parseSsoRedirect("streamero", "oidc", "c1", null))
        assertEquals(SsoResult.Failed("no_account"), parseSsoRedirect("streamero", "oidc", null, "no_account"))
        assertEquals(SsoResult.Failed("internal"), parseSsoRedirect("streamero", "oidc", null, null))
        assertNull(parseSsoRedirect("https", "oidc", "c1", null))
        assertNull(parseSsoRedirect("streamero", "other", "c1", null))
    }

    @Test fun readsProviders() = runTest {
        server.enqueue(MockResponse().setBody("""{"passwordLogin":false,"ldap":true,"oidc":[{"id":"kc","label":"Keycloak","authorizeUrl":"https://m/api/v1/auth/oidc/kc/authorize"}]}"""))
        val p = api.authProviders()
        assertFalse(p.passwordLogin)
        assertTrue(p.ldap)
        assertEquals("Keycloak", p.oidc.single().label)
        assertEquals("/api/v1/auth/providers", server.takeRequest().path)
    }

    @Test fun exchangesTheCodeWithTheVerifier() = runTest {
        server.enqueue(MockResponse().setBody("""{"token":"t1","sessionId":"s","user":{"id":"u","username":"ana"}}"""))
        val res = api.oidcExchange("code1", "verifier1", DeviceInfo("Pixel", "Streamero Android"))
        assertEquals("t1", res.token)
        val r = server.takeRequest()
        assertEquals("POST", r.method)
        assertEquals("/api/v1/auth/oidc/exchange", r.path)
        val body = r.body.readUtf8()
        assertTrue(body, "\"code\":\"code1\"" in body && "\"codeVerifier\":\"verifier1\"" in body && "\"device\"" in body)
    }

    @Test fun exchangeErrorsCarryTheServerCode() = runTest {
        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"code":"invalid_state","message":"expired"}"""))
        val e = runCatching { api.oidcExchange("c", "v", DeviceInfo("x", "y")) }.exceptionOrNull() as ApiException
        assertEquals("invalid_state", e.code)
    }
}
