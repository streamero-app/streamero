package net.scyde.streamero.manage

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ManageSearch
import androidx.compose.material.icons.automirrored.outlined.Label
import androidx.compose.material.icons.automirrored.outlined.DriveFileMove
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import net.scyde.streamero.R
import net.scyde.streamero.ui.*

private enum class LibFilter { All, Monitored, Unmonitored, Missing }
private enum class LibSort { Title, Added, Year, Size }

/**
 * The library of Radarr, Sonarr or Lidarr: search, sort, filter, posters. A long press (or the
 * select button) starts multi-select; the selection is then edited in bulk through the
 * service's editor endpoint.
 */
@Composable
fun LibraryPage(nav: NavHostController, c: ArrClient) {
    val s = c.service
    val all = rememberLoaded(s) { c.list(Lib.resource(s)) }
    var q by rememberSaveable { mutableStateOf("") }
    var filter by rememberSaveable { mutableStateOf(LibFilter.All) }
    var sort by rememberSaveable { mutableStateOf(LibSort.Title) }
    var selecting by remember { mutableStateOf(false) }
    var selected by remember { mutableStateOf(setOf<Int>()) }
    val shown = remember(all.value, q, filter, sort) {
        all.value.orEmpty().asSequence()
            .filter { q.isBlank() || Lib.title(it).contains(q.trim(), ignoreCase = true) }
            .filter {
                when (filter) {
                    LibFilter.All -> true
                    LibFilter.Monitored -> it.b("monitored") == true
                    LibFilter.Unmonitored -> it.b("monitored") != true
                    LibFilter.Missing -> it.b("monitored") == true && !Lib.complete(s, it)
                }
            }
            .let { seq ->
                when (sort) {
                    LibSort.Title -> seq.sortedBy { (it.s("sortTitle") ?: it.s("sortName") ?: Lib.title(it)).lowercase() }
                    LibSort.Added -> seq.sortedByDescending { it.s("added").orEmpty() }
                    LibSort.Year -> seq.sortedByDescending { it.i("year") ?: 0 }
                    LibSort.Size -> seq.sortedByDescending { Lib.sizeOnDisk(it) ?: 0 }
                }
            }.toList()
    }
    fun exitSelection() { selecting = false; selected = emptySet() }
    androidx.activity.compose.BackHandler(enabled = selecting) { exitSelection() }
    Box(Modifier.fillMaxSize()) {
        ManagePage(nav, if (selecting) plural(R.plurals.m_selected_n, selected.size) else stringResource(R.string.m_library), all.value?.let { itemsCount(it.size) }, actions = {
            if (selecting) {
                val ids = shown.mapNotNull { it.i("id") }.toSet()
                val allOn = ids.isNotEmpty() && selected.containsAll(ids)
                TextButton(onClick = { selected = if (allOn) selected - ids else selected + ids }) {
                    Text(stringResource(if (allOn) R.string.m_select_none else R.string.m_select_all), color = Kino.text, fontSize = 13.sp)
                }
                IconButton(onClick = { exitSelection() }) { Icon(Icons.Outlined.Close, stringResource(R.string.cancel), tint = Kino.text) }
            } else {
                IconButton(onClick = { selecting = true }) { Icon(Icons.Outlined.Checklist, stringResource(R.string.m_select), tint = Kino.text) }
                IconButton(onClick = { nav.manage(s, "add") }) { Icon(Icons.Outlined.Add, stringResource(R.string.m_add_new), tint = Kino.text) }
            }
        }) {
            item { SearchField(q, { q = it }, stringResource(R.string.m_filter_library), Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) }
            item {
                ChipRow(listOf(LibFilter.All to stringResource(R.string.m_all), LibFilter.Monitored to stringResource(R.string.m_monitored),
                    LibFilter.Unmonitored to stringResource(R.string.m_unmonitored), LibFilter.Missing to stringResource(R.string.m_missing)), filter) { filter = it }
                Spacer(Modifier.height(6.dp))
                ChipRow(listOf(LibSort.Title to stringResource(R.string.m_sort_title), LibSort.Added to stringResource(R.string.m_sort_added),
                    LibSort.Year to stringResource(R.string.m_sort_year), LibSort.Size to stringResource(R.string.m_sort_size)), sort) { sort = it }
            }
            loaded(all) { _ ->
                if (shown.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
                keyedItems(shown, { it.i("id") }, "library") { o ->
                    val id = o.i("id")
                    val on = id != null && id in selected
                    fun toggle() { if (id != null) selected = if (on) selected - id else selected + id }
                    ListRow(Lib.title(o), librarySubtitle(s, o), image = Lib.poster(c, o),
                        modifier = if (on) Modifier.background(Kino.surface2) else Modifier,
                        leading = if (selecting) { { Checkbox(on, { toggle() }, colors = CheckboxDefaults.colors(checkedColor = Kino.text, checkmarkColor = Kino.bg)) } } else null,
                        trailing = { Dot(when { o.b("monitored") != true -> Kino.faint; Lib.complete(s, o) -> Kino.success; else -> Kino.accent }) },
                        onLongClick = { if (!selecting) selecting = true; toggle() },
                    ) { if (selecting) toggle() else id?.let { nav.manage(s, "item", it.toString()) } }
                }
                if (selecting) item { Spacer(Modifier.height(150.dp)) }
            }
        }
        if (selecting) BulkBar(c, selected, Modifier.align(Alignment.BottomCenter)) { all.reload(); exitSelection() }
    }
}

private enum class BulkDialog { Profile, Root, Tags, Delete }

/** The actions on a multi-selection of the library, each sent to the editor endpoint in one call. */
@Composable
private fun BulkBar(c: ArrClient, selected: Set<Int>, modifier: Modifier, onDone: () -> Unit) {
    val s = c.service
    val (act, run) = rememberAction()
    var dialog by remember { mutableStateOf<BulkDialog?>(null) }
    val doneMsg = plural(R.plurals.m_bulk_done, selected.size)
    fun edit(e: BulkEdit) = run(doneMsg) { c.put(BulkEditor.resource(s), BulkEditor.editBody(s, selected, e)); onDone() }
    val enabled = selected.isNotEmpty() && !act.busy
    Surface(modifier.fillMaxWidth().navigationBarsPadding().padding(12.dp), shape = RoundedCornerShape(12.dp), color = Kino.surface,
        border = androidx.compose.foundation.BorderStroke(1.dp, Kino.border), shadowElevation = 12.dp) {
        Column(Modifier.padding(vertical = 8.dp)) {
            if (act.busy) LinearProgressIndicator(Modifier.fillMaxWidth().padding(horizontal = 12.dp), color = Kino.text, trackColor = Kino.surface3)
            Row(Modifier.fillMaxWidth().horizontalScroll(androidx.compose.foundation.rememberScrollState()).padding(horizontal = 4.dp)) {
                BulkButton(Icons.Outlined.Visibility, stringResource(R.string.m_monitor_on), enabled) { edit(BulkEdit(monitored = true)) }
                BulkButton(Icons.Outlined.VisibilityOff, stringResource(R.string.m_monitor_off), enabled) { edit(BulkEdit(monitored = false)) }
                BulkButton(Icons.Outlined.HighQuality, stringResource(R.string.m_quality_profile), enabled) { dialog = BulkDialog.Profile }
                BulkButton(Icons.AutoMirrored.Outlined.DriveFileMove, stringResource(R.string.m_root_folder), enabled) { dialog = BulkDialog.Root }
                BulkButton(Icons.AutoMirrored.Outlined.Label, stringResource(R.string.m_tags), enabled) { dialog = BulkDialog.Tags }
                BulkButton(Icons.Outlined.DeleteOutline, stringResource(R.string.delete), enabled, danger = true) { dialog = BulkDialog.Delete }
            }
            Column(Modifier.padding(horizontal = 14.dp)) {
                if (selected.isEmpty()) Text(stringResource(R.string.m_select_hint), color = Kino.faint, fontSize = 12.sp)
                FormErrors(act.error, emptyList())
                act.done?.let { Text(it, color = Kino.success, fontSize = 13.sp) }
            }
        }
    }
    when (dialog) {
        BulkDialog.Profile -> {
            val profiles = rememberLoaded("bulk/qp/$s") { c.list("qualityprofile") }
            var pick by remember { mutableStateOf<Int?>(null) }
            BulkDialogFrame(stringResource(R.string.m_quality_profile), selected.size, pick != null, { dialog = null }, { dialog = null; edit(BulkEdit(qualityProfileId = pick)) }) {
                val ps = profiles.value
                if (ps == null) Loading() else Picker(pick, ps.map { it.i("id") to (it.s("name") ?: "?") }, { pick = it })
            }
        }
        BulkDialog.Root -> {
            val roots = rememberLoaded("bulk/rf/$s") { c.list("rootfolder") }
            var pick by remember { mutableStateOf<String?>(null) }
            var move by remember { mutableStateOf(false) }
            BulkDialogFrame(stringResource(R.string.m_root_folder), selected.size, pick != null, { dialog = null }, { dialog = null; edit(BulkEdit(rootFolderPath = pick, moveFiles = move)) }) {
                val r = roots.value
                if (r == null) Loading() else Picker(pick, r.map { it.s("path") to "${it.s("path")} (${bytes(it.l("freeSpace"))})" }, { pick = it })
                Toggle(stringResource(R.string.m_move_files), stringResource(R.string.m_move_files_hint), move) { move = it }
            }
        }
        BulkDialog.Tags -> {
            var tags by remember { mutableStateOf(listOf<Int>()) }
            var mode by remember { mutableStateOf(TagMode.Add) }
            BulkDialogFrame(stringResource(R.string.m_tags), selected.size, tags.isNotEmpty() || mode == TagMode.Replace, { dialog = null }, { dialog = null; edit(BulkEdit(tags = tags, tagMode = mode)) }) {
                Picker(mode, listOf(TagMode.Add to stringResource(R.string.m_tags_add), TagMode.Remove to stringResource(R.string.m_tags_remove), TagMode.Replace to stringResource(R.string.m_tags_replace)), { mode = it })
                TagPicker(c, tags) { tags = it }
            }
        }
        BulkDialog.Delete -> DeleteDialog(onDismiss = { dialog = null }, message = plural(R.plurals.m_bulk_delete_confirm, selected.size)) { files, exclude ->
            run(doneMsg) { c.request("DELETE", BulkEditor.resource(s), BulkEditor.deleteBody(s, selected, files, exclude), emptyMap()); onDone() }
        }
        null -> {}
    }
}

@Composable
private fun BulkButton(icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, enabled: Boolean, danger: Boolean = false, onClick: () -> Unit) {
    val tint = when { !enabled -> Kino.faint; danger -> Kino.danger; else -> Kino.text }
    Column(Modifier.widthIn(min = 76.dp).clip(RoundedCornerShape(8.dp)).clickable(enabled = enabled, onClick = onClick).padding(horizontal = 8.dp, vertical = 6.dp),
        horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Icon(icon, null, tint = tint)
        Text(label, color = tint, fontSize = 11.sp, maxLines = 1)
    }
}

@Composable
private fun BulkDialogFrame(title: String, count: Int, canApply: Boolean, onDismiss: () -> Unit, onApply: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    AlertDialog(onDismissRequest = onDismiss, containerColor = Kino.surface,
        title = { Text(title, color = Kino.text) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(plural(R.plurals.m_applies_to_n, count), color = Kino.muted, fontSize = 13.sp)
                content()
            }
        },
        confirmButton = { TextButton(onClick = onApply, enabled = canApply) { Text(stringResource(R.string.m_apply), color = if (canApply) Kino.text else Kino.faint, fontWeight = FontWeight.SemiBold) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel), color = Kino.muted) } })
}

