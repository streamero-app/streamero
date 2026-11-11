package net.scyde.streamero.offline

import net.scyde.streamero.api.ItemSummary
import net.scyde.streamero.api.OfflineItemState
import net.scyde.streamero.api.OfflineSyncResponse
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Locale

class OfflineSyncTest {
    private val t0 = parseTime("2026-11-11T10:00:00Z")

    private fun entry(id: String, pb: LocalPlayback = LocalPlayback(), state: String = EntryState.DONE, kind: String = "movie", series: String? = null, dur: Long = 100_000) =
        OfflineEntry(itemId = id, quality = Quality.P720, state = state, durationMs = dur, playback = pb,
            item = ItemSummary(id = id, kind = kind, title = id, seriesId = series, durationMs = dur))

    private fun server(id: String, pos: Long, played: Boolean, at: Long) = OfflineItemState(id, pos, played, isoTime(at))

    @Test fun parsesUtcAndOffsetTimes() {
        assertEquals(t0, parseTime("2026-11-11T11:00:00+01:00"))
        assertEquals(t0 + 123, parseTime("2026-11-11T10:00:00.123Z"))
        assertEquals(0, parseTime("not a time"))
        assertEquals(0, parseTime(null))
    }

    @Test fun requestListsEveryItemAndOnlyDirtyProgress() {
        val r = OfflineSync.request(listOf(
            entry("a", LocalPlayback(5000, false, t0, dirty = true)),
            entry("b", LocalPlayback(7000, false, t0, dirty = false)),
            entry("c", state = EntryState.DOWNLOADING),
        ))
        assertEquals(listOf("a", "b", "c"), r.itemIds)
        assertEquals(1, r.progress.size)
        assertEquals("a", r.progress[0].itemId)
        assertEquals(5000, r.progress[0].positionMs)
        assertEquals(t0, parseTime(r.progress[0].updatedAt))
        assertEquals(100_000L, r.progress[0].durationMs)
    }

    @Test fun revokedItemsAreRemoved() {
        val entries = listOf(entry("a"), entry("b"))
        val plan = OfflineSync.plan(entries, emptyList(), OfflineSyncResponse(revoked = listOf("b", "unknown")))
        assertEquals(setOf("b"), plan.revoked)
        val after = OfflineSync.apply(OfflineData(entries = entries), plan)
        assertEquals(listOf("a"), after.entries.map { it.itemId })
    }

    @Test fun acceptedReportClearsDirty() {
        val mine = LocalPlayback(5000, false, t0, dirty = true)
        val entries = listOf(entry("a", mine))
        val req = OfflineSync.request(entries)
        val plan = OfflineSync.plan(entries, req.progress, OfflineSyncResponse(items = listOf(server("a", 5000, false, t0)), applied = 1))
        val pb = plan.playback.getValue("a")
        assertFalse(pb.dirty)
        assertEquals(5000, pb.positionMs)
    }

    @Test fun deviceClockAheadIsStillAccepted() {
        // The server stores a time from the future as its "now", earlier than the device's time.
        val mine = LocalPlayback(5000, true, t0 + 60_000, dirty = true)
        val entries = listOf(entry("a", mine))
        val req = OfflineSync.request(entries)
        val plan = OfflineSync.plan(entries, req.progress, OfflineSyncResponse(items = listOf(server("a", 5000, true, t0)), applied = 1))
        assertFalse(plan.playback.getValue("a").dirty)
    }

    @Test fun newerServerStateWinsOverDirtyLocal() {
        // Watched further on another device after the offline viewing: the server keeps that.
        val mine = LocalPlayback(5000, false, t0, dirty = true)
        val entries = listOf(entry("a", mine))
        val req = OfflineSync.request(entries)
        val plan = OfflineSync.plan(entries, req.progress, OfflineSyncResponse(items = listOf(server("a", 80_000, false, t0 + 10_000)), applied = 0))
        val pb = plan.playback.getValue("a")
        assertEquals(80_000, pb.positionMs)
        assertEquals(t0 + 10_000, pb.updatedAt)
        assertFalse(pb.dirty)
    }

    @Test fun newerServerStateReplacesCleanLocal() {
        val entries = listOf(entry("a", LocalPlayback(1000, false, t0)))
        val plan = OfflineSync.plan(entries, emptyList(), OfflineSyncResponse(items = listOf(server("a", 90_000, true, t0 + 1))))
        assertEquals(LocalPlayback(90_000, true, t0 + 1, false), plan.playback["a"])
    }

    @Test fun olderServerStateIsIgnored() {
        val entries = listOf(entry("a", LocalPlayback(1000, false, t0)))
        val plan = OfflineSync.plan(entries, emptyList(), OfflineSyncResponse(items = listOf(server("a", 90_000, true, t0 - 1))))
        assertNull(plan.playback["a"])
    }

