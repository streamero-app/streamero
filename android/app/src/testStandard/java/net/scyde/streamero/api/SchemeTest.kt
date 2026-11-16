package net.scyde.streamero.api

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The standard app keeps the `streamero://` scheme the server and web already use. */
class SchemeTest {
    @Test fun standardKeepsStreameroScheme() {
        assertEquals("streamero", APP_SCHEME)
        assertEquals("streamero://oidc", SSO_REDIRECT)
        assertNull(parseSsoRedirect("streamero-manage", "oidc", "x", null))
        assertNull(parseInviteDeepLink("streamero-manage://invite?token=abcdefghijklmnop1234"))
    }
}
