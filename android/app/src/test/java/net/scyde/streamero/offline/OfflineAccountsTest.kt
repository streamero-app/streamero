package net.scyde.streamero.offline

import java.io.File
import java.nio.file.Files
import net.scyde.streamero.api.ItemSummary
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class OfflineAccountsTest {
    private val root: File = Files.createTempDirectory("offline").toFile()
    @After fun clean() { root.deleteRecursively() }

    private fun entry(id: String) = OfflineEntry(itemId = id, quality = Quality.ORIGINAL, file = OfflineStore.safeName(id) + "/video.mp4",
        state = EntryState.DONE, item = ItemSummary(id, "movie", "Film $id"))

    @Test fun accountsOnTheSameOrAnotherServerAreSeparate() {
        val a = OfflineAccounts.key("server-1", "user-1")
        assertNotEquals(a, OfflineAccounts.key("server-1", "user-2"))
        assertNotEquals(a, OfflineAccounts.key("server-2", "user-1"))
        assertNotEquals(OfflineAccounts.dir(root, a), OfflineAccounts.dir(root, OfflineAccounts.key("server-1", "user-2")))
        assertNotEquals(OfflineAccounts.dir(root, a), OfflineAccounts.dir(root, null))
        assertTrue(OfflineAccounts.dir(root, "../../etc").canonicalPath.startsWith(root.canonicalPath))
    }

    @Test fun rememberedAccount() {
        assertNull(OfflineAccounts.current(root))
        OfflineAccounts.setCurrent(root, "s_u")
        assertEquals("s_u", OfflineAccounts.current(root))
        OfflineAccounts.setCurrent(root, null)
        assertNull(OfflineAccounts.current(root))
    }

    @Test fun downloadsFromBeforeMoveToTheFirstAccountOnce() {
        val legacy = OfflineData(entries = listOf(entry("a-1"), entry("b-2")))
        File(root, "index.json").writeText(OfflineStore.encode(legacy))
        listOf("a-1", "b-2").forEach { File(root, "$it/video.mp4").apply { parentFile.mkdirs(); writeText("v$it") } }
        assertTrue(OfflineAccounts.hasLegacy(root))

        val first = OfflineAccounts.dir(root, OfflineAccounts.key("s", "u1"))
        assertTrue(OfflineAccounts.migrateLegacy(root, first))
        assertFalse(OfflineAccounts.hasLegacy(root))
        val store = OfflineStore(first)
        assertEquals(listOf("a-1", "b-2"), store.data.value.entries.map { it.itemId })
        assertEquals("va-1", store.file(store.entry("a-1")!!.file)!!.readText())
        assertFalse(File(root, "a-1").exists())

        // Another account signing in later gets nothing of it.
        val second = OfflineAccounts.dir(root, OfflineAccounts.key("s", "u2"))
        assertFalse(OfflineAccounts.migrateLegacy(root, second))
        assertTrue(OfflineStore(second).data.value.entries.isEmpty())
    }

    @Test fun neverOverwritesAnAccountsOwnLibrary() {
        val dir = OfflineAccounts.dir(root, "s_u")
        dir.mkdirs()
        File(dir, "index.json").writeText(OfflineStore.encode(OfflineData(entries = listOf(entry("mine")))))
        File(root, "index.json").writeText(OfflineStore.encode(OfflineData(entries = listOf(entry("old")))))
        assertFalse(OfflineAccounts.migrateLegacy(root, dir))
        assertEquals(listOf("mine"), OfflineStore(dir).data.value.entries.map { it.itemId })
    }
}