@Composable
private fun librarySubtitle(s: ArrService, o: JsonObject): String {
    val st = o.o("statistics")
    val progress = when (s) {
        ArrService.Sonarr -> st?.let { "${it.i("episodeFileCount") ?: 0}/${it.i("episodeCount") ?: 0}" }
        ArrService.Lidarr -> st?.let { "${it.i("trackFileCount") ?: 0}/${it.i("trackCount") ?: 0}" }
        else -> if (o.b("hasFile") == true) stringResource(R.string.m_downloaded) else stringResource(R.string.m_missing)
    }
    return listOfNotNull(o.i("year")?.takeIf { it > 0 }?.toString(), progress, Lib.sizeOnDisk(o)?.takeIf { it > 0 }?.let(::bytes), o.s("status")?.let(::humanize)).joinToString(" · ")
}

/** One movie, series or artist: files, history, monitoring, profile, edit, delete and searches. */
@Composable
fun ItemPage(nav: NavHostController, c: ArrClient, id: Int) {
    val s = c.service
    val res = Lib.resource(s)
    val item = rememberLoaded(id) { c.obj("$res/$id") }
    val profiles = rememberLoaded(s) { c.list("qualityprofile") }
    val (act, run) = rememberAction()
    var askDelete by remember { mutableStateOf(false) }
    var edited by remember(item.value) { mutableStateOf(item.value) }
    val queued = stringResource(R.string.m_command_queued)
    val savedMsg = stringResource(R.string.saved)
    // The series as the service has it, with the season switches that were saved right away.
    var stored by remember(item.value) { mutableStateOf(item.value) }
    val o = edited
    UnsavedChangesGuard(nav, o != null && stored != null && o != stored)
    ManagePage(nav, o?.let(Lib::title) ?: s.title, o?.let { listOfNotNull(it.i("year")?.takeIf { y -> y > 0 }?.toString(), it.s("status")?.let(::humanize), it.s("network") ?: it.s("studio")).joinToString(" · ") }) {
        loaded(item) { _ ->
            val cur = o ?: return@loaded
            item {
                Row(Modifier.padding(16.dp), horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    Box(Modifier.width(110.dp).aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(Kino.surface2)) { ServerImage(Lib.poster(c, cur), Modifier.fillMaxSize()) }
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(cur.s("overview").orEmpty(), color = Kino.muted, fontSize = 13.sp, maxLines = 8)
                        Text(listOfNotNull(Lib.sizeOnDisk(cur)?.let(::bytes), cur.s("path")).joinToString(" · "), color = Kino.faint, fontSize = 12.sp)
                    }
                }
            }
            block {
                Actions {
                    GlassButton(stringResource(R.string.m_auto_search), { val (n, a) = Lib.search(s, id); run(queued) { c.command(n, *a) } }, icon = Icons.Outlined.Search)
                    GlassButton(stringResource(R.string.m_interactive_search), { nav.manage(s, "releases", id.toString()) }, icon = Icons.AutoMirrored.Outlined.ManageSearch)
                    GlassButton(stringResource(R.string.m_refresh), { val (n, a) = Lib.refresh(s, id); run(queued) { c.command(n, *a) } }, icon = Icons.Outlined.Refresh)
                    Lib.rescan(s, id)?.let { (n, a) -> GlassButton(stringResource(R.string.m_rescan), { run(queued) { c.command(n, *a) } }, icon = Icons.Outlined.FolderOpen) }
                }
                ActionOutcome(act)
            }
            block {
                Card(stringResource(R.string.edit)) {
                    Toggle(stringResource(R.string.m_monitored), null, cur.b("monitored") == true) { edited = cur.with("monitored", it) }
                    profiles.value?.let { ps ->
                        FieldRow(stringResource(R.string.m_quality_profile)) {
                            Picker(cur.i("qualityProfileId"), ps.map { it.i("id") to (it.s("name") ?: "?") }, { edited = cur.with("qualityProfileId", it) })
                        }
                    }
                    when (s) {
                        ArrService.Radarr -> FieldRow(stringResource(R.string.m_min_availability)) {
                            Picker(cur.s("minimumAvailability"), listOf("announced", "inCinemas", "released").map { it to humanize(it) }, { edited = cur.with("minimumAvailability", it) })
                        }
                        ArrService.Sonarr -> {
                            FieldRow(stringResource(R.string.m_series_type)) {
                                Picker(cur.s("seriesType"), listOf("standard", "daily", "anime").map { it to humanize(it) }, { edited = cur.with("seriesType", it) })
                            }
                            Toggle(stringResource(R.string.m_season_folder), null, cur.b("seasonFolder") == true) { edited = cur.with("seasonFolder", it) }
                        }
                        else -> MetadataProfilePicker(c, cur) { edited = it }
                    }
                    FormText(stringResource(R.string.m_path), cur.s("path").orEmpty(), { edited = cur.with("path", it) }, error = (act.error as? ArrException)?.messageFor("path"))
                    TagPicker(c, cur.ints("tags")) { edited = cur.with("tags", it) }
                    if (cur.s("path") != item.value?.s("path")) Text(stringResource(R.string.m_path_moves_files), color = Kino.faint, fontSize = 12.sp)
                    PrimaryButton(stringResource(R.string.save), {
                        val moved = cur.s("path") != item.value?.s("path")
                        run(savedMsg) { c.put("$res/$id", cur, if (moved) mapOf("moveFiles" to true) else emptyMap()); item.reload() }
                    }, Modifier.fillMaxWidth(), enabled = !act.busy && cur != stored)
                }
            }
            when (s) {
                ArrService.Sonarr -> item {
                    // A season switch saves at once: it is applied to the series as the service has
                    // it (not to unsaved edits of the form, like a new path) and to the form.
                    SeasonsBlock(nav, c, id, cur) { n, on ->
                        edited = Lib.withSeasonMonitored(cur, n, on)
                        stored?.let { Lib.withSeasonMonitored(it, n, on) }?.also { stored = it }
                    }
                }
                ArrService.Lidarr -> item { AlbumsBlock(nav, c, id) }
                else -> {}
            }
            item { FilesBlock(c, id) }
            item { ItemHistoryBlock(c, id) }
            block { DangerAction(stringResource(R.string.delete)) { askDelete = true } }
        }
    }
    if (askDelete) DeleteDialog(onDismiss = { askDelete = false }) { files, exclude ->
        run(null) {
            c.delete("$res/$id", mapOf("deleteFiles" to files, if (s == ArrService.Radarr) "addImportExclusion" to exclude else "addImportListExclusion" to exclude))
            nav.popBackStack()
        }
    }
}

