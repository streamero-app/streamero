package net.scyde.streamero.manage

import net.scyde.streamero.R

/**
 * Seerr's user permission bitmask. [KNOWN] are the bits Seerr's public documentation names;
 * any other set bit is kept as it is and shown as "Other (bit N)" so a save never drops it.
 */
object SeerrPermissions {
    data class Bit(val value: Long, val key: String, val label: Int)

    const val ADMIN = 2L

    val KNOWN: List<Bit> = listOf(
        Bit(2L, "ADMIN", R.string.m_perm_admin),
        Bit(4L, "MANAGE_SETTINGS", R.string.m_perm_manage_settings),
        Bit(8L, "MANAGE_USERS", R.string.m_perm_manage_users),
        Bit(16L, "MANAGE_REQUESTS", R.string.m_perm_manage_requests),
        Bit(32L, "REQUEST", R.string.m_perm_request),
        Bit(64L, "VOTE", R.string.m_perm_vote),
        Bit(128L, "AUTO_APPROVE", R.string.m_perm_auto_approve),
        Bit(256L, "AUTO_APPROVE_MOVIE", R.string.m_perm_auto_approve_movie),
        Bit(512L, "AUTO_APPROVE_TV", R.string.m_perm_auto_approve_tv),
        Bit(1024L, "REQUEST_4K", R.string.m_perm_request_4k),
        Bit(2048L, "REQUEST_4K_MOVIE", R.string.m_perm_request_4k_movie),
        Bit(4096L, "REQUEST_4K_TV", R.string.m_perm_request_4k_tv),
        Bit(8192L, "REQUEST_ADVANCED", R.string.m_perm_request_advanced),
        Bit(16384L, "REQUEST_VIEW", R.string.m_perm_request_view),
        Bit(32768L, "AUTO_APPROVE_4K", R.string.m_perm_auto_approve_4k),
        Bit(65536L, "AUTO_APPROVE_4K_MOVIE", R.string.m_perm_auto_approve_4k_movie),
        Bit(131072L, "AUTO_APPROVE_4K_TV", R.string.m_perm_auto_approve_4k_tv),
        Bit(262144L, "REQUEST_MOVIE", R.string.m_perm_request_movie),
        Bit(524288L, "REQUEST_TV", R.string.m_perm_request_tv),
        Bit(1048576L, "MANAGE_ISSUES", R.string.m_perm_manage_issues),
        Bit(2097152L, "VIEW_ISSUES", R.string.m_perm_view_issues),
        Bit(4194304L, "CREATE_ISSUES", R.string.m_perm_create_issues),
        Bit(8388608L, "AUTO_REQUEST", R.string.m_perm_auto_request),
        Bit(16777216L, "AUTO_REQUEST_MOVIE", R.string.m_perm_auto_request_movie),
        Bit(33554432L, "AUTO_REQUEST_TV", R.string.m_perm_auto_request_tv),
        Bit(67108864L, "RECENT_VIEW", R.string.m_perm_recent_view),
        Bit(134217728L, "WATCHLIST_VIEW", R.string.m_perm_watchlist_view),
    )

    private val knownMask: Long = KNOWN.fold(0L) { m, b -> m or b.value }

    fun has(mask: Long, bit: Long): Boolean = mask and bit != 0L
    fun set(mask: Long, bit: Long, on: Boolean): Long = if (on) mask or bit else mask and bit.inv()

    /** Bit positions (0-based) that are set but not documented; kept on save. */
    fun otherBits(mask: Long): List<Int> = (0 until 63).filter { i -> val b = 1L shl i; mask and b != 0L && knownMask and b == 0L }

    fun names(mask: Long): List<String> = KNOWN.filter { has(mask, it.value) }.map { it.key } + otherBits(mask).map { "BIT_$it" }

    /** True when an edit takes the admin bit away from a user that had it. */
    fun losesAdmin(before: Long, after: Long): Boolean = has(before, ADMIN) && !has(after, ADMIN)
}
