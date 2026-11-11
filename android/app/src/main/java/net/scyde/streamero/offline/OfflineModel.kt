package net.scyde.streamero.offline

import kotlinx.serialization.Serializable
import net.scyde.streamero.api.ItemSummary
import net.scyde.streamero.api.OfflineProgress
import net.scyde.streamero.api.OfflineSyncRequest
import net.scyde.streamero.api.OfflineSyncResponse
import java.time.Instant
import java.time.OffsetDateTime
import java.util.Locale

// The device's offline library. Everything here is plain Kotlin (no Android types) so the
// sync and storage rules are unit-tested on the JVM.

object Quality {
    const val ORIGINAL = "original"
    const val SOURCE = "source"
    const val P1080 = "1080p"
    const val P720 = "720p"
    val all = listOf(ORIGINAL, SOURCE, P1080, P720)
    /** Qualities the server transcodes (they need the videoTranscoding permission). */
    val transcoded = setOf(SOURCE, P1080, P720)
}

object EntryState {
    const val QUEUED = "queued"
    const val TRANSCODING = "transcoding"
    const val DOWNLOADING = "downloading"
    const val DONE = "done"
    const val FAILED = "failed"

    /** Maps a server job state (queued|transcoding|ready|failed) to the local one. */
    fun fromJob(state: String): String = when (state) {
        "transcoding" -> TRANSCODING
        "ready" -> DOWNLOADING
        "failed" -> FAILED
        else -> QUEUED
    }
}

/** Playback state recorded on the device; `dirty` until the server has confirmed it. */
@Serializable data class LocalPlayback(
    val positionMs: Long = 0, val played: Boolean = false, val updatedAt: Long = 0, val dirty: Boolean = false,
)

/** A subtitle sidecar next to the video (paths are relative to the offline folder). */
@Serializable data class OfflineSubtitle(val index: Int, val language: String = "", val forced: Boolean = false, val file: String)

/** One downloaded (or downloading) film or episode. */
@Serializable data class OfflineEntry(
    val itemId: String,
    val quality: String,
    val jobId: String? = null,
    val fileId: String? = null,
    val state: String = EntryState.QUEUED,
    /** Server transcode progress 0–1. */
    val transcodeProgress: Double = 0.0,
    val downloadedBytes: Long = 0,
    val totalBytes: Long = 0,
    /** Everything this entry keeps on disk (video, sidecars, artwork) once done. */
    val storedBytes: Long = 0,
    val file: String? = null,
    val container: String? = null,
    val durationMs: Long? = null,
    val subtitles: List<OfflineSubtitle> = emptyList(),
    val poster: String? = null,
    val backdrop: String? = null,
    val seriesPoster: String? = null,
    val item: ItemSummary,
    val overview: String? = null,
    val series: ItemSummary? = null,
    val addedAt: Long = 0,
    val error: String? = null,
    val playback: LocalPlayback = LocalPlayback(),
) {
    val done: Boolean get() = state == EntryState.DONE
    val seriesId: String? get() = series?.id ?: item.seriesId
    /** 0–1 for the current phase (transcoding, then downloading). */
    val phaseProgress: Float get() = when (state) {
        EntryState.TRANSCODING -> transcodeProgress.toFloat().coerceIn(0f, 1f)
        EntryState.DOWNLOADING -> if (totalBytes > 0) (downloadedBytes.toFloat() / totalBytes).coerceIn(0f, 1f) else 0f
        EntryState.DONE -> 1f
        else -> 0f
    }
}

/** "Keep the next N episodes of this series downloaded." */
@Serializable data class AutoSubscription(val seriesId: String, val title: String, val count: Int, val quality: String, val addedAt: Long = 0)

