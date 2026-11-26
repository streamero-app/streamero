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
val ADULT_SORTS = listOf("date", "oldest", "added", "title", "duration", "played")
val ADULT_GALLERY_SORTS = listOf("date", "oldest", "added", "title", "images")
val ADULT_NAMED_SORTS = listOf("count", "name")

/** The places of the section's navigation, in order (D-058). */
enum class AdultPlace { Home, Scenes, Movies, Photos, Performers, Studios, Tags }

/** Filters of the galleries grid, as the query of GET /adult/galleries (defaults are left out). */
data class AdultGalleryFilter(
    val q: String = "", val sort: String = "date", val studioId: String? = null, val performerId: String? = null,
    val sceneId: String? = null, val tag: String? = null,
) {
    fun query(offset: Int, limit: Int): Map<String, Any?> = mapOf(
        "q" to q.trim().ifBlank { null },
        "sort" to sort.takeIf { it.isNotBlank() && it != "date" },
        "studioId" to studioId,
        "performerId" to performerId,
        "sceneId" to sceneId,
        "tag" to tag?.ifBlank { null },
        "offset" to offset,
        "limit" to limit,
    )
}

/** The hidden tags after hiding or showing [tag] again (case-insensitive, like the server). */
fun toggleHiddenTag(hidden: List<String>, tag: String): List<String> =
    if (hidden.any { it.equals(tag, ignoreCase = true) }) hidden.filterNot { it.equals(tag, ignoreCase = true) } else hidden + tag

/** The next picture of the viewer, wrapping around (arrows, D-pad and the slideshow). */
fun nextPicture(index: Int, delta: Int, count: Int): Int = if (count <= 0) 0 else ((index + delta) % count + count) % count

/** How long the slideshow shows each picture. */
const val SLIDESHOW_MS = 4000L

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
