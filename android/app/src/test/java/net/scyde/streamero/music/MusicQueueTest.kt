package net.scyde.streamero.music

import java.io.IOException
import kotlinx.coroutines.delay
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class MusicQueueTest {
    private val tracks = listOf("a", "b", "c", "d", "e")

    @Test fun theChosenTrackIsResolvedAlone() = runTest {
        val asked = mutableListOf<String>()
        val (at, first) = MusicQueue.first(tracks, 2) { t -> asked += t; "url-$t" }!!
        assertEquals(2, at)
        assertEquals("url-c", first.stream)
        assertEquals(listOf("c"), asked)
    }

    @Test fun anUnplayableChosenTrackMovesOnToTheNext() = runTest {
        val (at, first) = MusicQueue.first(tracks, 1) { t -> if (t == "b") throw IOException("gone") else if (t == "c") null else "url-$t" }!!
        assertEquals(3, at)
        assertEquals("d", first.track)
        assertNull(MusicQueue.first(tracks, 0) { null })
    }

    @Test fun theRestKeepsOrderAndDropsFailures() = runTest {
        val (before, after) = MusicQueue.rest(tracks, 2, parallel = 2) { t ->
            delay(if (t == "a") 50 else 1) // finishing out of order must not reorder the queue
            if (t == "d") throw IllegalStateException("bad") else "url-$t"
        }
        assertEquals(listOf("a", "b"), before.map { it.track })
        assertEquals(listOf("e"), after.map { it.track })
    }
}