    @Test fun progressRecordedDuringSyncStaysDirty() {
        val sentState = LocalPlayback(5000, false, t0, dirty = true)
        val req = OfflineSync.request(listOf(entry("a", sentState)))
        // Meanwhile the player saved a newer position.
        val now = listOf(entry("a", LocalPlayback(9000, false, t0 + 5000, dirty = true)))
        val plan = OfflineSync.plan(now, req.progress, OfflineSyncResponse(items = listOf(server("a", 5000, false, t0)), applied = 1))
        assertNull(plan.playback["a"])
    }

    @Test fun reportOfNothingWithoutServerStateIsDone() {
        val entries = listOf(entry("a", LocalPlayback(0, false, t0, dirty = true)))
        val req = OfflineSync.request(entries)
        val plan = OfflineSync.plan(entries, req.progress, OfflineSyncResponse())
        assertFalse(plan.playback.getValue("a").dirty)
    }

    @Test fun recordMarksPlayedPastNinetyPercent() {
        val e = entry("a", dur = 100_000)
        val early = OfflineSync.record(e, 50_000, null, t0)
        assertFalse(early.played); assertTrue(early.dirty); assertEquals(t0, early.updatedAt)
        val late = OfflineSync.record(e, 91_000, 100_000, t0)
        assertTrue(late.played)
        // Watching again keeps it watched.
        assertTrue(OfflineSync.record(e.copy(playback = late), 10_000, 100_000, t0 + 1).played)
    }

    @Test fun startPositionResumesUnlessFinished() {
        assertEquals(40_000, OfflineSync.startPosition(entry("a", LocalPlayback(40_000))))
        assertEquals(0, OfflineSync.startPosition(entry("a", LocalPlayback(95_000, true))))
    }

    @Test fun autoDownloadsDeleteWatchedAndSkipTheRest() {
        val sub = AutoSubscription("s1", "Show", 3, Quality.P720)
        val entries = listOf(
            entry("e1", LocalPlayback(0, true), kind = "episode", series = "s1"),
            entry("e2", kind = "episode", series = "s1"),
            entry("e3", state = EntryState.DOWNLOADING, kind = "episode", series = "s1"),
            entry("x1", LocalPlayback(0, true), kind = "episode", series = "other"),
            entry("m", LocalPlayback(0, true)),
        )
        val plan = AutoDownloads.plan(entries, sub)
        assertEquals(listOf("e1"), plan.delete)
        assertEquals(listOf("e2", "e3"), plan.skip)
    }

    @Test fun storageAccounting() {
        val done = entry("a").copy(storedBytes = 1_500_000, totalBytes = 1_400_000)
        val partial = entry("b", state = EntryState.DOWNLOADING).copy(downloadedBytes = 500_000, totalBytes = 2_000_000)
        val queued = entry("c", state = EntryState.QUEUED)
        assertEquals(1_500_000, Storage.entryBytes(done))
        assertEquals(500_000, Storage.entryBytes(partial))
        assertEquals(0, Storage.entryBytes(queued))
        assertEquals(2_000_000, Storage.totalBytes(listOf(done, partial, queued)))
        assertEquals(0.25f, partial.phaseProgress, 0.0001f)
    }

    @Test fun formatsSizes() {
        assertEquals("0 B", Storage.format(0, Locale.US))
        assertEquals("512 B", Storage.format(512, Locale.US))
        assertEquals("1.5 KB", Storage.format(1536, Locale.US))
        assertEquals("12 MB", Storage.format(12L * 1024 * 1024, Locale.US))
        assertEquals("3.4 GB", Storage.format((3.4 * 1024 * 1024 * 1024).toLong(), Locale.US))
        assertEquals("3,4 GB", Storage.format((3.4 * 1024 * 1024 * 1024).toLong(), Locale.forLanguageTag("hr")))
    }

    @Test fun groupsFilmsAndSeriesEpisodesInOrder() {
        fun ep(id: String, s: Int, n: Int) = entry(id, kind = "episode", series = "s1").let {
            it.copy(item = it.item.copy(seasonNumber = s, episodeNumber = n, seriesTitle = "Beta"))
        }
        val groups = Storage.groups(listOf(ep("e3", 2, 1), entry("zeta").let { it.copy(item = it.item.copy(title = "Zeta")) }, ep("e1", 1, 1), ep("e2", 1, 2),
            entry("alpha").let { it.copy(item = it.item.copy(title = "Alpha")) }))
        assertEquals(listOf("Alpha", "Beta", "Zeta"), groups.map { it.title })
        assertEquals(listOf("e1", "e2", "e3"), groups[1].entries.map { it.itemId })
    }
}
