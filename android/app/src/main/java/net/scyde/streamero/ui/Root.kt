package net.scyde.streamero.ui

import androidx.activity.compose.BackHandler
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusGroup
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.Alignment
import androidx.compose.ui.ExperimentalComposeUiApi
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.sp
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
import net.scyde.streamero.Connection
import net.scyde.streamero.R
import net.scyde.streamero.applyLanguage
import kotlinx.coroutines.launch

private enum class Gate { Loading, Server, Login, Profiles, Ready, Offline }

@Composable
fun StreameroRoot(state: AppState) {
    var gate by remember { mutableStateOf(Gate.Loading) }
    var attempt by remember { mutableIntStateOf(0) }
    var retrying by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(attempt) {
        retrying = true
        val api = state.session.restore()
        state.attach(api)
        gate = when {
            api == null -> Gate.Server
            api.token == null -> Gate.Login
            else -> when (state.connect()) {
                Connection.Ok -> afterSignIn(state).also { state.offline.syncSoon() }
                Connection.SignedOut -> Gate.Login
                // No server: the downloads still play.
                Connection.Unreachable -> Gate.Offline
            }
        }
        retrying = false
    }
    val ctx = LocalContext.current
    val tv = remember { isTelevision(ctx) }
    val density = LocalDensity.current
    StreameroTheme {
        // On a TV the type is set a size larger for reading from the sofa.
        CompositionLocalProvider(LocalImages provides state.images, LocalTv provides tv,
            LocalDensity provides if (tv) Density(density.density, density.fontScale * 1.12f) else density) {
            Surface(Modifier.fillMaxSize(), color = Kino.bg) {
                when (gate) {
                    Gate.Loading -> Loading(Modifier.fillMaxSize())
                    Gate.Server -> ServerScreen(state) { gate = Gate.Login }
                    Gate.Login -> LoginScreen(state, onChangeServer = { gate = Gate.Server }) { scope.launch { gate = afterSignIn(state) } }
                    Gate.Profiles -> ProfileGate(state) { gate = Gate.Ready }
                    Gate.Ready -> Main(state, onSignedOut = { gate = Gate.Login }, onSwitchProfile = { gate = Gate.Profiles })
                    Gate.Offline -> OfflineMain(state, retrying) { if (!retrying) attempt++ }
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

private val FULL_SCREEN = listOf("watch", "read/", "photo/", "listen/")

/** Without the server: the offline library and its player; connects again when the network returns. */
@Composable
private fun OfflineMain(state: AppState, retrying: Boolean, onRetry: () -> Unit) {
    val nav = rememberNavController()
    val online by state.offline.online.collectAsState()
    var wasOnline by remember { mutableStateOf(online) }
    LaunchedEffect(online) {
        if (online && !wasOnline) onRetry()
        wasOnline = online
    }
    NavHost(nav, startDestination = "downloads") {
        composable("downloads") { DownloadsScreen(state, onPlay = { nav.navigate("offline/$it") }, banner = { OfflineBanner(retrying, onRetry) }) }
        composable("offline/{id}") { OfflinePlayerScreen(state, it.arguments?.getString("id")!!) { nav.popBackStack() } }
    }
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
    // The player, the readers and the photo viewer use the whole screen.
    val watching = FULL_SCREEN.any { route.startsWith(it) } || route.startsWith("offline/")
    if (LocalTv.current) { TvMain(state, nav, tabs, route, watching, onSignedOut, onSwitchProfile); return }
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
        composable("shelf/{lib}?author={author}&series={series}", arguments = listOf(
            navArgument("author") { type = NavType.StringType; nullable = true },
            navArgument("series") { type = NavType.StringType; nullable = true },
        )) {
            val a = it.arguments!!
            ShelfScreen(state, nav, a.getString("lib")!!, a.getString("author"), a.getString("series"))
        }
        composable("read/{id}") { ReaderScreen(state, nav, it.arguments?.getString("id")!!) }
        composable("listen/{id}") { AudiobookPlayerScreen(state, nav, it.arguments?.getString("id")!!) }
        composable("photo/{id}") { PhotoViewerScreen(state, nav, it.arguments?.getString("id")!!) }
        composable("settings/{page}") { SettingsPage(state, nav, it.arguments?.getString("page")!!) }
        composable("downloads") { DownloadsScreen(state, onPlay = { id -> nav.navigate("offline/$id") }) }
        composable("offline/{id}") { OfflinePlayerScreen(state, it.arguments?.getString("id")!!) { nav.popBackStack() } }
        composable("together") { TogetherScreen(state, nav, null) }
        composable("together/{id}") { TogetherScreen(state, nav, it.arguments?.getString("id")) }
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

private const val RAIL = 84

/**
 * The TV frame: a navigation rail on the left of the top-level screens that opens up with
 * labels while it holds the focus; detail screens and the player use the whole screen.
 */
@OptIn(ExperimentalComposeUiApi::class)
@Composable
private fun TvMain(state: AppState, nav: NavHostController, tabs: List<Tab>, route: String, watching: Boolean, onSignedOut: () -> Unit, onSwitchProfile: () -> Unit) {
    val top = tabs.any { it.route == route }
    val selected = tabs.indexOfFirst { it.route == route }.coerceAtLeast(0)
    val requesters = remember { tabs.map { FocusRequester() } }
    var railFocused by remember { mutableStateOf(false) }
    Box(Modifier.fillMaxSize().background(Kino.bg)) {
        Column(Modifier.fillMaxSize().padding(start = if (top) RAIL.dp else 0.dp)) {
            Box(Modifier.weight(1f)) { Routes(state, nav, onSignedOut, onSwitchProfile) }
            if (!watching) MiniPlayer(state, nav)
        }
        // Back from the content of a top-level screen goes to the rail first; from the rail the
        // usual back applies (to Home, then out of the app). Composed after the NavHost so it
        // is asked before the NavHost's own back handling.
        BackHandler(enabled = top && !railFocused) { runCatching { requesters[selected].requestFocus() } }
        if (top) {
            val width by animateDpAsState(if (railFocused) 250.dp else RAIL.dp, tween(200), label = "rail")
            Column(Modifier.fillMaxHeight().width(width)
                .background(Brush.horizontalGradient(if (railFocused) listOf(Kino.bg, Kino.bg.copy(alpha = 0.94f), Kino.bg.copy(alpha = 0.0f)) else listOf(Kino.bg, Kino.bg)))
                .onFocusChanged { railFocused = it.hasFocus }
                .focusProperties { enter = { requesters[selected] } }
                .focusGroup()
                .padding(horizontal = 14.dp, vertical = 28.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Image(painterResource(R.mipmap.ic_launcher), null, Modifier.padding(start = 6.dp, bottom = 22.dp).size(44.dp).clip(RoundedCornerShape(10.dp)))
                tabs.forEachIndexed { i, t ->
                    RailItem(t, i == selected, railFocused, Modifier.focusRequester(requesters[i])) {
                        if (i != selected) nav.navigate(t.route) { popUpTo("home") { saveState = true }; launchSingleTop = true; restoreState = true }
                    }
                }
            }
        }
    }
}

@Composable
private fun RailItem(t: Tab, selected: Boolean, expanded: Boolean, modifier: Modifier, onClick: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    val fg = when { focused -> Kino.bg; selected -> Kino.text; else -> Kino.faint }
    Row(modifier.fillMaxWidth().height(52.dp).clip(RoundedCornerShape(26.dp)).background(if (focused) Kino.text else Color.Transparent)
        .clickable(interactionSource = src, indication = null, onClick = onClick).padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        Box(contentAlignment = Alignment.BottomCenter) {
            Icon(t.icon, stringResource(t.label), tint = fg, modifier = Modifier.size(26.dp))
            if (selected && !focused && !expanded) Box(Modifier.offset(y = 8.dp).size(5.dp).clip(RoundedCornerShape(3.dp)).background(LocalAccent.current))
        }
        if (expanded) Text(stringResource(t.label), color = fg, fontSize = 17.sp, fontWeight = if (selected) FontWeight.Bold else FontWeight.Medium, maxLines = 1)
    }
}
