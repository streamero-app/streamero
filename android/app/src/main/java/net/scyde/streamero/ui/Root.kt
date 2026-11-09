package net.scyde.streamero.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.applyLanguage
import kotlinx.coroutines.launch

private enum class Gate { Loading, Server, Login, Profiles, Ready }

@Composable
fun StreameroRoot(state: AppState) {
    var gate by remember { mutableStateOf(Gate.Loading) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) {
        val api = state.session.restore()
        state.attach(api)
        gate = when {
            api == null -> Gate.Server
            api.token == null -> Gate.Login
            state.refreshMe() == null -> Gate.Login
            else -> afterSignIn(state)
        }
    }
    StreameroTheme {
        CompositionLocalProvider(LocalImages provides state.images) {
            Surface(Modifier.fillMaxSize(), color = Kino.bg) {
                when (gate) {
                    Gate.Loading -> Loading(Modifier.fillMaxSize())
                    Gate.Server -> ServerScreen(state) { gate = Gate.Login }
                    Gate.Login -> LoginScreen(state, onChangeServer = { gate = Gate.Server }) { scope.launch { gate = afterSignIn(state) } }
                    Gate.Profiles -> ProfileGate(state) { gate = Gate.Ready }
                    Gate.Ready -> Main(state, onSignedOut = { gate = Gate.Login }, onSwitchProfile = { gate = Gate.Profiles })
                }
            }
        }
    }
}

private suspend fun afterSignIn(state: AppState): Gate {
    state.me?.language?.let { applyLanguage(it) }
    val profiles = runCatching { state.api!!.profiles() }.getOrDefault(emptyList())
    return if (profiles.isNotEmpty() && state.me?.currentProfile == null) Gate.Profiles else Gate.Ready
}

private data class Tab(val route: String, val label: Int, val icon: ImageVector)

@Composable
private fun Main(state: AppState, onSignedOut: () -> Unit, onSwitchProfile: () -> Unit) {
    val nav = rememberNavController()
    val tabs = listOf(
        Tab("home", R.string.nav_home, Icons.Outlined.Home),
        Tab("search", R.string.nav_search, Icons.Outlined.Search),
        Tab("discover", R.string.nav_discover, Icons.Outlined.Explore),
        Tab("libraries", R.string.nav_libraries, Icons.Outlined.VideoLibrary),
        Tab("me", R.string.nav_me, Icons.Outlined.AccountCircle),
    )
    val entry by nav.currentBackStackEntryAsState()
    val route = entry?.destination?.route.orEmpty()
    val watching = route.startsWith("watch")
    Scaffold(
        containerColor = Kino.bg,
        bottomBar = {
            if (!watching) Column {
                MiniPlayer(state, nav)
                NavigationBar(containerColor = Kino.bg.copy(alpha = 0.96f), tonalElevation = 0.dp) {
                    tabs.forEach { t ->
                        NavigationBarItem(selected = route == t.route, onClick = {
                            nav.navigate(t.route) { popUpTo("home") { saveState = true }; launchSingleTop = true; restoreState = true }
                        }, icon = { Icon(t.icon, null) }, label = { Text(stringResource(t.label)) },
                            colors = NavigationBarItemDefaults.colors(selectedIconColor = Kino.text, selectedTextColor = Kino.text,
                                unselectedIconColor = Kino.faint, unselectedTextColor = Kino.faint, indicatorColor = Kino.surface3))
                    }
                }
            }
        },
    ) { pad ->
        Box(Modifier.padding(bottom = if (watching) 0.dp else pad.calculateBottomPadding()).background(Kino.bg)) {
            Routes(state, nav, onSignedOut, onSwitchProfile)
        }
    }
}

@Composable
private fun Routes(state: AppState, nav: NavHostController, onSignedOut: () -> Unit, onSwitchProfile: () -> Unit) {
    NavHost(nav, startDestination = "home") {
        composable("home") { HomeScreen(state, nav) }
        composable("search") { SearchScreen(state, nav) }
        composable("discover") { DiscoverScreen(state, nav) }
        composable("libraries") { LibrariesScreen(state, nav) }
        composable("me") { MeScreen(state, nav, onSignedOut, onSwitchProfile) }
        composable("library/{id}") { LibraryScreen(state, nav, it.arguments?.getString("id")!!) }
        composable("item/{id}") { ItemScreen(state, nav, it.arguments?.getString("id")!!) }
        composable("collection/{id}") { CollectionScreen(state, nav, it.arguments?.getString("id")!!) }
        composable("collections") { CollectionsScreen(state, nav) }
        composable("playlist/{id}") { PlaylistScreen(state, nav, it.arguments?.getString("id")!!) }
        composable("watch/{id}?start={start}&file={file}&audio={audio}&sub={sub}", arguments = listOf(
            navArgument("start") { type = NavType.LongType; defaultValue = -1L },
            navArgument("file") { type = NavType.StringType; nullable = true },
            navArgument("audio") { type = NavType.IntType; defaultValue = -1 },
            navArgument("sub") { type = NavType.IntType; defaultValue = -2 },
        )) {
            val a = it.arguments!!
            PlayerScreen(state, nav, a.getString("id")!!, a.getLong("start").takeIf { s -> s >= 0 }, a.getString("file"),
                a.getInt("audio").takeIf { x -> x >= 0 }, a.getInt("sub").takeIf { x -> x >= -1 })
        }
        composable("settings/{page}") { SettingsPage(state, nav, it.arguments?.getString("page")!!) }
    }
}

fun NavHostController.watch(id: String, start: Long? = null, file: String? = null, audio: Int? = null, sub: Int? = null) {
    val q = buildList {
        if (start != null) add("start=$start")
        if (file != null) add("file=$file")
        if (audio != null) add("audio=$audio")
        if (sub != null) add("sub=$sub")
    }.joinToString("&")
    navigate(if (q.isEmpty()) "watch/$id" else "watch/$id?$q")
}