@Composable
fun DeleteDialog(onDismiss: () -> Unit, message: String? = null, onConfirm: (deleteFiles: Boolean, exclude: Boolean) -> Unit) {
    var files by remember { mutableStateOf(false) }
    var exclude by remember { mutableStateOf(false) }
    AlertDialog(onDismissRequest = onDismiss, containerColor = Kino.surface,
        title = { Text(stringResource(R.string.m_delete_title), color = Kino.text) },
        text = {
            Column {
                message?.let { Text(it, color = Kino.text, fontSize = 14.sp) }
                Toggle(stringResource(R.string.m_delete_files), stringResource(R.string.m_delete_files_hint), files) { files = it }
                Toggle(stringResource(R.string.m_add_exclusion), null, exclude) { exclude = it }
            }
        },
        confirmButton = { TextButton(onClick = { onDismiss(); onConfirm(files, exclude) }) { Text(stringResource(R.string.delete), color = Kino.danger) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel), color = Kino.muted) } })
}

@Composable
private fun MetadataProfilePicker(c: ArrClient, cur: JsonObject, onChange: (JsonObject) -> Unit) {
    val mps = rememberLoaded(c.service) { c.list("metadataprofile") }
    mps.value?.let { ps ->
        FieldRow(stringResource(R.string.m_metadata_profile)) {
            Picker(cur.i("metadataProfileId"), ps.map { it.i("id") to (it.s("name") ?: "?") }, { onChange(cur.with("metadataProfileId", it)) })
        }
    }
}

