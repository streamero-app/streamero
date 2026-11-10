package net.scyde.streamero.ui

import android.content.res.Configuration
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TvTest {
    @Test fun televisionModeIsATv() = assertTrue(isTelevision(Configuration.UI_MODE_TYPE_TELEVISION, hasLeanback = false))
    @Test fun leanbackFeatureIsATv() = assertTrue(isTelevision(Configuration.UI_MODE_TYPE_NORMAL, hasLeanback = true))
    @Test fun aPhoneIsNotATv() = assertFalse(isTelevision(Configuration.UI_MODE_TYPE_NORMAL, hasLeanback = false))
    @Test fun aCarIsNotATv() = assertFalse(isTelevision(Configuration.UI_MODE_TYPE_CAR, hasLeanback = false))

    @Test fun keyboardTypesAndDeletes() {
        assertEquals("ab", typeKey("a", "b"))
        assertEquals("a ", typeKey("a", KEY_SPACE))
        assertEquals("a ", typeKey("a ", KEY_SPACE)) // no double spaces
        assertEquals("", typeKey("", KEY_SPACE)) // no leading space
        assertEquals("a", typeKey("ab", KEY_DELETE))
        assertEquals("", typeKey("", KEY_DELETE))
        assertEquals("", typeKey("abc", KEY_CLEAR))
    }

    @Test fun seekStaysInsideTheFilm() {
        assertEquals(0L, seekTarget(5_000, -10_000, 60_000))
        assertEquals(60_000L, seekTarget(55_000, 10_000, 60_000))
        assertEquals(30_000L, seekTarget(20_000, 10_000, 60_000))
        assertEquals(30_000L, seekTarget(20_000, 10_000, 0)) // duration unknown yet
    }
}
