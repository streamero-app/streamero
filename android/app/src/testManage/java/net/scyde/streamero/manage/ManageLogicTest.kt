package net.scyde.streamero.manage

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import net.scyde.streamero.api.json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class ManageLogicTest {
    @Test fun pollingBacksOffOnErrorsAndRecovers() {
        assertEquals(5_000, pollDelay(5_000, 0))
        assertEquals(10_000, pollDelay(5_000, 1))
        assertEquals(40_000, pollDelay(5_000, 3))
        assertEquals("capped", 5 * 60_000L, pollDelay(5_000, 50))
        assertEquals("no overflow on long outages", 5 * 60_000L, pollDelay(15_000, Int.MAX_VALUE))
        assertEquals(0, pollDelay(0, 3))
    }

    @Test fun lazyKeysNeverRepeat() {
        val keys = uniqueKeys(listOf(1, 2, null, 2, "1", null))
        assertEquals(6, keys.toSet().size)
        assertEquals("k:1", keys[0])
        assertEquals("k:2", keys[1])
        // Missing and repeated ids fall back to their position.
        assertEquals("i:2", keys[2])
        assertEquals("i:3", keys[3])
        assertTrue(uniqueKeys(emptyList()).isEmpty())
    }

    @Test fun brokenLinksAreRefusedNotCrashed() {
        assertFalse(ManageRoutes.valid("item", null, null))
        assertFalse(ManageRoutes.valid("item", "null", null))
        assertFalse(ManageRoutes.valid("suser", "12x", null))
        assertTrue(ManageRoutes.valid("item", "12", null))
        assertFalse(ManageRoutes.valid("providers", null, null))
        assertTrue(ManageRoutes.valid("provider", "new", "indexer"))
        assertFalse(ManageRoutes.valid("bsubs", "null", "movies"))
        assertTrue(ManageRoutes.valid("bsubs", "7", "episodes:3"))
        assertTrue(ManageRoutes.valid("queue", null, null))
    }

    @Test fun bulkRunsReportEachFailure() = runTest {
        val done = mutableListOf<Int>()
        try {
            runEach(listOf(1, 2, 3, 4), { "#$it" }) { if (it % 2 == 0) error("refused $it") else done += it }
            fail("expected a partial failure")
        } catch (e: PartialFailure) {
            assertEquals(listOf(1, 3), done)
            assertEquals(4, e.total)
            assertEquals(listOf("#2", "#4"), e.failed.map { it.first })
            assertEquals("refused 2", e.failed[0].second.message)
        }
        runEach(listOf(1), { "x" }) { }
    }

    @Test fun cancellingABulkRunIsNotAFailure() = runTest {
        var after = false
        try {
            runEach(listOf(1, 2), { "x" }) { if (it == 1) throw CancellationException("left") else after = true }
            fail("cancellation must pass")
        } catch (e: CancellationException) {
            assertFalse("stops at once", after)
        }
    }

    @Test fun backupNamesStayInsideDownloads() {
        assertEquals("radarr_backup_v5.2_2026.zip", backupFileName("radarr_backup_v5.2_2026.zip"))
        assertEquals("evil.zip", backupFileName("../../evil.zip"))
        assertEquals("x.zip", backupFileName("C:\\a\\x.zip"))
        assertNull(backupFileName(".."))
        assertNull(backupFileName(null))
    }

    @Test fun intFieldsKeepNegativeNumbersWhileTyping() {
        assertTrue(isIntInput(""))
        assertTrue(isIntInput("-"))
        assertTrue(isIntInput("-250"))
        assertTrue(isIntInput("42"))
        assertFalse(isIntInput("4.2"))
        assertFalse(isIntInput("abc"))
        assertFalse("would overflow an Int", isIntInput("99999999999"))
    }

    @Test fun seasonSwitchChangesOnlyThatSeason() {
        val series = json.parseToJsonElement("""{"id":1,"path":"/tv/a","seasons":[{"seasonNumber":0,"monitored":false},{"seasonNumber":1,"monitored":true,"statistics":{"x":1}},"odd"],"unknown":{"k":[1,2]}}""") as JsonObject
        val out = Lib.withSeasonMonitored(series, 1, false)
        assertEquals(listOf(false, false), out.objs("seasons").map { it.b("monitored") })
        assertEquals(series.o("unknown"), out.o("unknown"))
        assertEquals("non-object entries kept", 3, out.a("seasons")!!.size)
        assertEquals(series.objs("seasons")[1].o("statistics"), out.objs("seasons")[1].o("statistics"))
        // A series without seasons does not crash.
        assertEquals(0, Lib.withSeasonMonitored(JsonObject(emptyMap()), 1, true).a("seasons")!!.size)
    }
}
