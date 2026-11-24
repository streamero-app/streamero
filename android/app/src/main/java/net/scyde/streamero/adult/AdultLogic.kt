package net.scyde.streamero.adult

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import net.scyde.streamero.api.AdultStatus
import net.scyde.streamero.api.ApiException
import net.scyde.streamero.api.User

// The adult section (D-053): pure rules shared by its screens, kept here so they can be unit tested.

/** The entry point is shown only when the server says the section is open to this session. */
fun showsAdultSection(me: User?): Boolean = me?.adultAccess == true && me.currentProfile?.isKid != true

/** A PIN the server accepts: 4 to 12 digits. */
fun validAdultPin(pin: String): Boolean = pin.length in 4..12 && pin.all { it in '0'..'9' }

/** What an adult screen shows before its content. */
enum class AdultGate { Loading, Unavailable, Locked, Open }

/** The section's state from /adult/status (or the error asking for it). */
fun adultGate(status: AdultStatus?, error: Throwable?): AdultGate = when {
    status != null -> if (status.pinSet && !status.unlocked) AdultGate.Locked else AdultGate.Open
    error is ApiException && error.status == 403 && error.code == "adult_locked" -> AdultGate.Locked
    error is ApiException && error.status == 404 -> AdultGate.Unavailable
    error != null -> AdultGate.Unavailable
    else -> AdultGate.Loading
}

/** Filters of the scenes grid, as the query of GET /adult/scenes (defaults are left out). */
data class AdultSceneFilter(
    val q: String = "", val kind: String = "", val status: String = "all", val sort: String = "date",
    val libraryId: String? = null, val studioId: String? = null, val performerId: String? = null, val tag: String? = null,
) {
    fun query(offset: Int, limit: Int): Map<String, Any?> = mapOf(
        "q" to q.trim().ifBlank { null },
        "kind" to kind.ifBlank { null },
        "status" to status.takeIf { it != "all" && it.isNotBlank() },
        "sort" to sort.ifBlank { null },
        "libraryId" to libraryId,
        "studioId" to studioId,
        "performerId" to performerId,
        "tag" to tag?.ifBlank { null },
        "offset" to offset,
        "limit" to limit,
    )
}

val ADULT_KINDS = listOf("", "scene", "movie")
val ADULT_STATUSES = listOf("all", "unwatched", "inProgress", "watched")
val ADULT_SORTS = listOf("date", "added", "title", "played")

/**
 * Blurred artwork: with blurring on, every image stays blurred until it is tapped. What was
 * revealed is remembered for one visit of a screen only (a new instance per screen).
 */
class AdultBlur(val enabled: Boolean) {
    private var revealed by mutableStateOf(emptySet<String>())

    fun blurred(key: String): Boolean = enabled && key !in revealed

    /** Reveals [key]; returns true when the tap was used for that (the image was blurred). */
    fun reveal(key: String): Boolean {
        if (!blurred(key)) return false
        revealed = revealed + key
        return true
    }
}
