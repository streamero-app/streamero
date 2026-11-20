package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull

/**
 * Structural edits of the generic settings editor ([JsonForm]): adding, removing, duplicating
 * and reordering items of nested arrays. Every operation returns a new tree and leaves all
 * other keys (including ones the app does not know about) exactly as they were.
 */
object JsonTree {
    /** Keys a new or duplicated item must not carry: the service assigns them. */
    private val IDENTITY = setOf("id")

    /** A value of the same type as [like] but empty: "" / 0 / false / [] / an object with the same keys emptied. */
    fun blankLike(like: JsonElement): JsonElement = when (like) {
        is JsonObject -> JsonObject(like.filterKeys { it !in IDENTITY }.mapValues { blankLike(it.value) })
        is JsonArray -> JsonArray(emptyList())
        JsonNull -> JsonNull
        is JsonPrimitive -> when {
            like.isString -> JsonPrimitive("")
            like.booleanOrNull != null -> JsonPrimitive(false)
            like.content.contains('.') || like.content.contains('e', true) -> JsonPrimitive(0.0)
            else -> JsonPrimitive(0)
        }
    }

    /**
     * A new item for [array]: shaped like its first item (an object with the same keys and
     * type-appropriate empty values, or an empty value of the same primitive type); an empty
     * string when the array is empty and its type cannot be known.
     */
    fun newItem(array: JsonArray): JsonElement = array.firstOrNull()?.let(::blankLike) ?: JsonPrimitive("")

    /**
     * A copy of an item for "duplicate": everything kept except its identity and its stored
     * secrets (passwords, keys; often only the service's mask), which the copy must get anew.
     */
    fun copyOf(item: JsonElement): JsonElement = if (item is JsonObject) withoutSecrets(JsonObject(item.filterKeys { it !in IDENTITY })) else item

    private fun withoutSecrets(e: JsonElement): JsonElement = when (e) {
        is JsonObject -> JsonObject(e.mapValues { (k, v) -> if (v is JsonPrimitive && v.isString && isSecretKey(k)) JsonPrimitive("") else withoutSecrets(v) })
        is JsonArray -> JsonArray(e.map { withoutSecrets(it) })
        else -> e
    }

    /** True when [array] holds objects (edited as sub-sections) rather than plain values. */
    fun isObjectArray(array: JsonArray): Boolean = array.isNotEmpty() && array.all { it is JsonObject }

    /** The array at [path] with [item] inserted at [index] (appended when null or out of range). */
    fun insert(root: JsonElement, path: List<Any>, item: JsonElement, index: Int? = null): JsonElement =
        update(root, path) { arr ->
            val i = index?.takeIf { it in 0..arr.size } ?: arr.size
            JsonArray(arr.toMutableList().apply { add(i, item) })
        }

    /** The array at [path] with an item shaped like its siblings appended. */
    fun add(root: JsonElement, path: List<Any>): JsonElement = update(root, path) { JsonArray(it + newItem(it)) }

    /** The array at [path] with a copy of item [index] inserted right after it. */
    fun duplicate(root: JsonElement, path: List<Any>, index: Int): JsonElement = update(root, path) { arr ->
        if (index !in arr.indices) arr else JsonArray(arr.toMutableList().apply { add(index + 1, copyOf(arr[index])) })
    }

    /** The array at [path] without item [index]. */
    fun remove(root: JsonElement, path: List<Any>, index: Int): JsonElement = update(root, path) { arr ->
        if (index !in arr.indices) arr else JsonArray(arr.filterIndexed { i, _ -> i != index })
    }

    /** The array at [path] with item [from] moved to position [to] (no-op when either is out of range). */
    fun move(root: JsonElement, path: List<Any>, from: Int, to: Int): JsonElement = update(root, path) { arr ->
        if (from !in arr.indices || to !in arr.indices || from == to) arr
        else JsonArray(arr.toMutableList().apply { add(to, removeAt(from)) })
    }

    /**
     * A plain value typed by the user for an array of primitives: a number when the array's
     * values are numbers (and the text is one), a boolean for boolean arrays, otherwise text.
     */
    fun primitiveFor(array: JsonArray, text: String): JsonElement {
        val t = text.trim()
        val sample = array.firstOrNull { it is JsonPrimitive && it !is JsonNull } as? JsonPrimitive
        if (sample != null && !sample.isString) {
            sample.booleanOrNull?.let { return t.toBooleanStrictOrNull()?.let(::JsonPrimitive) ?: JsonPrimitive(t) }
            t.toLongOrNull()?.let { return JsonPrimitive(it) }
            t.toDoubleOrNull()?.let { return JsonPrimitive(it) }
        }
        return JsonPrimitive(t)
    }

    /** How an item of an array of primitives reads in a chip. */
    fun label(e: JsonElement): String = (e as? JsonPrimitive)?.contentOrNull ?: e.toString()

    private fun update(root: JsonElement, path: List<Any>, f: (JsonArray) -> JsonArray): JsonElement {
        val arr = root.at(path) as? JsonArray ?: if (root.at(path) == null || root.at(path) == JsonNull) JsonArray(emptyList()) else return root
        return root.setAt(path, f(arr))
    }
}
