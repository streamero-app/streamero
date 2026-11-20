package net.scyde.streamero.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TvPlayerKeysTest {
    @Test fun theScreenSeeksOnlyWhenNothingElseIsOpen() {
        assertTrue(screenTakesArrows(panel = false, controls = false, failed = false))
        assertFalse(screenTakesArrows(panel = true, controls = false, failed = false))
        assertFalse(screenTakesArrows(panel = false, controls = true, failed = false))
    }

    @Test fun anErrorBoxCanBeOperatedWithTheRemote() {
        // Its Retry and Back buttons need the arrows and the centre key.
        assertFalse(screenTakesArrows(panel = false, controls = false, failed = true))
    }
}
