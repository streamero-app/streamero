package net.scyde.streamero.manage

import net.scyde.streamero.api.APP_SCHEME
import net.scyde.streamero.api.InviteLink
import net.scyde.streamero.api.SSO_REDIRECT
import net.scyde.streamero.api.SsoResult
import net.scyde.streamero.api.parseInviteLink
import net.scyde.streamero.api.parseSsoRedirect
import net.scyde.streamero.api.ssoAuthorizeUrl
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** Streamero Manage has its own deep-link scheme, so it never competes with the Streamero app. */
class SchemeTest {
    private val token = "abcdefghijklmnop1234"

    @Test fun manageUsesItsOwnScheme() {
        assertEquals("streamero-manage", APP_SCHEME)
        assertEquals("streamero-manage://oidc", SSO_REDIRECT)
        assertEquals("https://m/a?client=app&redirectUri=streamero-manage%3A%2F%2Foidc&codeChallenge=c", ssoAuthorizeUrl("https://m/a", "c"))
    }

    @Test fun onlyItsOwnSsoRedirectIsAccepted() {
        assertEquals(SsoResult.Code("x"), parseSsoRedirect("streamero-manage", "oidc", "x", null))
        assertNull(parseSsoRedirect("streamero", "oidc", "x", null))
    }

    @Test fun pastedInvitesWorkWithEitherScheme() {
        assertEquals(InviteLink("https://m.example", token), parseInviteLink("streamero-manage://invite?server=https%3A%2F%2Fm.example&token=$token"))
        assertEquals(InviteLink("https://m.example", token), parseInviteLink("streamero://invite?server=https%3A%2F%2Fm.example&token=$token"))
    }
}
