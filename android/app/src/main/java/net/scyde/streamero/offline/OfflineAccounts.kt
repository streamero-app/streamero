package net.scyde.streamero.offline

import java.io.File

/**
 * Downloads belong to one account on one server: each keeps its own library under
 * `offline/accounts/<server>_<user>/` (index and files), so another user on the same device
 * never sees them and their progress is never synced under the wrong account.
 * Plain JVM code (no Android types), so it is unit-tested directly.
 */
object OfflineAccounts {
    private const val ACCOUNTS = "accounts"
    private const val CURRENT = "account"
    private const val NONE = "_signed_out"

    fun key(serverId: String, userId: String): String = OfflineStore.safeName(serverId) + "_" + OfflineStore.safeName(userId)

    /** The library folder of one account; signed out gets an empty one of its own. */
    fun dir(root: File, key: String?): File = File(File(root, ACCOUNTS), key?.filter { it.isLetterOrDigit() || it == '-' || it == '_' }?.ifEmpty { null } ?: NONE)

    /** The account signed in last, so downloads play (and transfers continue) before the server answers. */
    fun current(root: File): String? = runCatching { File(root, CURRENT).readText().trim() }.getOrNull()?.ifEmpty { null }

    fun setCurrent(root: File, key: String?) {
        val f = File(root, CURRENT)
        if (key == null) { f.delete(); return }
        root.mkdirs()
        f.writeText(key)
    }

    /** Whether a library from before downloads were per account is still at the top of [root]. */
    fun hasLegacy(root: File): Boolean = File(root, "index.json").isFile

    /**
     * Moves a library from before downloads were per account (`offline/index.json` and its item
     * folders) into [target], once: the first account that signs in after the update keeps them.
     * Does nothing when [target] already has a library. Returns whether anything moved.
     */
    fun migrateLegacy(root: File, target: File): Boolean {
        val legacy = File(root, "index.json")
        if (!legacy.isFile || File(target, "index.json").exists()) return false
        val data = runCatching { OfflineStore.decode(legacy.readText()) }.getOrNull() ?: return false
        target.mkdirs()
        for (e in data.entries) {
            val name = OfflineStore.safeName(e.itemId)
            val from = File(root, name)
            if (!from.exists()) continue
            val to = File(target, name)
            if (!from.renameTo(to)) {
                from.copyRecursively(to, overwrite = true)
                from.deleteRecursively()
            }
        }
        val index = File(target, "index.json")
        if (!legacy.renameTo(index)) { legacy.copyTo(index, overwrite = true); legacy.delete() }
        return true
    }
}