@Serializable data class OfflineData(
    val version: Int = 1,
    val entries: List<OfflineEntry> = emptyList(),
    val subscriptions: List<AutoSubscription> = emptyList(),
    val wifiOnly: Boolean = false,
) {
    fun entry(itemId: String): OfflineEntry? = entries.firstOrNull { it.itemId == itemId }
    fun subscription(seriesId: String): AutoSubscription? = subscriptions.firstOrNull { it.seriesId == seriesId }
}

/** Past this share of the running time an item counts as watched. */
const val PLAYED_SHARE = 0.9

fun isPlayed(positionMs: Long, durationMs: Long?): Boolean =
    durationMs != null && durationMs > 0 && positionMs >= durationMs * PLAYED_SHARE

fun isoTime(epochMs: Long): String = Instant.ofEpochMilli(epochMs).toString()

/** Epoch milliseconds of an ISO-8601 time (with Z or an offset); 0 when unreadable. */
fun parseTime(s: String?): Long {
    if (s.isNullOrBlank()) return 0
    return runCatching { OffsetDateTime.parse(s).toInstant().toEpochMilli() }
        .recoverCatching { Instant.parse(s).toEpochMilli() }
        .getOrDefault(0)
}

/**
 * Offline progress sync (POST /downloads/sync). Newest wins: the server applies a report only
 * when it is newer than its own state, and the device takes the server's state when that is newer.
 */
object OfflineSync {
    /** All items kept offline (so revoked ones come back) and the progress not yet confirmed. */
    fun request(entries: List<OfflineEntry>): OfflineSyncRequest = OfflineSyncRequest(
        itemIds = entries.map { it.itemId }.distinct(),
        progress = entries.filter { it.playback.dirty }.map {
            OfflineProgress(it.itemId, it.playback.positionMs.coerceAtLeast(0), it.durationMs ?: it.item.durationMs, it.playback.played, isoTime(it.playback.updatedAt))
        },
    )

    data class Plan(
        /** Items the profile may no longer keep: delete their files. */
        val revoked: Set<String>,
        /** New local playback state per item (only items that change). */
        val playback: Map<String, LocalPlayback>,
    )

    /**
     * What to change locally after a sync. `entries` is the state now (it may have moved on
     * while the request was in flight), `sent` the progress that went out.
     */
    fun plan(entries: List<OfflineEntry>, sent: List<OfflineProgress>, resp: OfflineSyncResponse): Plan {
        val local = entries.associateBy { it.itemId }
        val revoked = resp.revoked.filter { it in local }.toSet()
        val server = resp.items.associateBy { it.itemId }
        val sentBy = sent.associateBy { it.itemId }
        val out = mutableMapOf<String, LocalPlayback>()
        for (e in entries) {
            if (e.itemId in revoked) continue
            val mine = e.playback
            val s = server[e.itemId]
            val p = sentBy[e.itemId]
            val sentAt = p?.let { parseTime(it.updatedAt) }
            // Played again on the device since the report went out: that is newer than anything
            // the server could have answered, keep it for the next sync.
            val changedSince = mine.dirty && sentAt != null && mine.updatedAt > sentAt
            if (s == null) {
                // The server keeps no state for never-played items; a report of "nothing" is done.
                if (mine.dirty && p != null && !changedSince) out[e.itemId] = mine.copy(dirty = false)
                continue
            }
            val serverAt = parseTime(s.updatedAt)
            val serverState = LocalPlayback(s.positionMs, s.played, serverAt, dirty = false)
            if (mine.dirty) {
                if (changedSince) continue
                // Accepted (the server now holds our time or a newer one, or exactly our values:
                // a device clock ahead of the server's is stored as the server's "now").
                val accepted = p != null && (serverAt >= sentAt!! || (s.positionMs == p.positionMs && s.played == p.played))
                when {
                    accepted -> out[e.itemId] = serverState.copy(updatedAt = maxOf(serverAt, mine.updatedAt))
                    serverAt > mine.updatedAt -> out[e.itemId] = serverState
                }
            } else if (serverAt > mine.updatedAt) {
                out[e.itemId] = serverState
            }
        }
        return Plan(revoked, out)
    }

