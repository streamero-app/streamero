package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonObject
import net.scyde.streamero.api.json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LibraryKindsTest {
    private fun obj(s: String) = json.parseToJsonElement(s) as JsonObject

    @Test fun resourcesPerService() {
        assertEquals("movie", Lib.resource(ArrService.Radarr))
        assertEquals("series", Lib.resource(ArrService.Sonarr))
        assertEquals("artist", Lib.resource(ArrService.Lidarr))
        assertEquals("episodefile", Lib.filesResource(ArrService.Sonarr))
        assertEquals("history/artist", Lib.historyResource(ArrService.Lidarr))
        assertEquals(mapOf("includeSeries" to true, "includeEpisode" to true), Lib.include(ArrService.Sonarr))
    }

    @Test fun releaseQueries() {
        assertEquals(mapOf("movieId" to 3), Lib.releaseQuery(ArrService.Radarr, 3, null))
        assertEquals(mapOf("seriesId" to 3, "seasonNumber" to 2), Lib.releaseQuery(ArrService.Sonarr, 3, "season:2"))
        assertEquals(mapOf("episodeId" to 99), Lib.releaseQuery(ArrService.Sonarr, 3, "episode:99"))
        assertEquals(mapOf("albumId" to 7), Lib.releaseQuery(ArrService.Lidarr, 3, "album:7"))
        assertEquals(mapOf("artistId" to 3), Lib.releaseQuery(ArrService.Lidarr, 3, "album:x"))
    }

    @Test fun commands() {
        val (n, a) = Lib.search(ArrService.Radarr, 4)
        assertEquals("MoviesSearch", n)
        assertEquals(listOf(4), a.toMap()["movieIds"])
        assertEquals("SeriesSearch", Lib.search(ArrService.Sonarr, 4).first)
        assertEquals("EpisodeSearch", Lib.searchWanted(ArrService.Sonarr, listOf(1)).first)
        assertEquals("CutoffUnmetAlbumSearch", Lib.searchAllWanted(ArrService.Lidarr, true))
        assertEquals(null, Lib.rescan(ArrService.Lidarr, 1))
    }

    @Test fun completeness() {
        assertTrue(Lib.complete(ArrService.Radarr, obj("""{"hasFile":true}""")))
        assertFalse(Lib.complete(ArrService.Sonarr, obj("""{"statistics":{"percentOfEpisodes":50.0}}""")))
        assertTrue(Lib.complete(ArrService.Lidarr, obj("""{"statistics":{"percentOfTracks":100.0}}""")))
    }

    @Test fun recordTitles() {
        assertEquals("Severance · S02E03 · Who Is Alive?", Lib.recordTitle(ArrService.Sonarr,
            obj("""{"series":{"title":"Severance"},"episode":{"seasonNumber":2,"episodeNumber":3,"title":"Who Is Alive?"}}""")))
        assertEquals("Alien", Lib.recordTitle(ArrService.Radarr, obj("""{"movie":{"title":"Alien"},"sourceTitle":"Alien.1979.1080p"}""")))
        assertEquals("Björk · Homogenic", Lib.recordTitle(ArrService.Lidarr, obj("""{"artist":{"artistName":"Björk"},"album":{"title":"Homogenic"}}""")))
    }

    @Test fun seerrPermissions() {
        assertEquals(listOf("ADMIN"), seerrPermissionNames(2))
        assertEquals(listOf("REQUEST", "AUTO_APPROVE"), seerrPermissionNames(32 + 128))
        assertEquals(emptyList<String>(), seerrPermissionNames(0))
    }
}
