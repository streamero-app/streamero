package net.scyde.streamero.sync

import net.scyde.streamero.api.SyncGroup
import net.scyde.streamero.api.SyncMember
import net.scyde.streamero.api.SyncQueueEntry
import org.junit.Assert.assertEquals
import org.junit.Test

class SyncRejoinTest {
    private val entry = SyncQueueEntry("e1", "item1", "Film", 7_200_000)

    private fun group(id: String, version: Long = 1) = SyncGroup(
        id = id, name = "Movie night", state = "waiting", queue = listOf(entry), index = 0, positionMs = 0, version = version, selfId = "me",
        members = listOf(SyncMember("me", "Ana", connected = true), SyncMember("o", "Ivo", connected = true)),
    )

    private val atStart = PlayerSnapshot("e1", prepared = true, positionMs = 0, playing = false, buffering = false)

    private fun readies(e: SyncEngine) = e.step(atStart, 1_000).messages.count { it.type == "ready" }

    @Test fun aNewGroupAfterLeavingGetsItsReadyReport() {
        val e = SyncEngine()
        e.onState(group("g"))
        assertEquals(1, readies(e))
        e.onState(null)
        // Versions start again in the new group: the old group's "already sent" must not hold it back.
        e.onState(group("h"))
        assertEquals(1, readies(e))
    }

    @Test fun switchingStraightToAnotherGroupAlsoStartsFresh() {
        val e = SyncEngine()
        e.onState(group("g"))
        assertEquals(1, readies(e))
        e.onState(group("h"))
        assertEquals(1, readies(e))
    }

    @Test fun theSameGroupDoesNotRepeatItself() {
        val e = SyncEngine()
        e.onState(group("g"))
        assertEquals(1, readies(e))
        e.onState(group("g"))
        assertEquals(0, readies(e))
    }
}
