package net.scyde.streamero

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LaunchLinkTest {
    @Test fun onlyAFreshStartHandsTheLinkOn() {
        assertTrue(isFreshLink(restored = false, launchedFromHistory = false))
        assertFalse(isFreshLink(restored = true, launchedFromHistory = false))
        assertFalse(isFreshLink(restored = false, launchedFromHistory = true))
    }
}