    /** The data after a plan: revoked entries gone, playback states replaced. */
    fun apply(data: OfflineData, plan: Plan): OfflineData = data.copy(
        entries = data.entries.filter { it.itemId !in plan.revoked }.map { e -> plan.playback[e.itemId]?.let { e.copy(playback = it) } ?: e },
    )

    /** Local playback after watching on the device (dirty until the server confirms). */
    fun record(e: OfflineEntry, positionMs: Long, durationMs: Long?, now: Long): LocalPlayback {
        val dur = durationMs?.takeIf { it > 0 } ?: e.durationMs ?: e.item.durationMs
        // Watching an item again keeps it watched; past 90% marks it.
        return LocalPlayback(positionMs.coerceAtLeast(0), e.playback.played || isPlayed(positionMs, dur), now, dirty = true)
    }

    /** Where offline playback starts: the saved position, or the beginning when it was finished. */
    fun startPosition(e: OfflineEntry): Long {
        val dur = e.durationMs ?: e.item.durationMs
        val pos = e.playback.positionMs
        return if (isPlayed(pos, dur) || (dur != null && pos >= dur - 5_000)) 0 else pos
    }
}

/** Keeping the next N episodes of a series: which local episodes go, which the server should skip. */
object AutoDownloads {
    data class Plan(val delete: List<String>, val skip: List<String>)

    fun plan(entries: List<OfflineEntry>, sub: AutoSubscription): Plan {
        val episodes = entries.filter { it.item.kind == "episode" && it.seriesId == sub.seriesId }
        val delete = episodes.filter { it.done && it.playback.played }.map { it.itemId }
        return Plan(delete, episodes.map { it.itemId } - delete.toSet())
    }
}

/** A row of the offline library: a film, or a series with its episodes. */
data class OfflineGroup(val key: String, val title: String, val series: ItemSummary?, val entries: List<OfflineEntry>) {
    val bytes: Long get() = entries.sumOf { Storage.entryBytes(it) }
}

object Storage {
    /** What an entry takes on the device now. */
    fun entryBytes(e: OfflineEntry): Long =
        if (e.done) e.storedBytes.takeIf { it > 0 } ?: e.totalBytes else e.downloadedBytes

    fun totalBytes(entries: List<OfflineEntry>): Long = entries.sumOf { entryBytes(it) }

    /** "0 B", "512 B", "1.5 KB", "12 MB", "3.4 GB" (decimal separator of the locale). */
    fun format(bytes: Long, locale: Locale = Locale.getDefault()): String {
        if (bytes < 1024) return "${bytes.coerceAtLeast(0)} B"
        val units = listOf("KB", "MB", "GB", "TB")
        var v = bytes / 1024.0
        var i = 0
        while (v >= 1024 && i < units.size - 1) { v /= 1024; i++ }
        return if (v < 10) String.format(locale, "%.1f %s", v, units[i]) else String.format(locale, "%.0f %s", v, units[i])
    }

    /** Films one by one, episodes under their series (in season and episode order), by title. */
    fun groups(entries: List<OfflineEntry>): List<OfflineGroup> {
        val (episodes, others) = entries.partition { it.item.kind == "episode" && it.seriesId != null }
        val series = episodes.groupBy { it.seriesId!! }.map { (sid, eps) ->
            val s = eps.firstNotNullOfOrNull { it.series }
            OfflineGroup("s:$sid", s?.title ?: eps.first().item.seriesTitle ?: eps.first().item.title, s,
                eps.sortedWith(compareBy({ it.item.seasonNumber ?: 0 }, { it.item.episodeNumber ?: 0 })))
        }
        val films = others.map { OfflineGroup("i:${it.itemId}", it.item.title, null, listOf(it)) }
        return (series + films).sortedBy { it.title.lowercase() }
    }
}