/** Tags of an item (ids), chosen from the service's tags. */
@Composable
fun TagPicker(c: ArrClient, selected: List<Int>, onChange: (List<Int>) -> Unit) {
    val tags = rememberLoaded(c.service) { c.list("tag") }
    val opts = tags.value?.map { SelectOption(toJson(it.i("id")), it.s("label") ?: "?") } ?: return
    if (opts.isEmpty()) return
    MultiPick(stringResource(R.string.m_tags), opts, selected.map { toJson(it) }) { onChange(it.mapNotNull { e -> e.toString().toIntOrNull() }) }
}

@Composable
private fun SeasonsBlock(nav: NavHostController, c: ArrClient, seriesId: Int, series: JsonObject, onSeason: (season: Int, on: Boolean) -> JsonObject?) {
    val episodes = rememberLoaded(seriesId) { c.list("episode", mapOf("seriesId" to seriesId)) }
    val (act, run) = rememberAction()
    val queued = stringResource(R.string.m_command_queued)
    var open by remember { mutableStateOf<Int?>(null) }
    Column {
        Heading(stringResource(R.string.m_seasons))
        ActionOutcome(act)
        series.objs("seasons").sortedByDescending { it.i("seasonNumber") ?: 0 }.forEach { season ->
            val n = season.i("seasonNumber") ?: 0
            val st = season.o("statistics")
            Panel {
                ListRow(if (n == 0) stringResource(R.string.m_specials) else stringResource(R.string.m_season_n, n),
                    st?.let { "${it.i("episodeFileCount") ?: 0}/${it.i("episodeCount") ?: 0} · ${bytes(it.l("sizeOnDisk"))}" },
                    trailing = {
                        Switch(season.b("monitored") == true, { m ->
                            if (!act.busy) onSeason(n, m)?.let { stored -> run(null) { c.put("series/$seriesId", stored) } }
                        }, enabled = !act.busy, colors =SwitchDefaults.colors(checkedThumbColor = Kino.bg, checkedTrackColor = Kino.text))
                    }) { open = if (open == n) null else n }
                if (open == n) {
                    Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        TextButton(onClick = { run(queued) { c.command("SeasonSearch", "seriesId" to seriesId, "seasonNumber" to n) } }) { Text(stringResource(R.string.m_auto_search)) }
                        TextButton(onClick = { nav.manage(c.service, "releases", seriesId.toString(), "season:$n") }) { Text(stringResource(R.string.m_interactive_search)) }
                    }
                    episodes.value?.filter { it.i("seasonNumber") == n }?.sortedByDescending { it.i("episodeNumber") ?: 0 }?.forEach { e ->
                        val eid = e.i("id") ?: return@forEach
                        ListRow(listOfNotNull(e.i("episodeNumber")?.toString(), e.s("title")).joinToString(". ").ifEmpty { "?" },
                            listOfNotNull(e.s("airDate"), if (e.b("hasFile") == true) stringResource(R.string.m_downloaded) else stringResource(R.string.m_missing)).joinToString(" · "),
                            trailing = {
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    IconButton(onClick = { run(queued) { c.command("EpisodeSearch", "episodeIds" to listOf(eid)) } }) { Icon(Icons.Outlined.Search, stringResource(R.string.m_auto_search), tint = Kino.muted) }
                                    IconButton(onClick = { nav.manage(c.service, "releases", seriesId.toString(), "episode:$eid") }) { Icon(Icons.AutoMirrored.Outlined.ManageSearch, stringResource(R.string.m_interactive_search), tint = Kino.muted) }
                                    Checkbox(e.b("monitored") == true, { m -> run(null) { c.put("episode/monitor", jsonOf("episodeIds" to listOf(eid), "monitored" to m)); episodes.reload() } },
                                        colors = CheckboxDefaults.colors(checkedColor = Kino.text, checkmarkColor = Kino.bg))
                                }
                            })
                    }
                }
            }
        }
    }
}

