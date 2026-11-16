package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class BulkAndPermissionsTest {
    @Test fun bulkEditSendsOnlyWhatIsSet() {
        val b = BulkEditor.editBody(ArrService.Radarr, setOf(3, 1), BulkEdit(monitored = false))
        assertEquals(setOf("movieIds", "monitored"), b.keys)
        assertEquals(listOf(1, 3), b.ints("movieIds"))
        assertEquals(false, b.b("monitored"))
        assertEquals("movie/editor", BulkEditor.resource(ArrService.Radarr))
    }

    @Test fun bulkEditPerService() {
        val sonarr = BulkEditor.editBody(ArrService.Sonarr, listOf(5), BulkEdit(qualityProfileId = 7))
        assertEquals(listOf(5), sonarr.ints("seriesIds"))
        assertEquals(7, sonarr.i("qualityProfileId"))
        assertEquals("series/editor", BulkEditor.resource(ArrService.Sonarr))
        val lidarr = BulkEditor.editBody(ArrService.Lidarr, listOf(9), BulkEdit(rootFolderPath = "/music", moveFiles = true))
        assertEquals(listOf(9), lidarr.ints("artistIds"))
        assertEquals("/music", lidarr.s("rootFolderPath"))
        assertEquals(true, lidarr.b("moveFiles"))
        assertEquals("artist/editor", BulkEditor.resource(ArrService.Lidarr))
    }

    @Test fun bulkTagsCarryTheirMode() {
        val b = BulkEditor.editBody(ArrService.Radarr, listOf(1), BulkEdit(tags = listOf(4, 5), tagMode = TagMode.Remove))
        assertEquals(listOf(4, 5), b.ints("tags"))
        assertEquals("remove", b.s("applyTags"))
        assertNull(b["moveFiles"])
        val replaceNone = BulkEditor.editBody(ArrService.Radarr, listOf(1), BulkEdit(tags = emptyList(), tagMode = TagMode.Replace))
        assertEquals("replace", replaceNone.s("applyTags"))
        assertTrue(BulkEdit().isEmpty)
    }

    @Test fun bulkDeleteNamesTheExclusionFlagPerService() {
        val r = BulkEditor.deleteBody(ArrService.Radarr, listOf(2, 1), deleteFiles = true, exclude = false)
        assertEquals(listOf(1, 2), r.ints("movieIds"))
        assertEquals(true, r.b("deleteFiles"))
        assertEquals(false, r.b("addImportExclusion"))
        val s: JsonObject = BulkEditor.deleteBody(ArrService.Sonarr, listOf(1), deleteFiles = false, exclude = true)
        assertEquals(true, s.b("addImportListExclusion"))
        assertFalse(s.containsKey("addImportExclusion"))
    }

    @Test fun permissionBitsRoundTrip() {
        var mask = 0L
        SeerrPermissions.KNOWN.forEach { mask = SeerrPermissions.set(mask, it.value, true) }
        SeerrPermissions.KNOWN.forEach { assertTrue(SeerrPermissions.has(mask, it.value)) }
        SeerrPermissions.KNOWN.forEach { mask = SeerrPermissions.set(mask, it.value, false) }
        assertEquals(0L, mask)
        assertEquals(SeerrPermissions.KNOWN.size, SeerrPermissions.KNOWN.map { it.value }.toSet().size)
        assertTrue(SeerrPermissions.KNOWN.all { java.lang.Long.bitCount(it.value) == 1 })
    }

    @Test fun undocumentedBitsAreKept() {
        val unknown = 1L shl 30
        val mask = 32L or unknown
        assertEquals(listOf(30), SeerrPermissions.otherBits(mask))
        // Toggling a documented bit leaves the unknown one alone.
        val edited = SeerrPermissions.set(SeerrPermissions.set(mask, 128L, true), 32L, false)
        assertEquals(128L or unknown, edited)
        assertEquals(listOf("AUTO_APPROVE", "BIT_30"), SeerrPermissions.names(edited))
        assertEquals(listOf("REQUEST", "AUTO_APPROVE"), seerrPermissionNames(32 + 128))
    }

    @Test fun detectsLosingAdmin() {
        assertTrue(SeerrPermissions.losesAdmin(2L or 32L, 32L))
        assertFalse(SeerrPermissions.losesAdmin(32L, 0L))
        assertFalse(SeerrPermissions.losesAdmin(2L, 2L or 4L))
    }

    @Test fun mediaServerKindAndLibraryEnableQuery() {
        assertEquals(SeerrMediaServerKind.PLEX, SeerrMediaServerKind.fromType(1))
        assertEquals(SeerrMediaServerKind.ALT, SeerrMediaServerKind.fromType(3))
        assertNull(SeerrMediaServerKind.fromType(4))
        assertEquals(SeerrMediaServerKind.PLEX, SeerrMediaServerKind.fromAltDoc(jsonOf("ip" to "", "libraries" to emptyList<Any>())))
        assertEquals(SeerrMediaServerKind.ALT, SeerrMediaServerKind.fromAltDoc(jsonOf("ip" to "10.0.0.2")))
        val libs = listOf(jsonOf("id" to "1", "enabled" to true), jsonOf("id" to "2", "enabled" to false), jsonOf("id" to "3", "enabled" to true))
        assertEquals("1,2,3", SeerrMediaServerKind.enableQuery(libs, "2", true))
        assertEquals("3", SeerrMediaServerKind.enableQuery(libs, "1", false))
    }

    @Test fun warningsOnlyAllowsSaveAnyway() {
        assertTrue(warningsOnly(ArrException(400, "validation", "w", listOf(ArrValidation("p", "w", warning = true)))))
        assertFalse(warningsOnly(ArrException(400, "validation", "e", listOf(ArrValidation("p", "w", true), ArrValidation("q", "e", false)))))
        assertFalse(warningsOnly(ArrException(500, "upstream", "x")))
        assertFalse(warningsOnly(null))
    }
}
