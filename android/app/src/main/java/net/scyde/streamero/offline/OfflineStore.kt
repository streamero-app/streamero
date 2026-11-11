package net.scyde.streamero.offline

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import net.scyde.streamero.api.json
import java.io.File

/**
 * The offline library on disk: `index.json` plus one folder per item with the video, its
 * subtitle sidecars and artwork. File paths in the index are relative to [root].
 * Plain JVM code (no Android types), so it is unit-tested directly.
 */
class OfflineStore(val root: File) {
    private val index = File(root, "index.json")
    private val mutex = Mutex()
    private val state = MutableStateFlow(load())

    val data: StateFlow<OfflineData> = state.asStateFlow()

    fun entry(itemId: String): OfflineEntry? = state.value.entry(itemId)

    /** Changes the data atomically; `persist = false` keeps a change in memory only (progress ticks). */
    suspend fun update(persist: Boolean = true, change: (OfflineData) -> OfflineData): OfflineData = mutex.withLock {
        val next = change(state.value)
        state.value = next
        if (persist) withContext(Dispatchers.IO) { save(next) }
        next
    }

    suspend fun updateEntry(itemId: String, persist: Boolean = true, change: (OfflineEntry) -> OfflineEntry): OfflineEntry? =
        update(persist) { d -> d.copy(entries = d.entries.map { if (it.itemId == itemId) change(it) else it }) }.entry(itemId)

    /** Removes entries and their folders. */
    suspend fun remove(itemIds: Collection<String>) {
        if (itemIds.isEmpty()) return
        val ids = itemIds.toSet()
        update { d -> d.copy(entries = d.entries.filter { it.itemId !in ids }) }
        withContext(Dispatchers.IO) { ids.forEach { dirFor(it).deleteRecursively() } }
    }

    /** The folder of one item (the id is reduced to safe characters). */
    fun dirFor(itemId: String): File = File(root, safeName(itemId))

    /** Path relative to [root] for a file of one item. */
    fun relative(itemId: String, name: String): String = safeName(itemId) + "/" + name

    /** A stored relative path as a file, never outside [root]. */
    fun file(relative: String?): File? {
        if (relative.isNullOrBlank()) return null
        val f = File(root, relative).canonicalFile
        return f.takeIf { it.path.startsWith(root.canonicalPath + File.separator) }
    }

    /** Bytes in an item's folder (video, sidecars, artwork). */
    fun bytesOnDisk(itemId: String): Long = dirFor(itemId).walkTopDown().filter { it.isFile }.sumOf { it.length() }

    private fun load(): OfflineData {
        val text = runCatching { index.readText() }.getOrNull() ?: return OfflineData()
        return runCatching { decode(text) }.getOrElse {
            // Keep an unreadable index for inspection instead of silently losing it.
            runCatching { index.copyTo(File(root, "index.broken.json"), overwrite = true) }
            OfflineData()
        }
    }

    private fun save(d: OfflineData) {
        root.mkdirs()
        val tmp = File(root, "index.json.tmp")
        tmp.writeText(encode(d))
        if (!tmp.renameTo(index)) { index.delete(); tmp.renameTo(index) }
    }

    companion object {
        fun encode(d: OfflineData): String = json.encodeToString(OfflineData.serializer(), d)
        fun decode(s: String): OfflineData = json.decodeFromString(OfflineData.serializer(), s)
        fun safeName(id: String): String = id.filter { it.isLetterOrDigit() || it == '-' }.ifEmpty { "_" }
    }
}