@Composable
private fun AlbumsBlock(nav: NavHostController, c: ArrClient, artistId: Int) {
    val albums = rememberLoaded(artistId) { c.list("album", mapOf("artistId" to artistId)) }
    val (act, run) = rememberAction()
    val queued = stringResource(R.string.m_command_queued)
    var open by remember { mutableStateOf<Int?>(null) }
    Column {
        Heading(stringResource(R.string.albums))
        ActionOutcome(act)
        albums.value?.sortedByDescending { it.s("releaseDate").orEmpty() }?.forEach { a ->
            val aid = a.i("id") ?: return@forEach
            val st = a.o("statistics")
            ListRow(a.s("title") ?: "?", listOfNotNull(a.s("releaseDate")?.take(4), a.s("albumType"), st?.let { "${it.i("trackFileCount") ?: 0}/${it.i("trackCount") ?: 0}" }).joinToString(" · "),
                image = Lib.poster(c, a), trailing = {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        IconButton(onClick = { run(queued) { c.command("AlbumSearch", "albumIds" to listOf(aid)) } }) { Icon(Icons.Outlined.Search, stringResource(R.string.m_auto_search), tint = Kino.muted) }
                        IconButton(onClick = { nav.manage(c.service, "releases", artistId.toString(), "album:$aid") }) { Icon(Icons.AutoMirrored.Outlined.ManageSearch, stringResource(R.string.m_interactive_search), tint = Kino.muted) }
                        Checkbox(a.b("monitored") == true, { m -> run(null) { c.put("album/monitor", jsonOf("albumIds" to listOf(aid), "monitored" to m)); albums.reload() } },
                            colors = CheckboxDefaults.colors(checkedColor = Kino.text, checkmarkColor = Kino.bg))
                    }
                }) { open = if (open == aid) null else aid }
            if (open == aid) AlbumTracks(c, aid)
        }
        if (albums.loading && albums.value == null) Loading()
    }
}

