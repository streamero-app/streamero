package net.scyde.streamero.api

import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class FormsTest {
    private val token = "Xy3_kL9-aB2cD4eF6gH8iJ0kLmNoPqRsTuVwXyZ01234"

    @Test fun parsesWebInviteLink() {
        assertEquals(InviteLink("https://media.example.com", token), parseInviteLink("https://media.example.com/invite/$token"))
        assertEquals(InviteLink("http://10.0.0.5:8096", token), parseInviteLink("  http://10.0.0.5:8096/invite/$token?x=1#top "))
    }

    @Test fun keepsPathPrefixAndAddsScheme() {
        assertEquals(InviteLink("https://example.com/streamero", token), parseInviteLink("https://example.com/streamero/invite/$token"))
        assertEquals(InviteLink("https://media.example.com", token), parseInviteLink("media.example.com/invite/$token"))
    }

    @Test fun parsesBareTokenAndDeepLink() {
        assertEquals(InviteLink(null, token), parseInviteLink(token))
        assertEquals(InviteLink("https://media.example.com", token),
            parseInviteLink("streamero://invite?server=https%3A%2F%2Fmedia.example.com%2F&token=$token"))
        assertEquals(InviteLink(null, token), parseInviteDeepLink("streamero://invite?token=$token"))
    }

    @Test fun rejectsWhatIsNotAnInvite() {
        assertNull(parseInviteLink(""))
        assertNull(parseInviteLink("hello"))
        assertNull(parseInviteLink("https://media.example.com/together/abc"))
        assertNull(parseInviteLink("https://media.example.com/invite/"))
        assertNull(parseInviteLink("https://media.example.com/invite/short"))
        assertNull(parseInviteLink("streamero://oidc?code=abc"))
        assertNull(parseInviteDeepLink("streamero://invite?server=x"))
    }

    @Test fun signUpValidation() {
        assertEquals(SignUpProblem.Username, signUpProblem(" ", "password1", "password1"))
        assertEquals(SignUpProblem.Username, signUpProblem("a".repeat(65), "password1", "password1"))
        assertEquals(SignUpProblem.PasswordShort, signUpProblem("ana", "short", "short"))
        assertEquals(SignUpProblem.Mismatch, signUpProblem("ana", "password1", "password2"))
        assertNull(signUpProblem("ana", "password1", "password1"))
    }

    @Test fun profileValidation() {
        assertTrue(profileFormValid("Ana", "#e5484d", ""))
        assertTrue(profileFormValid("Ana", "#E5484D", "123456"))
        assertFalse(profileFormValid("  ", "#e5484d", ""))
        assertFalse(profileFormValid("a".repeat(41), "#e5484d", ""))
        assertFalse(profileFormValid("Ana", "red", ""))
        assertFalse(profileFormValid("Ana", "#e5484d", "123"))
        assertFalse(profileFormValid("Ana", "#e5484d", "12a4"))
        assertFalse(profileFormValid("Ana", "#e5484d", "1234567"))
        assertTrue(PROFILE_COLORS.all { profileFormValid("x", it, "") })
    }

    @Test fun profileInputKeepsOrRemovesPin() {
        val keep = profileInput(" Ana ", "#e5484d", true, 12, "", false)
        assertEquals("Ana", keep.name); assertNull(keep.pin); assertNull(keep.removePin); assertEquals(12, keep.maxRating)
        val change = profileInput("Ana", "#e5484d", false, null, "1234", false)
        assertEquals("1234", change.pin)
        assertEquals(true, profileInput("Ana", "#e5484d", false, null, "", true).removePin)
        // Omitted fields are left out of the request, so the server keeps the PIN.
        assertEquals("""{"name":"Ana","color":"#e5484d","isKid":true,"maxRating":12}""", json.encodeToString(ProfileInput.serializer(), keep))
    }

    @Test fun parsesOtpAuthUri() {
        val o = parseOtpAuth("otpauth://totp/Streamero:ana%40example.com?secret=JBSWY3DPEHPK3PXP&issuer=Streamero&digits=6&period=30")!!
        assertEquals("Streamero", o.issuer); assertEquals("ana@example.com", o.account); assertEquals("JBSWY3DPEHPK3PXP", o.secret)
        assertEquals(6, o.digits); assertEquals(30, o.period)
        val noIssuerParam = parseOtpAuth("otpauth://totp/Media%20Server:ana?secret=ABC")!!
        assertEquals("Media Server", noIssuerParam.issuer); assertEquals("ana", noIssuerParam.account)
        assertNull(parseOtpAuth("otpauth://hotp/x?secret=ABC"))
        assertNull(parseOtpAuth("otpauth://totp/x"))
        assertNull(parseOtpAuth("https://example.com"))
    }

    @Test fun secretAndCodeFormatting() {
        assertEquals("JBSW Y3DP EHPK 3PXP", groupSecret("JBSWY3DPEHPK3PXP"))
        assertEquals("ABCD E", groupSecret("AB CDE"))
        assertEquals("123456", cleanTotpCode(" 123 456 "))
        assertEquals("abcd-efgh", cleanTotpCode("abcd-efgh!"))
    }

    @Test fun adminFormRules() {
        assertTrue(isValidOidcId("authentik")); assertTrue(isValidOidcId("my-idp-2"))
        assertFalse(isValidOidcId("-x")); assertFalse(isValidOidcId("Auth")); assertFalse(isValidOidcId("a".repeat(33)))
        assertEquals(listOf("openid", "profile", "email"), parseScopes(" openid  profile,email openid "))
        assertTrue(notificationTargetValid(NotificationTarget("1", "Discord", "discord", "https://discord.com/api/webhooks/1")))
        assertFalse(notificationTargetValid(NotificationTarget("1", "", "discord", "https://x.y")))
        assertFalse(notificationTargetValid(NotificationTarget("1", "n", "ntfy", "ntfy.sh/topic")))
        assertTrue(sourceValid("IPTV", false, "https://x/p.m3u", ""))
        assertFalse(sourceValid("IPTV", true, "https://x/p.m3u", ""))
        assertFalse(sourceValid(" ", false, "https://x", ""))
        assertNull(optionalInt("", 0..64)); assertEquals(64, optionalInt("99", 0..64)); assertNull(optionalInt("x", 0..64))
        assertTrue(restoreConfirmed("streamero-2026.tar.gz", "streamero-2026.tar.gz"))
        assertFalse(restoreConfirmed("streamero-2026.tar.gz ", "streamero-2026.tar.gz"))
        assertEquals("512 B", formatBytes(512)); assertEquals("1.5 MB", formatBytes(1_572_864))
    }

    @Test fun inviteRequestsAndSecretsAreWriteOnly() = runTest {
        val server = MockWebServer()
        server.start()
        try {
            val api = Api(server.url("/").toString())
            server.enqueue(MockResponse().setBody("""{"serverName":"Kino","expiresAt":"2026-12-01T00:00:00Z"}"""))
            assertEquals("Kino", api.inviteInfo(token).serverName)
            assertEquals("/api/v1/invite/$token", server.takeRequest().path)
            server.enqueue(MockResponse().setBody("""{"token":"t","sessionId":"s","user":{"id":"u","username":"ana"}}"""))
            assertEquals("t", api.acceptInvite(token, AcceptInvite("ana", "password1", DeviceInfo("Pixel", "Streamero Android"))).token)
            val body = server.takeRequest().body.readUtf8()
            assertTrue(body.contains("\"username\":\"ana\"") && !body.contains("useCookie"))
            // An unchanged OIDC secret and LDAP bind password are not sent at all.
            server.enqueue(MockResponse().setBody("""{"enabled":true}"""))
            api.saveLdapSettings(LdapSettings(enabled = true, bindPasswordSet = true).toInput())
            assertFalse(server.takeRequest().body.readUtf8().contains("bindPassword"))
            server.enqueue(MockResponse().setBody("""{"id":"a","label":"A","issuer":"https://a","clientId":"c"}"""))
            api.updateOidcProvider("a", OidcProvider("a", "A", "https://a", "c", clientSecretSet = true).toInput())
            assertFalse(server.takeRequest().body.readUtf8().contains("clientSecret"))
        } finally { server.shutdown() }
    }
}
