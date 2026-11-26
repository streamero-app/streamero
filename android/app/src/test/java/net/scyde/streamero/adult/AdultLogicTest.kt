package net.scyde.streamero.adult

import net.scyde.streamero.api.AdultStatus
import net.scyde.streamero.api.ApiException
import net.scyde.streamero.api.Profile
import net.scyde.streamero.api.User
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AdultLogicTest {
    private val user = User(id = "u", username = "a")

    @Test fun entryOnlyWithAccess() {
        assertFalse(showsAdultSection(null))
        assertFalse(showsAdultSection(user))
        assertTrue(showsAdultSection(user.copy(adultAccess = true)))
        val kid = Profile(id = "p", name = "k", color = "#000", isKid = true)
        assertFalse(showsAdultSection(user.copy(adultAccess = true, currentProfile = kid)))
    }

    @Test fun pinRules() {
        assertTrue(validAdultPin("1234"))
        assertTrue(validAdultPin("123456789012"))
        assertFalse(validAdultPin("123"))
        assertFalse(validAdultPin("1234567890123"))
        assertFalse(validAdultPin("12a4"))
        assertFalse(validAdultPin(""))
    }

    @Test fun gate() {
        assertEquals(AdultGate.Loading, adultGate(null, null))
        assertEquals(AdultGate.Open, adultGate(AdultStatus(pinSet = false, unlocked = true), null))
        assertEquals(AdultGate.Open, adultGate(AdultStatus(pinSet = true, unlocked = true), null))
        assertEquals(AdultGate.Locked, adultGate(AdultStatus(pinSet = true, unlocked = false), null))
        assertEquals(AdultGate.Locked, adultGate(null, ApiException(403, "adult_locked", "")))
        assertEquals(AdultGate.Unavailable, adultGate(null, ApiException(404, "not_found", "")))
    }

    @Test fun queryLeavesDefaultsOut() {
        val q = AdultSceneFilter(q = "  ", studioId = "s").query(60, 60)
        assertEquals(null, q["q"])
        assertEquals(null, q["kind"])
        assertEquals(null, q["status"])
        assertEquals("s", q["studioId"])
        assertEquals(60, q["offset"])
        val q2 = AdultSceneFilter(q = "x", kind = "movie", status = "inProgress", sort = "title").query(0, 60)
        assertEquals("x", q2["q"])
        assertEquals("movie", q2["kind"])
        assertEquals("inProgress", q2["status"])
        assertEquals("title", q2["sort"])
    }

    @Test fun blurRevealsOncePerImage() {
        val b = AdultBlur(enabled = true)
        assertTrue(b.blurred("scene:1"))
        assertTrue("the first tap reveals", b.reveal("scene:1"))
        assertFalse(b.blurred("scene:1"))
        assertFalse("the next tap opens", b.reveal("scene:1"))
        assertTrue(b.blurred("scene:2"))
        // A new visit of the screen starts blurred again.
        assertTrue(AdultBlur(enabled = true).blurred("scene:1"))
        val off = AdultBlur(enabled = false)
        assertFalse(off.blurred("scene:1"))
        assertFalse(off.reveal("scene:1"))
    }

    @Test fun galleryFilterLeavesDefaultsOut() {
        val q = AdultGalleryFilter(q = "  set ", sort = "date", tag = "Outdoor").query(60, 60)
        assertEquals("set", q["q"])
        assertEquals(null, q["sort"])
        assertEquals("Outdoor", q["tag"])
        assertEquals("added", AdultGalleryFilter(sort = "added").query(0, 1)["sort"])
    }

    @Test fun hidingTagsIsCaseInsensitive() {
        assertEquals(listOf("Outdoor"), toggleHiddenTag(emptyList(), "Outdoor"))
        assertEquals(emptyList<String>(), toggleHiddenTag(listOf("outdoor"), "Outdoor"))
    }

    @Test fun picturesWrapAround() {
        assertEquals(1, nextPicture(0, 1, 3))
        assertEquals(0, nextPicture(2, 1, 3))
        assertEquals(2, nextPicture(0, -1, 3))
        assertEquals(0, nextPicture(0, 1, 0))
    }

    @Test fun navigationHasEveryPlace() {
        assertEquals(listOf("Home", "Scenes", "Movies", "Photos", "Performers", "Studios", "Tags"), AdultPlace.entries.map { it.name })
    }
}