/** The tracks of one Lidarr album (`track?albumId=`), with whether each has a file. */
@Composable
private fun AlbumTracks(c: ArrClient, albumId: Int) {
    val tracks = rememberLoaded("tracks/$albumId") { c.list("track", mapOf("albumId" to albumId)) }
    Column(Modifier.padding(start = 24.dp)) {
        if (tracks.value == null && tracks.error == null) Loading()
        ArrError(tracks.error)
        tracks.value?.sortedWith(compareBy({ it.i("mediumNumber") ?: 0 }, { it.i("absoluteTrackNumber") ?: it.s("trackNumber")?.toIntOrNull() ?: 0 }))?.forEach { t ->
            val has = t.b("hasFile") == true
            ListRow(listOfNotNull(t.s("trackNumber"), t.s("title")).joinToString(". "),
                listOfNotNull(t.l("duration")?.takeIf { it > 0 }?.let { ms -> "%d:%02d".format(ms / 60000, (ms / 1000) % 60) },
                    stringResource(if (has) R.string.m_downloaded else R.string.m_missing)).joinToString(" · "),
                trailing = { Dot(if (has) Kino.success else Kino.faint) })
        }
    }
}

@Composable
private fun FilesBlock(c: ArrClient, id: Int) {
    val s = c.service
    val files = rememberLoaded(id) { c.list(Lib.filesResource(s), mapOf(Lib.idParam(s) to id)) }
    val (act, run) = rememberAction()
    var confirm by remember { mutableStateOf<Int?>(null) }
    val list = files.value ?: return
    if (list.isEmpty()) return
    Column {
        Heading(stringResource(R.string.m_files))
        ActionOutcome(act)
        list.forEach { f ->
            ListRow(f.s("relativePath") ?: f.s("path") ?: "?",
                listOfNotNull(f.o("quality")?.o("quality")?.s("name"), bytes(f.l("size")), f.o("mediaInfo")?.let { mi -> listOfNotNull(mi.s("videoCodec"), mi.s("audioCodec"), mi.s("resolution")).joinToString(" ") }).joinToString(" · "),
                trailing = { IconButton(onClick = { confirm = f.i("id") }) { Icon(Icons.Outlined.DeleteOutline, stringResource(R.string.delete), tint = Kino.danger) } })
        }
    }
    confirm?.let { fid ->
        ConfirmDialog(stringResource(R.string.m_delete_file_confirm), stringResource(R.string.delete), { confirm = null }) {
            run(null) { c.delete("${Lib.filesResource(s)}/$fid"); files.reload() }
        }
    }
}

