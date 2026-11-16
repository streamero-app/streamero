package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonObject

/** How the tags of a bulk edit are applied (`applyTags` of the editor endpoints). */
enum class TagMode(val api: String) { Add("add"), Remove("remove"), Replace("replace") }

/**
 * One bulk edit of library items through the documented editor endpoints
 * (`movie/editor`, `series/editor`, `artist/editor`): only the fields that are set are sent,
 * so everything else stays as it is on each item.
 */
data class BulkEdit(
    val monitored: Boolean? = null,
    val qualityProfileId: Int? = null,
    val rootFolderPath: String? = null,
    val moveFiles: Boolean = false,
    val tags: List<Int>? = null,
    val tagMode: TagMode = TagMode.Add,
) {
    val isEmpty: Boolean get() = monitored == null && qualityProfileId == null && rootFolderPath == null && tags == null
}

object BulkEditor {
    fun resource(s: ArrService) = Lib.resource(s) + "/editor"
    fun idsKey(s: ArrService) = when (s) { ArrService.Sonarr -> "seriesIds"; ArrService.Lidarr -> "artistIds"; else -> "movieIds" }

    /** The PUT body of a bulk edit. */
    fun editBody(s: ArrService, ids: Collection<Int>, e: BulkEdit): JsonObject {
        val m = linkedMapOf<String, Any?>(idsKey(s) to ids.sorted())
        e.monitored?.let { m["monitored"] = it }
        e.qualityProfileId?.let { m["qualityProfileId"] = it }
        e.rootFolderPath?.let { m["rootFolderPath"] = it; m["moveFiles"] = e.moveFiles }
        e.tags?.let { m["tags"] = it; m["applyTags"] = e.tagMode.api }
        return jsonOf(*m.toList().toTypedArray())
    }

    /** The DELETE body of a bulk delete (Radarr names its exclusion flag differently). */
    fun deleteBody(s: ArrService, ids: Collection<Int>, deleteFiles: Boolean, exclude: Boolean): JsonObject = jsonOf(
        idsKey(s) to ids.sorted(),
        "deleteFiles" to deleteFiles,
        (if (s == ArrService.Radarr) "addImportExclusion" else "addImportListExclusion") to exclude,
    )
}
