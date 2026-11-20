package net.scyde.streamero.manage

import kotlinx.coroutines.CancellationException
import net.scyde.streamero.R

// Pure helpers of the Manage screens (no Android, no Compose), unit tested on the JVM.

/**
 * The wait before the next poll: the normal [baseMs] while calls succeed, doubled for each
 * failure in a row (a service that is down is not asked every few seconds), at most [maxMs].
 */
fun pollDelay(baseMs: Long, failures: Int, maxMs: Long = 5 * 60_000L): Long {
    if (baseMs <= 0) return 0
    if (failures <= 0) return baseMs
    val shift = failures.coerceAtMost(6)
    return (baseMs shl shift).coerceAtMost(maxOf(maxMs, baseMs))
}

/**
 * Keys for a lazy list from the items' ids. A lazy list crashes on a repeated key, and the
 * services do return records without an id (or the same id twice across pages), so a missing
 * or repeated id gets a key from its position instead.
 */
fun uniqueKeys(ids: List<Any?>): List<String> {
    val seen = HashSet<String>(ids.size * 2)
    return ids.mapIndexed { i, id ->
        val k = id?.toString()?.let { "k:$it" }
        if (k != null && seen.add(k)) k else "i:$i".also { seen.add(it) }
    }
}

/**
 * The message for an [ArrException] code: each code of the proxy (D-049, D-052) and each kind
 * of refusal by the service has its own, saying what to do. Null for `upstream` and
 * `validation`, which show the service's own words.
 */
fun arrErrorRes(code: String): Int? = when (code) {
    "unauthenticated" -> R.string.m_err_signed_out
    "forbidden" -> R.string.m_err_forbidden
    "read_only_key" -> R.string.m_err_read_only
    "invalid_path" -> R.string.m_err_invalid_path
    "method_not_allowed" -> R.string.m_err_method
    "arr_not_configured" -> R.string.m_err_not_configured
    "request_too_large" -> R.string.m_err_too_large
    "rate_limited" -> R.string.m_err_rate_limited
    "arr_unreachable" -> R.string.m_err_unreachable
    "arr_auth" -> R.string.m_err_arr_auth
    "not_found" -> R.string.m_err_not_found
    else -> null
}

/** Which Manage routes need which arguments, so a broken link shows a message instead of crashing. */
object ManageRoutes {
    private val INT_ID = setOf("item", "releases", "media", "suser", "sissue")
    private val NEEDS_ARG = setOf("providers", "provider", "config", "crud", "crudedit")
    private val NEEDS_ID = setOf("bsubs")

    fun valid(page: String, id: String?, arg: String?): Boolean = when (page) {
        in INT_ID -> id?.toIntOrNull() != null
        in NEEDS_ARG -> !arg.isNullOrBlank()
        in NEEDS_ID -> !id.isNullOrBlank() && id != "null"
        else -> true
    }
}

/** Some items of a bulk action failed: [failed] names each one with its error, of [total]. */
class PartialFailure(val failed: List<Pair<String, Throwable>>, val total: Int) :
    Exception("${failed.size} of $total failed: " + failed.joinToString("; ") { "${it.first}: ${it.second.message}" })

/**
 * Runs [block] for every item, one after the other, and carries on past failures. When any
 * failed, a [PartialFailure] names each of them (by [name]); a cancelled run stops at once.
 */
suspend fun <T> runEach(items: List<T>, name: (T) -> String, block: suspend (T) -> Unit) {
    val failed = mutableListOf<Pair<String, Throwable>>()
    for (item in items) {
        try {
            block(item)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            failed += name(item) to e
        }
    }
    if (failed.isNotEmpty()) throw PartialFailure(failed, items.size)
}

/** Text typed into number fields that is not a number: the save is refused with the [fields] named. */
class InvalidNumber(val fields: List<String>) : Exception("not a number: " + fields.joinToString(", "))

/**
 * A backup's file name as the device should save it: the last path segment, without
 * characters a file name cannot have; null when nothing usable is left.
 */
fun backupFileName(name: String?): String? {
    val last = name?.substringAfterLast('/')?.substringAfterLast('\\')?.trim().orEmpty()
    val clean = last.filter { it.isLetterOrDigit() || it in "._- " }.trim('.', ' ').take(120)
    return clean.ifEmpty { null }
}

/** Text a whole-number field accepts while typing: empty, "-", or an optional minus and digits. */
fun isIntInput(t: String): Boolean = t.isEmpty() || t == "-" || INT_INPUT.matches(t)

private val INT_INPUT = Regex("-?\\d{1,9}")
