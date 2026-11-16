package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonObject
import net.scyde.streamero.api.json
import org.junit.Assert.assertEquals
import org.junit.Test

class QualityProfilesTest {
    // items[] lowest → highest, as the *arr quality profile API returns them; one group (id 1000).
    private val profile = json.parseToJsonElement("""
        {"id":1,"name":"HD","upgradeAllowed":true,"cutoff":7,"minFormatScore":0,
         "items":[
          {"quality":{"id":1,"name":"SDTV"},"items":[],"allowed":false},
          {"quality":{"id":4,"name":"HDTV-720p"},"items":[],"allowed":true},
          {"id":1000,"name":"WEB 1080p","items":[{"quality":{"id":3,"name":"WEBDL-1080p"},"allowed":true},{"quality":{"id":15,"name":"WEBRip-1080p"},"allowed":true}],"allowed":true},
          {"quality":{"id":7,"name":"Bluray-1080p"},"items":[],"allowed":true}
         ],
         "formatItems":[{"format":5,"name":"x265","score":0},{"format":6,"name":"HDR","score":10}]}
    """) as JsonObject

    private fun names(p: JsonObject) = QualityProfiles.display(p).map(QualityProfiles::itemName)

    @Test fun showsHighestFirst() {
        assertEquals(listOf("Bluray-1080p", "WEB 1080p", "HDTV-720p", "SDTV"), names(profile))
        assertEquals(1000, QualityProfiles.itemId(QualityProfiles.display(profile)[1]))
        assertEquals(7, QualityProfiles.itemId(QualityProfiles.display(profile)[0]))
    }

    @Test fun movesRowsAndKeepsTheStoredOrderLowestFirst() {
        val moved = QualityProfiles.move(profile, 0, 2) // Bluray below HDTV
        assertEquals(listOf("WEB 1080p", "HDTV-720p", "Bluray-1080p", "SDTV"), names(moved))
        assertEquals(listOf("SDTV", "Bluray-1080p", "HDTV-720p", "WEB 1080p"), QualityProfiles.items(moved).map(QualityProfiles::itemName))
        // The group keeps its nested qualities.
        assertEquals(2, QualityProfiles.items(moved).last().objs("items").size)
        // Out of range or no-op moves change nothing.
        assertEquals(profile, QualityProfiles.move(profile, 0, -1))
        assertEquals(profile, QualityProfiles.move(profile, 3, 4))
        assertEquals(profile, QualityProfiles.move(profile, 1, 1))
        // Everything else of the profile stays.
        assertEquals("HD", moved.s("name"))
    }

    @Test fun cutoffOnlyOnAllowedItems() {
        assertEquals(listOf(7 to "Bluray-1080p", 1000 to "WEB 1080p", 4 to "HDTV-720p"), QualityProfiles.cutoffChoices(profile))
        // Disallowing the cutoff moves it to the highest allowed item.
        val p = QualityProfiles.setAllowed(profile, 0, false)
        assertEquals(1000, p.i("cutoff"))
        assertEquals(false, QualityProfiles.display(p)[0].b("allowed"))
        // Allowing another one keeps a valid cutoff.
        assertEquals(7, QualityProfiles.setAllowed(profile, 3, true).i("cutoff"))
    }

    @Test fun setsCustomFormatScores() {
        val p = QualityProfiles.setScore(profile, 5, -50)
        assertEquals(listOf(-50, 10), p.objs("formatItems").map { it.i("score") })
    }
}
