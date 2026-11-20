package net.scyde.streamero.api

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ServerUrlTest {
    private val token = "Xy3_kL9-aB2cD4eF6gH8iJ0kLmNoPqRsTuVwXyZ01234"

    @Test fun normalizesServerAddresses() {
        assertEquals("https://media.example.com", normalizeServerUrl("media.example.com"))
        assertEquals("https://media.example.com:8096", normalizeServerUrl("  Media.Example.com:8096/ "))
        assertEquals("http://192.168.1.5:8096", normalizeServerUrl("http://192.168.1.5:8096"))
        assertEquals("https://example.com/streamero", normalizeServerUrl("https://example.com/streamero/?x=1#top"))
        assertEquals("https://httpbin.example", normalizeServerUrl("httpbin.example"))
    }

    @Test fun rejectsAddressesThatAreNotPlainServers() {
        assertNull(normalizeServerUrl(""))
        assertNull(normalizeServerUrl(null))
        assertNull(normalizeServerUrl("https://"))
        assertNull(normalizeServerUrl("ftp://media.example.com"))
        assertNull(normalizeServerUrl("javascript:alert(1)"))
        assertNull(normalizeServerUrl("https://user:secret@media.example.com"))
        assertNull(normalizeServerUrl("https://media.example.com@evil.example"))
        assertNull(normalizeServerUrl("file:///etc/passwd"))
    }

    @Test fun deepLinkServerMustBeAPlainHttpAddress() {
        fun link(server: String) = "streamero://invite?server=${java.net.URLEncoder.encode(server, "UTF-8")}&token=$token"
        assertEquals(InviteLink("https://media.example.com", token), parseInviteDeepLink(link("https://media.example.com/")))
        assertEquals(InviteLink("https://example.com/streamero", token), parseInviteDeepLink(link("https://example.com/streamero")))
        // A named server that is not usable rejects the link instead of sending the token elsewhere.
        assertNull(parseInviteDeepLink(link("https://user:pw@media.example.com")))
        assertNull(parseInviteDeepLink(link("javascript:alert(1)")))
        assertNull(parseInviteDeepLink(link("content://x")))
        assertNull(parseInviteDeepLink(link("")))
    }

    @Test fun deepLinkTokenIsChecked() {
        assertNull(parseInviteDeepLink("streamero://invite?server=https%3A%2F%2Fm.example.com&token=short"))
        assertNull(parseInviteDeepLink("streamero://invite?token=" + "a".repeat(129)))
        assertNull(parseInviteDeepLink("streamero://invite?token=abc%3Cscript%3Eabcdefghijkl"))
        assertNull(parseInviteDeepLink("streamero://invite?token=%zz"))
        assertEquals(InviteLink(null, token), parseInviteDeepLink("streamero://invite?token=$token"))
    }

    @Test fun webInviteLinkWithCredentialsIsRefused() {
        assertNull(parseInviteLink("https://admin:pw@media.example.com/invite/$token"))
        assertNull(parseInviteLink("ftp://media.example.com/invite/$token"))
        assertEquals(InviteLink("https://media.example.com", token), parseInviteLink("https://media.example.com/invite/$token"))
    }

    @Test fun inviteTextShowsTheServer() {
        val l = InviteLink("https://example.com/streamero", token)
        assertEquals("https://example.com/streamero/invite/$token", inviteText(l))
        assertEquals(l, parseInviteLink(inviteText(l)))
        assertEquals(token, inviteText(InviteLink(null, token)))
    }

    @Test fun webLinkKeepsThePathPrefix() {
        assertEquals("https://example.com/streamero/invite/abc", webLink("https://example.com/streamero", "invite", "abc"))
        assertEquals("https://example.com/invite/abc", webLink("https://example.com/", "invite", "abc"))
        assertEquals("https://example.com/settings/livetv", webLink("https://example.com", "settings", "livetv"))
        assertEquals("https://example.com/a%2Fb", webLink("https://example.com", "a/b"))
    }

    @Test fun localHosts() {
        listOf("192.168.1.5", "10.0.0.1", "172.16.0.1", "172.31.255.255", "127.0.0.1", "169.254.10.1", "100.64.0.1", "100.127.255.1",
            "[::1]", "::1", "fd12:3456::1", "fc00::1", "fe80::1", "fe80::1%eth0", "::ffff:192.168.0.1",
            "nas", "LOCALHOST", "nas.local", "box.lan", "media.home.arpa", "srv.internal", "nas.local.").forEach {
            assertTrue(it, isLocalHost(it))
        }
    }

    @Test fun remoteHosts() {
        listOf("8.8.8.8", "172.32.0.1", "172.15.0.1", "100.128.0.1", "100.63.0.1", "192.169.0.1", "2001:db8::1", "[2606:4700::1111]",
            "media.example.com", "evil.local.example.com", "lan.example", "", "999.1.1.1", "fe00::1").forEach {
            assertFalse(it, isLocalHost(it))
        }
    }

    @Test fun cleartextWarningOnlyForRemoteHttp() {
        assertTrue(isCleartextRemote("http://media.example.com"))
        assertTrue(isCleartextRemote("http://203.0.113.7:8096"))
        assertFalse(isCleartextRemote("https://media.example.com"))
        assertFalse(isCleartextRemote("http://192.168.1.2:8096"))
        assertFalse(isCleartextRemote("http://nas.local:8096"))
        assertFalse(isCleartextRemote("http://[fd00::5]:8096"))
        assertFalse(isCleartextRemote(null))
        assertFalse(isCleartextRemote("not a url at all"))
    }

    @Test fun brokenEscapesInOtpLinksAreRefused() {
        assertNull(parseOtpAuth("otpauth://totp/x?secret=%zz"))
    }
}
