package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonObject

/**
 * What differs between Radarr (movies), Sonarr (series) and Lidarr (artists) in their public
 * APIs; everything else (queue, history, wanted, system, settings) has the same shape.
 */
object Lib {
    fun resource(s: ArrService) = when (s) { ArrService.Sonarr -> "series"; ArrService.Lidarr -> "artist"; else -> "movie" }
    fun idParam(s: ArrService) = when (s) { ArrService.Sonarr -> "seriesId"; ArrService.Lidarr -> "artistId"; else -> "movieId" }
    fun filesResource(s: ArrService) = when (s) { ArrService.Sonarr -> "episodefile"; ArrService.Lidarr -> "trackfile"; else -> "moviefile" }
    fun historyResource(s: ArrService) = "history/" + resource(s)

    /** Query flags that make queue/history/wanted records carry their movie/series/episode/artist/album. */
    fun include(s: ArrService): Map<String, Any?> = when (s) {
        ArrService.Sonarr -> mapOf("includeSeries" to true, "includeEpisode" to true)
        ArrService.Lidarr -> mapOf("includeArtist" to true, "includeAlbum" to true)
        else -> mapOf("includeMovie" to true)
    }

    fun title(o: JsonObject): String = o.s("title") ?: o.s("artistName") ?: o.s("albumTitle") ?: o.s("sourceTitle") ?: "?"

    /** The poster of a library item or lookup result (through the proxy, or the remote one for lookups). */
    fun poster(c: ArrClient, o: JsonObject): String? {
        val img = o.cover("poster") ?: o.cover("cover") ?: o.objs("images").firstOrNull()
        return c.imageUrl(img?.s("url")) ?: img?.s("remoteUrl")
    }

    fun sizeOnDisk(o: JsonObject): Long? = o.l("sizeOnDisk") ?: o.o("statistics")?.l("sizeOnDisk")

    /** Has everything it should (movie file present, all episodes/tracks). */
    fun complete(s: ArrService, o: JsonObject): Boolean = when (s) {
        ArrService.Radarr -> o.b("hasFile") == true
        ArrService.Sonarr -> (o.o("statistics")?.d("percentOfEpisodes") ?: 0.0) >= 100.0
        else -> (o.o("statistics")?.d("percentOfTracks") ?: 0.0) >= 100.0
    }

    fun refresh(s: ArrService, id: Int): Pair<String, Array<Pair<String, Any?>>> = when (s) {
        ArrService.Sonarr -> "RefreshSeries" to arrayOf("seriesId" to id)
        ArrService.Lidarr -> "RefreshArtist" to arrayOf("artistId" to id)
        else -> "RefreshMovie" to arrayOf("movieIds" to listOf(id))
    }

    fun rescan(s: ArrService, id: Int): Pair<String, Array<Pair<String, Any?>>>? = when (s) {
        ArrService.Sonarr -> "RescanSeries" to arrayOf("seriesId" to id)
        ArrService.Radarr -> "RescanMovie" to arrayOf("movieId" to id)
        else -> null
    }

    fun search(s: ArrService, id: Int): Pair<String, Array<Pair<String, Any?>>> = when (s) {
        ArrService.Sonarr -> "SeriesSearch" to arrayOf("seriesId" to id)
        ArrService.Lidarr -> "ArtistSearch" to arrayOf("artistId" to id)
        else -> "MoviesSearch" to arrayOf("movieIds" to listOf(id))
    }

    /** Search for wanted records (movies, episodes or albums) by their ids. */
    fun searchWanted(s: ArrService, ids: List<Int>): Pair<String, Array<Pair<String, Any?>>> = when (s) {
        ArrService.Sonarr -> "EpisodeSearch" to arrayOf("episodeIds" to ids)
        ArrService.Lidarr -> "AlbumSearch" to arrayOf("albumIds" to ids)
        else -> "MoviesSearch" to arrayOf("movieIds" to ids)
    }

    fun searchAllWanted(s: ArrService, cutoff: Boolean): String = when (s) {
        ArrService.Sonarr -> if (cutoff) "CutoffUnmetEpisodeSearch" else "MissingEpisodeSearch"
        ArrService.Lidarr -> if (cutoff) "CutoffUnmetAlbumSearch" else "MissingAlbumSearch"
        else -> if (cutoff) "CutOffUnmetMoviesSearch" else "MissingMoviesSearch"
    }

    /** `addOptions.monitor` values the services document. */
    fun monitorOptions(s: ArrService): List<String> = when (s) {
        ArrService.Sonarr -> listOf("all", "future", "missing", "existing", "recent", "pilot", "firstSeason", "lastSeason", "monitorSpecials", "unmonitorSpecials", "none")
        ArrService.Lidarr -> listOf("all", "future", "missing", "existing", "first", "latest", "none")
        else -> listOf("movieOnly", "movieAndCollection", "none")
    }

    /** The release-search query for an item, a season (`season:N`), an episode (`episode:ID`) or an album (`album:ID`). */
    fun releaseQuery(s: ArrService, id: Int, scope: String?): Map<String, Any?> {
        val kind = scope?.substringBefore(':')
        val v = scope?.substringAfter(':', "")?.toIntOrNull()
        return when {
            kind == "season" && v != null -> mapOf("seriesId" to id, "seasonNumber" to v)
            kind == "episode" && v != null -> mapOf("episodeId" to v)
            kind == "album" && v != null -> mapOf("albumId" to v)
            else -> mapOf(idParam(s) to id)
        }
    }

    /** The record a queue/history/wanted row is about, as a line of text. */
    fun recordTitle(s: ArrService, r: JsonObject): String {
        val main = r.o("movie") ?: r.o("series") ?: r.o("artist")
        val ep = r.o("episode")
        val album = r.o("album")
        return when (s) {
            ArrService.Sonarr -> {
                val e = ep ?: r.takeIf { it.containsKey("episodeNumber") }
                val series = main?.let(::title) ?: r.o("series")?.let(::title)
                if (e != null) listOfNotNull(series, "S%02dE%02d".format(e.i("seasonNumber") ?: 0, e.i("episodeNumber") ?: 0), e.s("title")).joinToString(" · ")
                else series ?: r.s("title") ?: r.s("sourceTitle") ?: "?"
            }
            ArrService.Lidarr -> listOfNotNull(main?.let(::title), (album ?: r.takeIf { it.containsKey("albumType") })?.s("title")).joinToString(" · ").ifBlank { r.s("sourceTitle") ?: "?" }
            else -> main?.let(::title) ?: r.s("title") ?: r.s("sourceTitle") ?: "?"
        }
    }
}
