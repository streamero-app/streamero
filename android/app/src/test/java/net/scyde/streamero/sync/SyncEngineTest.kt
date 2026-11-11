package net.scyde.streamero.sync

import net.scyde.streamero.api.SyncClientMessage
import net.scyde.streamero.api.SyncCorrection
import net.scyde.streamero.api.SyncGroup
import net.scyde.streamero.api.SyncMember
import net.scyde.streamero.api.SyncQueueEntry
import net.scyde.streamero.api.SyncServerMessage
import net.scyde.streamero.api.json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SyncEngineTest {
    private val e1 = SyncQueueEntry("e1", "item1", "Film", 7_200_000)
    private val e2 = SyncQueueEntry("e2", "item2", "Episode 2", 1_800_000)

    private fun group(
        state: String = "waiting", positionMs: Long = 0, anchorAt: Long = 0, speed: Double = 1.0, version: Long = 1,
        index: Int = 0, ready: Boolean = false, buffering: Boolean = false,
    ) = SyncGroup(
        id = "g", name = "Movie night", state = state, queue = listOf(e1, e2), index = index, positionMs = positionMs,
        anchorAt = anchorAt, speed = speed, version = version, selfId = "me",
        members = listOf(SyncMember("me", "Ana", ready = ready, buffering = buffering, connected = true), SyncMember("o", "Ivo", connected = true)),
    )

    private fun snap(pos: Long, entry: String? = "e1", playing: Boolean = false, buffering: Boolean = false, prepared: Boolean = true, speed: Double = 1.0, ended: Boolean = false) =
        PlayerSnapshot(entry, prepared, pos, playing, buffering, ended, speed)

    private fun engine(g: SyncGroup) = SyncEngine().apply { onState(g) } // clock offset 0: local time == server time

    private fun List<SyncClientMessage>.types() = map { it.type }

    @Test fun clockTakesTheSampleWithTheSmallestRoundTrip() {
        val c = ClockSync()
        c.onPong(clientTime = 1000, serverTime = 6000, receivedAt = 1400) // rtt 400, offset 4800
        c.onPong(clientTime = 2000, serverTime = 7050, receivedAt = 2100) // rtt 100, offset 5000
        c.onPong(clientTime = 3000, serverTime = 7900, receivedAt = 3300) // rtt 300, offset 4750
        assertEquals(5000L, c.offsetMs)
        assertEquals(15_000L, c.serverNow(10_000))
    }

    @Test fun clockForgetsOldSamplesAndIgnoresImpossibleOnes() {
        val c = ClockSync(window = 2)
        c.onPong(0, 1000, 10) // rtt 10, offset 995
        c.onPong(100, 2000, 350) // rtt 250
        c.onPong(400, 3000, 600) // rtt 200: the rtt-10 sample is out of the window
        assertEquals(3000L - 500, c.offsetMs)
        c.onPong(1000, 9_999_999, 900) // answered before it was asked
        assertEquals(2500L, c.offsetMs)
    }

    @Test fun sharedPositionRunsOnlyWhilePlaying() {
        val playing = group(state = "playing", positionMs = 10_000, anchorAt = 100_000, speed = 1.5)
        assertEquals(10_000L, sharedPosition(playing, 90_000)) // anchor in the future: not yet
        assertEquals(13_000L, sharedPosition(playing, 102_000))
        assertEquals(10_000L, sharedPosition(group(state = "paused", positionMs = 10_000, anchorAt = 100_000), 200_000))
    }

    @Test fun loadsTheGroupEntryAtTheSharedPositionOnce() {
        val en = engine(group(state = "playing", positionMs = 60_000, anchorAt = 1000))
        val s = en.step(snap(0, entry = null), localNow = 3000)
        assertEquals(listOf(PlayerAction.Load(e1, 62_000)), s.actions)
        // Still the old entry in the next tick (the load is under way): no second load.
        assertTrue(en.step(snap(0, entry = null), localNow = 3200).actions.isEmpty())
    }

    @Test fun switchesWhenTheGroupMovesToAnotherEntry() {
        val en = engine(group(state = "waiting", index = 1, version = 5))
        assertEquals(listOf(PlayerAction.Load(e2, 0)), en.step(snap(1_000_000, entry = "e1", playing = true), 1000).actions)
    }

    @Test fun waitingPlacesThePlayerThenReportsReadyOnce() {
        val en = engine(group(state = "waiting", positionMs = 30_000, version = 3))
        val first = en.step(snap(5_000, playing = true), 1000)
        assertEquals(listOf(PlayerAction.Pause, PlayerAction.Seek(30_000)), first.actions)
        assertTrue("no ready while still seeking", first.messages.isEmpty())
        val placed = en.step(snap(30_000), 1200)
        assertEquals(listOf("ready"), placed.messages.types())
        assertEquals("e1", placed.messages[0].entryId)
        assertEquals(30_000L, placed.messages[0].positionMs)
        assertTrue(en.step(snap(30_000), 1400).messages.isEmpty())
        // A new state that still has us not ready (e.g. after a reconnect) asks again.
        en.onState(group(state = "waiting", positionMs = 30_000, version = 4))
        assertEquals(listOf("ready"), en.step(snap(30_000), 1600).messages.types())
    }

    @Test fun noReadyWhileBufferingOrNotLoaded() {
        val en = engine(group(state = "waiting"))
        assertTrue(en.step(snap(0, buffering = true), 1000).messages.isEmpty())
        assertTrue(en.step(snap(0, prepared = false), 1100).messages.isEmpty())
        assertEquals(listOf("ready"), en.step(snap(0), 1200).messages.types())
    }

    @Test fun synchronisedStartWaitsForTheAnchor() {
        val en = engine(group(state = "playing", positionMs = 30_000, anchorAt = 10_600, ready = true, version = 7))
        val before = en.step(snap(30_000), 10_000)
        assertTrue(before.actions.isEmpty())
        assertEquals(200L, en.nextWakeMs(10_400))
        assertEquals(0L, en.nextWakeMs(10_700))
        assertTrue(en.step(snap(30_000), 10_500).actions.isEmpty())
        assertEquals(listOf(PlayerAction.Play), en.step(snap(30_000), 10_600).actions)
    }

    @Test fun lateStartSeeksToWhereTheOthersAre() {
        val en = engine(group(state = "playing", positionMs = 30_000, anchorAt = 10_600, ready = true))
        en.step(snap(30_000), 10_000)
        assertEquals(listOf(PlayerAction.Seek(33_000), PlayerAction.Play), en.step(snap(30_000), 13_600).actions)
    }

    @Test fun followsPlayingPausedAndSpeed() {
        val en = engine(group(state = "playing", positionMs = 0, anchorAt = 0, speed = 1.25, ready = true, version = 1))
        // 0.5 s off: left to the server's rate correction, but play and speed follow.
        assertEquals(listOf(PlayerAction.Speed(1.25), PlayerAction.Play), en.step(snap(12_000, playing = false), 10_000).actions)
        en.onState(group(state = "paused", positionMs = 20_000, version = 2, ready = true))
        assertEquals(listOf(PlayerAction.Speed(1.0), PlayerAction.Pause, PlayerAction.Seek(20_000)), en.step(snap(12_600, playing = true, speed = 1.25), 11_000).actions)
        // The same state again changes nothing.
        assertTrue(en.step(snap(12_600, playing = true), 11_200).actions.isEmpty())
    }

    @Test fun bigDifferenceOnANewStateSeeks() {
        val en = engine(group(state = "playing", positionMs = 100_000, anchorAt = 50_000, ready = true))
        assertEquals(listOf(PlayerAction.Seek(110_000)), en.step(snap(95_000, playing = true), 60_000).actions)
    }

    @Test fun reportsPositionAboutOnceASecondWhilePlaying() {
        val en = engine(group(state = "playing", positionMs = 0, anchorAt = 0, ready = true))
        val m = en.step(snap(5_000, playing = true), 5_000).messages
        assertEquals(listOf("position"), m.types())
        assertEquals(5_000L, m[0].at)
        assertEquals(true, m[0].playing)
        assertTrue(en.step(snap(5_500, playing = true), 5_500).messages.isEmpty())
        assertEquals(listOf("position"), en.step(snap(6_000, playing = true), 6_000).messages.types())
    }

    @Test fun reportedTimesAreOnTheServerClock() {
        val en = engine(group(state = "playing", positionMs = 0, anchorAt = 0, ready = true))
        en.clock.onPong(clientTime = 1000, serverTime = 101_000, receivedAt = 1000) // server is 100 s ahead
        val m = en.step(snap(104_000, playing = true), localNow = 4_000).messages
        assertEquals(104_000L, m.single().at)
    }

    @Test fun aStallLongerThanASecondIsReportedOnce() {
        val en = engine(group(state = "playing", positionMs = 0, anchorAt = 0, ready = true))
        en.step(snap(10_000, playing = true), 10_000)
        assertTrue(en.step(snap(10_300, playing = true, buffering = true), 10_300).messages.isEmpty())
        assertEquals(listOf("buffering"), en.step(snap(10_300, playing = true, buffering = true), 11_400).messages.types())
        assertTrue(en.step(snap(10_300, playing = true, buffering = true), 12_000).messages.isEmpty())
    }

    @Test fun aNotReadyPlayerDoesNotReportBuffering() {
        val en = engine(group(state = "waiting", ready = false))
        en.step(snap(0, buffering = true), 0)
        assertFalse(en.step(snap(0, buffering = true), 5_000).messages.types().contains("buffering"))
    }

    @Test fun rateCorrectionLastsItsDurationThenReturnsToGroupSpeed() {
        val en = engine(group(state = "playing", positionMs = 0, anchorAt = 0, ready = true))
        en.step(snap(10_000, playing = true), 10_000)
        en.onCorrection(SyncCorrection("rate", rate = 0.95, durationMs = 4_000, driftMs = 200), 10_100)
        assertEquals(listOf(PlayerAction.Speed(0.95)), en.step(snap(10_100, playing = true, speed = 1.0), 10_100).actions)
        assertTrue(en.step(snap(12_000, playing = true, speed = 0.95), 12_000).actions.isEmpty())
        assertEquals(listOf(PlayerAction.Speed(1.0)), en.step(snap(14_000, playing = true, speed = 0.95), 14_100).actions)
    }

    @Test fun seekCorrectionAddsTheTimeSinceItWasSent() {
        val en = engine(group(state = "playing", positionMs = 0, anchorAt = 0, speed = 2.0, ready = true))
        en.step(snap(20_000, playing = true), 10_000)
        en.onCorrection(SyncCorrection("seek", positionMs = 40_000, driftMs = -5_000), 10_000)
        assertEquals(listOf(PlayerAction.Seek(40_600)), en.step(snap(35_000, playing = true), 10_300).actions)
    }

    @Test fun correctionsAreIgnoredWhenTheGroupDoesNotPlay() {
        val en = engine(group(state = "paused", positionMs = 1000, ready = true))
        en.step(snap(1000), 0)
        en.onCorrection(SyncCorrection("seek", positionMs = 40_000, driftMs = -5_000), 0)
        assertTrue(en.step(snap(1000), 100).actions.isEmpty())
    }

    @Test fun theEndMovesTheGroupOnOnce() {
        val en = engine(group(state = "playing", positionMs = 0, anchorAt = 0, ready = true))
        en.step(snap(7_200_000, playing = true), 7_200_000)
        val m = en.step(snap(7_200_000, playing = true, ended = true), 7_200_100).messages
        assertTrue(m.any { it.type == "next" && it.entryId == "e1" })
        assertFalse(en.step(snap(7_200_000, playing = true, ended = true), 7_200_300).messages.any { it.type == "next" })
    }

    @Test fun nothingQueuedPauses() {
        val en = engine(group(state = "idle", index = -1))
        assertEquals(listOf(PlayerAction.Pause), en.step(snap(0, entry = null, playing = true), 0).actions)
    }

    @Test fun leavingStopsEverything() {
        val en = engine(group(state = "playing", ready = true))
        en.onState(null)
        val s = en.step(snap(0, playing = true), 0)
        assertTrue(s.actions.isEmpty() && s.messages.isEmpty())
    }

    @Test fun commandsCarryTheCurrentEntry() {
        val en = engine(group())
        assertEquals("e1", en.next().entryId)
        assertEquals(0L, en.seek(-5).positionMs)
        val wire = json.encodeToString(SyncClientMessage.serializer(), en.seek(0))
        assertEquals("""{"type":"seek","positionMs":0}""", wire)
        assertEquals("""{"type":"pause"}""", json.encodeToString(SyncClientMessage.serializer(), en.pause()))
    }

    @Test fun decodesServerMessages() {
        val m = json.decodeFromString(SyncServerMessage.serializer(), """
            {"type":"state","serverTime":5,"group":{"id":"g","name":"n","state":"playing","queue":[{"id":"e1","itemId":"i","title":"t"}],
             "index":0,"positionMs":10,"anchorAt":3,"speed":1,"version":2,"members":[{"id":"me","name":"A","device":"d","ready":true,
             "buffering":false,"connected":true,"lagging":false}],"serverTime":5,"createdAt":"x","selfId":"me","future":true}}""")
        assertEquals("i", m.group!!.current!!.itemId)
        assertTrue(m.group!!.self!!.ready)
        val left = json.decodeFromString(SyncServerMessage.serializer(), """{"type":"state","serverTime":5,"reason":"closed"}""")
        assertNull(left.group)
        assertEquals("closed", left.reason)
    }

    @Test fun parsesInviteLinks() {
        val id = "3f2b8c1e-4d5a-4b6c-9d7e-8f9a0b1c2d3e"
        assertEquals(id, parseGroupId("https://media.example.net/together/$id"))
        assertEquals(id, parseGroupId("  ${id.uppercase()} "))
        assertEquals(id, parseGroupId("Watch with me: https://x.net/together/$id?ref=1"))
        assertNull(parseGroupId("https://x.net/together/not-a-group"))
        assertNull(parseGroupId(""))
    }
}
