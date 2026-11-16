package net.scyde.streamero.manage

import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.ui.Kino
import net.scyde.streamero.ui.Tab

/** Streamero Manage: the Manage tab (admins only) and its routes (D-048). */
internal object ManageEntry {
    const val ENABLED = true

    fun tab(state: AppState): Tab? = if (state.isAdmin) Tab("manage", R.string.nav_manage, Icons.Outlined.Tune) else null

    fun routes(graph: NavGraphBuilder, state: AppState, nav: NavHostController) {
        graph.composable("manage") { HubScreen(state, nav) }
        graph.composable("manage/{svc}/{page}?id={id}&arg={arg}", arguments = listOf(
            navArgument("id") { type = NavType.StringType; nullable = true },
            navArgument("arg") { type = NavType.StringType; nullable = true },
        )) {
            val a = it.arguments!!
            val svc = ArrService.of(a.getString("svc")) ?: return@composable
            ManageRoute(state, nav, svc, a.getString("page")!!, a.getString("id"), a.getString("arg"))
        }
    }
}

@Composable
private fun ManageRoute(state: AppState, nav: NavHostController, svc: ArrService, page: String, id: String?, arg: String?) {
    // Kid profiles and non-admins never reach these (no tab), but a stale back stack might.
    if (!state.isAdmin) { Text(stringResource(R.string.err_forbidden), color = Kino.danger, modifier = Modifier.statusBarsPadding().padding(20.dp)); return }
    val c = rememberArr(state, svc) ?: return
    when (page) {
        "home" -> ServiceHome(nav, c)
        // Radarr, Sonarr, Lidarr
        "library" -> LibraryPage(nav, c)
        "item" -> ItemPage(nav, c, id!!.toInt())
        "add" -> AddPage(nav, c)
        "releases" -> ReleasesPage(nav, c, id!!.toInt(), arg)
        "calendar" -> CalendarPage(nav, c)
        "queue" -> QueuePage(nav, c)
        "history" -> HistoryPage(nav, c)
        "blocklist" -> BlocklistPage(nav, c)
        "wanted" -> WantedPage(nav, c, cutoff = arg == "cutoff")
        // Shared by the *arr family incl. Prowlarr
        "system" -> SystemPage(nav, c)
        "logs" -> LogsPage(nav, c)
        "backups" -> BackupsPage(nav, c)
        "settings" -> SettingsIndex(nav, c)
        "providers" -> ProviderList(nav, c, arg!!)
        "provider" -> ProviderEdit(nav, c, arg!!, id)
        "config" -> ConfigPage(nav, c, arg!!)
        "qualityprofiles" -> QualityProfileList(nav, c)
        "qualityprofile" -> QualityProfileEdit(nav, c, id)
        "qualitydefinitions" -> QualityDefinitionsPage(nav, c)
        "customformat" -> CustomFormatEdit(nav, c, id)
        "rootfolders" -> RootFoldersPage(nav, c)
        "tags" -> TagsPage(nav, c)
        "crud" -> CrudList(nav, c, arg!!)
        "crudedit" -> CrudEdit(nav, c, arg!!, id)
        // Prowlarr
        "search" -> ProwlarrSearch(nav, c)
        "stats" -> IndexerStats(nav, c)
        // Bazarr
        "bwanted" -> BazarrWanted(nav, c, arg ?: "movies")
        "bsubs" -> BazarrSubtitles(nav, c, arg ?: "movies", id!!)
        "bhistory" -> BazarrHistory(nav, c, arg ?: "movies")
        "bproviders" -> BazarrProviders(nav, c)
        "bsystem" -> BazarrSystem(nav, c)
        "bsettings" -> BazarrSettings(nav, c)
        // Seerr
        "requests" -> SeerrRequests(nav, c)
        "discover" -> SeerrDiscover(nav, c)
        "media" -> SeerrMedia(nav, c, arg ?: "movie", id!!.toInt())
        "issues" -> SeerrIssues(nav, c)
        "users" -> SeerrUsers(nav, c)
        "suser" -> SeerrUser(nav, c, id!!.toInt())
        "sissue" -> SeerrIssue(nav, c, id!!.toInt())
        "sjobs" -> SeerrJobs(nav, c)
        "ssettings" -> SeerrSettings(nav, c, arg)
    }
}
