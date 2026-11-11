package net.scyde.streamero.offline

import kotlinx.coroutines.test.runTest
import net.scyde.streamero.api.ItemSummary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class OfflineStoreTest {
    @get:Rule val tmp = TemporaryFolder()

    private val sample = OfflineData(
        entries = listOf(
            OfflineEntry(
                itemId = "11111111-2222-3333-4444-555555555555", quality = Quality.P1080, jobId = "job-1", fileId = "f1",
                state = EntryState.DONE, transcodeProgress = 1.0, downloadedBytes = 10, totalBytes = 10, storedBytes = 42,
                file = "11111111-2222-3333-4444-555555555555/video.mp4", container = "mp4", durationMs = 60_000,
                subtitles = listOf(OfflineSubtitle(0, "hr", forced = true, file = "11111111-2222-3333-4444-555555555555/sub_0.srt")),
                poster = "11111111-2222-3333-4444-555555555555/poster.jpg", backdrop = null, seriesPoster = "x/series.jpg",
                item = ItemSummary("11111111-2222-3333-4444-555555555555", "episode", "Pilot", seasonNumber = 1, episodeNumber = 1, seriesId = "s", seriesTitle = "Show"),
                overview = "First", series = ItemSummary("s", "series", "Show", hasPoster = true), addedAt = 7, error = null,
                playback = LocalPlayback(1234, played = false, updatedAt = 99, dirty = true),
            ),
        ),
        subscriptions = listOf(AutoSubscription("s", "Show", 3, Quality.P720, 5)),
        wifiOnly = true,
    )

    @Test fun serializationRoundTrip() {
        assertEquals(sample, OfflineStore.decode(OfflineStore.encode(sample)))
    }

    @Test fun readsDataWithUnknownAndMissingFields() {
        val d = OfflineStore.decode("""{"entries":[{"itemId":"a","quality":"original","item":{"id":"a","kind":"movie","title":"A"},"future":1}]}""")
        assertEquals(EntryState.QUEUED, d.entries[0].state)
        assertFalse(d.wifiOnly)
    }

    @Test fun persistsAcrossInstances() = runTest {
        val root = tmp.newFolder("offline")
        val a = OfflineStore(root)
        a.update { sample }
        a.updateEntry(sample.entries[0].itemId) { it.copy(playback = it.playback.copy(positionMs = 5000)) }
        val b = OfflineStore(root)
        assertEquals(5000, b.data.value.entries[0].playback.positionMs)
        assertTrue(b.data.value.wifiOnly)
    }

    @Test fun inMemoryUpdatesAreNotWritten() = runTest {
        val root = tmp.newFolder("offline")
        val a = OfflineStore(root)
        a.update { sample }
        a.updateEntry(sample.entries[0].itemId, persist = false) { it.copy(downloadedBytes = 999) }
        assertEquals(999, a.entry(sample.entries[0].itemId)!!.downloadedBytes)
        assertEquals(10, OfflineStore(root).entry(sample.entries[0].itemId)!!.downloadedBytes)
    }

    @Test fun removeDeletesFolderAndEntry() = runTest {
        val root = tmp.newFolder("offline")
        val s = OfflineStore(root)
        s.update { sample }
        val id = sample.entries[0].itemId
        s.dirFor(id).mkdirs()
        File(s.dirFor(id), "video.mp4").writeBytes(ByteArray(100))
        assertEquals(100, s.bytesOnDisk(id))
        s.remove(listOf(id))
        assertNull(s.entry(id))
        assertFalse(s.dirFor(id).exists())
    }

    @Test fun corruptIndexStartsEmptyAndIsKept() {
        val root = tmp.newFolder("offline")
        File(root, "index.json").writeText("{not json")
        val s = OfflineStore(root)
        assertTrue(s.data.value.entries.isEmpty())
        assertTrue(File(root, "index.broken.json").exists())
    }

    @Test fun pathsStayInsideTheRoot() {
        val root = tmp.newFolder("offline")
        val s = OfflineStore(root)
        assertNull(s.file("../outside.txt"))
        assertNull(s.file(null))
        assertEquals(File(root, "a/video.mp4").canonicalFile, s.file("a/video.mp4"))
        assertEquals("abc-1", OfflineStore.safeName("../abc-1/"))
    }
}
