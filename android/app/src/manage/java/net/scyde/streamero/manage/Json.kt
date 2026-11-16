package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.longOrNull

// The services' resources are handled as JSON trees rather than typed models: their shapes
// differ between services and versions, and an edit must PUT back the whole resource with
// every field the app does not know about left untouched.

fun JsonObject.s(key: String): String? = (this[key] as? JsonPrimitive)?.takeIf { it !is JsonNull }?.contentOrNull
fun JsonObject.l(key: String): Long? = (this[key] as? JsonPrimitive)?.let { it.longOrNull ?: it.doubleOrNull?.toLong() }
fun JsonObject.i(key: String): Int? = l(key)?.toInt()
fun JsonObject.d(key: String): Double? = (this[key] as? JsonPrimitive)?.doubleOrNull
fun JsonObject.b(key: String): Boolean? = (this[key] as? JsonPrimitive)?.booleanOrNull
fun JsonObject.o(key: String): JsonObject? = this[key] as? JsonObject
fun JsonObject.a(key: String): JsonArray? = this[key] as? JsonArray
fun JsonObject.objs(key: String): List<JsonObject> = a(key)?.filterIsInstance<JsonObject>() ?: emptyList()
fun JsonObject.ints(key: String): List<Int> = a(key)?.mapNotNull { (it as? JsonPrimitive)?.longOrNull?.toInt() } ?: emptyList()
fun JsonObject.strs(key: String): List<String> = a(key)?.mapNotNull { (it as? JsonPrimitive)?.contentOrNull } ?: emptyList()

/** A copy with [key] set ([value] converted with [toJson]). */
fun JsonObject.with(key: String, value: Any?): JsonObject = JsonObject(this + (key to toJson(value)))
fun JsonObject.without(key: String): JsonObject = JsonObject(this - key)

/** A copy with the value at [path] (object keys and array indexes) replaced. */
fun JsonElement.setAt(path: List<Any>, value: JsonElement): JsonElement {
    if (path.isEmpty()) return value
    val head = path.first()
    val rest = path.drop(1)
    return when {
        this is JsonObject && head is String -> JsonObject(this + (head to (this[head] ?: JsonNull).setAt(rest, value)))
        this is JsonArray && head is Int && head in indices -> JsonArray(mapIndexed { i, e -> if (i == head) e.setAt(rest, value) else e })
        else -> this
    }
}

fun JsonElement.at(path: List<Any>): JsonElement? {
    var e: JsonElement? = this
    for (p in path) e = when {
        e is JsonObject && p is String -> e[p]
        e is JsonArray && p is Int -> e.getOrNull(p)
        else -> null
    }
    return e
}

/** Kotlin values to JSON: null, strings, numbers, booleans, lists, maps and JSON elements. */
fun toJson(v: Any?): JsonElement = when (v) {
    null -> JsonNull
    is JsonElement -> v
    is String -> JsonPrimitive(v)
    is Number -> JsonPrimitive(v)
    is Boolean -> JsonPrimitive(v)
    is Collection<*> -> JsonArray(v.map { toJson(it) })
    is Array<*> -> JsonArray(v.map { toJson(it) })
    is Map<*, *> -> JsonObject(v.entries.associate { it.key.toString() to toJson(it.value) })
    else -> JsonPrimitive(v.toString())
}

fun jsonOf(vararg pairs: Pair<String, Any?>): JsonObject = JsonObject(pairs.associate { it.first to toJson(it.second) })

/** "enableColorImpairedMode" → "Enable color impaired mode"; snake_case and kebab-case too. */
fun humanize(key: String): String {
    val spaced = key.replace(Regex("([a-z0-9])([A-Z])"), "$1 $2").replace(Regex("([A-Z]+)([A-Z][a-z])"), "$1 $2").replace('_', ' ').replace('-', ' ').trim()
    if (spaced.isEmpty()) return key
    val lower = spaced.split(' ').joinToString(" ") { w -> if (w.length > 1 && w.all { it.isUpperCase() || it.isDigit() }) w else w.lowercase() }
    return lower.replaceFirstChar { it.uppercase() }
}

/** Bytes as "1.4 GB". */
fun bytes(n: Long?): String {
    if (n == null || n < 0) return "—"
    val units = listOf("B", "KB", "MB", "GB", "TB", "PB")
    var v = n.toDouble()
    var u = 0
    while (v >= 1024 && u < units.lastIndex) { v /= 1024; u++ }
    return if (u == 0) "$n B" else String.format(java.util.Locale.ROOT, "%.1f %s", v, units[u])
}

/** The poster of an *arr resource (`images[]` with `coverType`). */
fun JsonObject.cover(type: String = "poster"): JsonObject? = objs("images").firstOrNull { it.s("coverType") == type }
