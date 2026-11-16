package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject

/**
 * Editing of an *arr quality profile (`qualityprofile`): `items[]` lists qualities and
 * groups from lowest to highest (a group has its own `id`/`name` and nested `items`), each
 * `allowed` or not; `cutoff` is the id of an allowed quality or group. The editor shows the
 * list highest first, so the "display" helpers here work on the reversed order.
 */
object QualityProfiles {
    fun itemId(item: JsonObject): Int? = if (item.objs("items").isNotEmpty() || item.o("quality") == null) item.i("id") else item.o("quality")?.i("id")
    fun itemName(item: JsonObject): String = item.s("name")?.ifBlank { null } ?: item.o("quality")?.s("name") ?: "?"
    fun isGroup(item: JsonObject): Boolean = item.objs("items").isNotEmpty()

    fun items(profile: JsonObject): List<JsonObject> = profile.objs("items")

    /** Highest quality first, as shown. */
    fun display(profile: JsonObject): List<JsonObject> = items(profile).reversed()

    /** Moves the shown row [from] to shown position [to] (both highest-first). */
    fun move(profile: JsonObject, from: Int, to: Int): JsonObject {
        val shown = display(profile).toMutableList()
        if (from !in shown.indices || to !in shown.indices || from == to) return profile
        val x = shown.removeAt(from)
        shown.add(to, x)
        return withItems(profile, shown.reversed())
    }

    fun setAllowed(profile: JsonObject, shownIndex: Int, allowed: Boolean): JsonObject {
        val shown = display(profile).toMutableList()
        if (shownIndex !in shown.indices) return profile
        shown[shownIndex] = shown[shownIndex].with("allowed", allowed)
        return fixCutoff(withItems(profile, shown.reversed()))
    }

    /** The qualities and groups the cutoff can be set to (allowed ones), highest first. */
    fun cutoffChoices(profile: JsonObject): List<Pair<Int, String>> =
        display(profile).filter { it.b("allowed") == true }.mapNotNull { item -> itemId(item)?.let { it to itemName(item) } }

    /** Keeps the cutoff on an allowed item: falls back to the highest allowed one. */
    fun fixCutoff(profile: JsonObject): JsonObject {
        val choices = cutoffChoices(profile)
        val cutoff = profile.i("cutoff")
        if (choices.isEmpty() || choices.any { it.first == cutoff }) return profile
        return profile.with("cutoff", choices.first().first)
    }

    fun setScore(profile: JsonObject, formatId: Int, score: Int): JsonObject =
        profile.with("formatItems", JsonArray(profile.objs("formatItems").map { if (it.i("format") == formatId) it.with("score", score) else it }))

    private fun withItems(profile: JsonObject, items: List<JsonObject>) = profile.with("items", JsonArray(items))
}