@Composable
private fun ItemHistoryBlock(c: ArrClient, id: Int) {
    val s = c.service
    val h = rememberLoaded(id) { c.list(Lib.historyResource(s), mapOf(Lib.idParam(s) to id) + Lib.include(s)) }
    val list = h.value ?: return
    if (list.isEmpty()) return
    Column {
        Heading(stringResource(R.string.m_history))
        list.sortedByDescending { it.s("date").orEmpty() }.take(30).forEach { r ->
            ListRow(r.s("sourceTitle") ?: "?", listOfNotNull(r.s("eventType")?.let(::humanize), r.o("quality")?.o("quality")?.s("name"), when_(r.s("date"))).joinToString(" · "))
        }
    }
}

/** Add a movie, series or artist: lookup, then root folder, profile, monitoring and search on add. */
@Composable
fun AddPage(nav: NavHostController, c: ArrClient) {
    val s = c.service
    var term by rememberSaveable { mutableStateOf("") }
    var submitted by rememberSaveable { mutableStateOf("") }
    val results = rememberLoaded(submitted) { if (submitted.isBlank()) emptyList() else c.list("${Lib.resource(s)}/lookup", mapOf("term" to submitted)) }
    var chosen by remember { mutableStateOf<JsonObject?>(null) }
    ManagePage(nav, stringResource(R.string.m_add_new), s.title) {
        item { SearchField(term, { term = it }, stringResource(R.string.m_lookup_hint), Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) { submitted = term.trim() } }
        block { GlassButton(stringResource(R.string.search), { submitted = term.trim() }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Search) }
        if (submitted.isNotBlank()) loaded(results) { list ->
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_found), color = Kino.muted) }
            keyedItems(list, { it.i("tmdbId") ?: it.i("tvdbId") ?: it.s("foreignArtistId") ?: it.i("id") }, "lookup") { o ->
                val inLib = (o.i("id") ?: 0) > 0
                ListRow(Lib.title(o), listOfNotNull(o.i("year")?.takeIf { it > 0 }?.toString(), o.s("network") ?: o.s("studio") ?: o.s("artistType"), o.s("overview")?.take(140)).joinToString(" · "),
                    image = Lib.poster(c, o), trailing = { if (inLib) Pill(stringResource(R.string.m_in_library), Kino.success) }) {
                    if (inLib) nav.manage(s, "item", o.i("id").toString()) else chosen = o
                }
            }
        }
    }
    chosen?.let { o -> AddDialog(c, o, onDismiss = { chosen = null }) { id -> chosen = null; nav.popBackStack(); if (id > 0) nav.manage(s, "item", id.toString()) } }
}

@Composable
private fun AddDialog(c: ArrClient, lookup: JsonObject, onDismiss: () -> Unit, onAdded: (Int) -> Unit) {
    val s = c.service
    val roots = rememberLoaded(s) { c.list("rootfolder") }
    val profiles = rememberLoaded(s) { c.list("qualityprofile") }
    val metadata = rememberLoaded(s) { if (s == ArrService.Lidarr) c.list("metadataprofile") else emptyList() }
    var root by remember { mutableStateOf<String?>(null) }
    var profile by remember { mutableStateOf<Int?>(null) }
    var meta by remember { mutableStateOf<Int?>(null) }
    var monitor by remember { mutableStateOf(Lib.monitorOptions(s).first()) }
    var search by remember { mutableStateOf(true) }
    var minAvail by remember { mutableStateOf("released") }
    var seriesType by remember { mutableStateOf(lookup.s("seriesType") ?: "standard") }
    var seasonFolder by remember { mutableStateOf(true) }
    var tags by remember { mutableStateOf(listOf<Int>()) }
    val (act, run) = rememberAction()
    LaunchedEffect(roots.value, profiles.value, metadata.value) {
        if (root == null) root = roots.value?.firstOrNull()?.s("path")
        if (profile == null) profile = profiles.value?.firstOrNull()?.i("id")
        if (meta == null) meta = metadata.value?.firstOrNull()?.i("id")
    }
    AlertDialog(onDismissRequest = onDismiss, containerColor = Kino.surface,
        title = { Text(Lib.title(lookup), color = Kino.text) },
        text = {
            Column(Modifier.verticalScroll(androidx.compose.foundation.rememberScrollState()), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                roots.value?.let { r -> FieldRow(stringResource(R.string.m_root_folder)) { Picker(root, r.map { it.s("path") to "${it.s("path")} (${bytes(it.l("freeSpace"))})" }, { root = it }) } }
                if (roots.value?.isEmpty() == true) Text(stringResource(R.string.m_no_root_folders), color = Kino.danger, fontSize = 13.sp)
                profiles.value?.let { p -> FieldRow(stringResource(R.string.m_quality_profile)) { Picker(profile, p.map { it.i("id") to (it.s("name") ?: "?") }, { profile = it }) } }
                metadata.value?.takeIf { it.isNotEmpty() }?.let { p -> FieldRow(stringResource(R.string.m_metadata_profile)) { Picker(meta, p.map { it.i("id") to (it.s("name") ?: "?") }, { meta = it }) } }
                FieldRow(stringResource(R.string.m_monitor)) { Picker(monitor, Lib.monitorOptions(s).map { it to humanize(it) }, { monitor = it }) }
                if (s == ArrService.Radarr) FieldRow(stringResource(R.string.m_min_availability)) { Picker(minAvail, listOf("announced", "inCinemas", "released").map { it to humanize(it) }, { minAvail = it }) }
                if (s == ArrService.Sonarr) {
                    FieldRow(stringResource(R.string.m_series_type)) { Picker(seriesType, listOf("standard", "daily", "anime").map { it to humanize(it) }, { seriesType = it }) }
                    Toggle(stringResource(R.string.m_season_folder), null, seasonFolder) { seasonFolder = it }
                }
                TagPicker(c, tags) { tags = it }
                Toggle(stringResource(R.string.m_search_on_add), null, search) { search = it }
                FormErrors(act.error, emptyList())
            }
        },
        confirmButton = {
            TextButton(enabled = !act.busy && root != null && profile != null, onClick = {
                run(null) {
                    var body = lookup.with("rootFolderPath", root).with("qualityProfileId", profile).with("monitored", monitor != "none").with("tags", tags)
                    body = when (s) {
                        ArrService.Radarr -> body.with("minimumAvailability", minAvail).with("addOptions", jsonOf("searchForMovie" to search, "monitor" to monitor))
                        ArrService.Sonarr -> body.with("seriesType", seriesType).with("seasonFolder", seasonFolder).with("languageProfileId", body.i("languageProfileId") ?: 1)
                            .with("addOptions", jsonOf("searchForMissingEpisodes" to search, "monitor" to monitor))
                        else -> body.with("metadataProfileId", meta).with("monitorNewItems", "all").with("addOptions", jsonOf("searchForMissingAlbums" to search, "monitor" to monitor))
                    }
                    val added = c.post(Lib.resource(s), body) as? JsonObject
                    onAdded(added?.i("id") ?: 0)
                }
            }) { Text(stringResource(R.string.add), color = Kino.text, fontWeight = FontWeight.SemiBold) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel), color = Kino.muted) } })
}

