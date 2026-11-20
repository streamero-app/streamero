package net.scyde.streamero.manage

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.serialization.json.JsonElement
import net.scyde.streamero.R
import net.scyde.streamero.ui.*

/** Prowlarr search across indexers (`GET search`), grab sends the release to a download client (`POST search`). */
@Composable
fun ProwlarrSearch(nav: NavHostController, c: ArrClient) {
    val indexers = rememberLoaded(c.service) { c.list("indexer") }
    var q by rememberSaveable { mutableStateOf("") }
    var type by rememberSaveable { mutableStateOf("search") }
    var chosen by remember { mutableStateOf<List<JsonElement>>(emptyList()) }
    var submitted by remember { mutableStateOf<Triple<String, String, List<JsonElement>>?>(null) }
    val results = rememberLoaded(submitted) {
        val s = submitted ?: return@rememberLoaded emptyList()
        c.list("search", mapOf("query" to s.first, "type" to s.second, "indexerIds" to s.third.map { it.toString() }, "limit" to 100))
    }
    val (act, run) = rememberAction()
    val grabbed = stringResource(R.string.m_grabbed)
    ManagePage(nav, stringResource(R.string.search), c.service.title) {
        item { SearchField(q, { q = it }, stringResource(R.string.m_search_indexers), Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) { submitted = Triple(q.trim(), type, chosen) } }
        item { ChipRow(listOf("search", "movie", "tvsearch", "music", "book").map { it to humanize(it) }, type) { type = it } }
        block {
            indexers.value?.let { l ->
                MultiPick(stringResource(R.string.m_indexers), l.map { SelectOption(toJson(it.i("id")), it.s("name") ?: "?") }, chosen, stringResource(R.string.m_all_indexers_hint)) { chosen = it }
            }
            GlassButton(stringResource(R.string.search), { submitted = Triple(q.trim(), type, chosen) }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Search, enabled = q.isNotBlank())
            ActionOutcome(act)
        }
        if (submitted != null) loaded(results) { list ->
            releaseRows(list.sortedByDescending { it.i("seeders") ?: 0 }) { r -> run(grabbed) { c.post("search", jsonOf("guid" to r.s("guid"), "indexerId" to r.i("indexerId"))) } }
        }
    }
}

/** Per-indexer statistics (`indexerstats`): queries, grabs, failures, response time. */
@Composable
fun IndexerStats(nav: NavHostController, c: ArrClient) {
    val stats = rememberLoaded(c.service) { c.obj("indexerstats") }
    ManagePage(nav, stringResource(R.string.m_stats), c.service.title) {
        loaded(stats) { s ->
            items(s.objs("indexers").sortedByDescending { it.i("numberOfQueries") ?: 0 }) { i ->
                Panel {
                    Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(i.s("indexerName") ?: "?", color = Kino.text, fontSize = 15.sp)
                        Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
                            Stat((i.i("numberOfQueries") ?: 0).toString(), stringResource(R.string.m_queries))
                            Stat((i.i("numberOfGrabs") ?: 0).toString(), stringResource(R.string.m_grabs))
                            Stat(((i.i("numberOfFailedQueries") ?: 0) + (i.i("numberOfFailedGrabs") ?: 0)).toString(), stringResource(R.string.m_failures), Kino.danger)
                            Stat("${i.i("averageResponseTime") ?: 0} ms", stringResource(R.string.m_response))
                        }
                    }
                }
            }
            if (s.objs("userAgents").isNotEmpty()) {
                item { Heading(stringResource(R.string.m_user_agents)) }
                items(s.objs("userAgents")) { u -> ListRow(u.s("userAgent") ?: "?", "${u.i("numberOfQueries") ?: 0} / ${u.i("numberOfGrabs") ?: 0}") }
            }
        }
    }
}