/** Interactive search: the releases the indexers have for an item, season, episode or album, to grab one. */
@Composable
fun ReleasesPage(nav: NavHostController, c: ArrClient, id: Int, scope: String?) {
    val releases = rememberLoaded("$id/$scope") { c.list("release", Lib.releaseQuery(c.service, id, scope)) }
    var approvedOnly by remember { mutableStateOf(false) }
    val (act, run) = rememberAction()
    val grabbed = stringResource(R.string.m_grabbed)
    ManagePage(nav, stringResource(R.string.m_interactive_search), releases.value?.let { itemsCount(it.size) }) {
        item { ChipRow(listOf(false to stringResource(R.string.m_all), true to stringResource(R.string.m_approved)), approvedOnly) { approvedOnly = it } }
        block { ActionOutcome(act) }
        loaded(releases) { list -> releaseRows(list.filter { !approvedOnly || it.b("rejected") != true }) { r -> run(grabbed) { c.post("release", jsonOf("guid" to r.s("guid"), "indexerId" to r.i("indexerId"))) } } }
    }
}

/** Rows of releases (Radarr/Sonarr/Lidarr interactive search, Prowlarr search) with a grab button. */
fun androidx.compose.foundation.lazy.LazyListScope.releaseRows(list: List<JsonObject>, onGrab: (JsonObject) -> Unit) {
    if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_found), color = Kino.muted) }
    keyedItems(list, { it.s("guid") }, "release") { r ->
        val rejected = r.b("rejected") == true
        Panel {
            Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(r.s("title") ?: "?", color = if (rejected) Kino.muted else Kino.text, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                Text(listOfNotNull(r.s("indexer"), r.o("quality")?.o("quality")?.s("name"), bytes(r.l("size")),
                    r.i("seeders")?.let { "↑$it ↓${r.i("leechers") ?: 0}" }, r.d("ageHours")?.let { h -> if (h < 48) "${h.toInt()} h" else "${(h / 24).toInt()} d" } ?: r.i("age")?.let { "$it d" },
                    r.i("customFormatScore")?.takeIf { it != 0 }?.let { "CF $it" }, r.s("protocol")).joinToString(" · "), color = Kino.muted, fontSize = 12.sp)
                r.strs("rejections").take(3).forEach { Text("• $it", color = Kino.danger, fontSize = 12.sp) }
                Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
                    TextButton(onClick = { onGrab(r) }) {
                        Icon(Icons.Outlined.Download, null, Modifier.size(18.dp), tint = Kino.text)
                        Spacer(Modifier.width(6.dp))
                        Text(stringResource(R.string.m_grab), color = Kino.text)
                    }
                }
            }
        }
    }
}

