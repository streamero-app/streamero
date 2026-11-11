package net.scyde.streamero.ui

import android.content.Intent
import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.Share
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.SyncGroup
import net.scyde.streamero.api.SyncMember
import net.scyde.streamero.sync.parseGroupId

/** Watch together needs the syncPlay permission (administrators always have it). */
fun canWatchTogether(state: AppState): Boolean = state.me?.let { it.isAdmin || it.permissions.syncPlay } == true

/** Opens the player on the group's current entry. */
fun NavHostController.watchGroup(g: SyncGroup): Boolean {
    val entry = g.current ?: return false
    watch(entry.itemId)
    return true
}

/** Groups to join, joining by an invite link or code, and the group this device is in. */
@Composable
fun TogetherScreen(state: AppState, nav: NavHostController, linkId: String?) {
    val api = state.api ?: return
    val sync = state.sync
    val scope = rememberCoroutineScope()
    var link by remember { mutableStateOf(linkId.orEmpty()) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var invalid by remember { mutableStateOf(false) }
    var nothing by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var reload by remember { mutableIntStateOf(0) }
    val (groups, loading, loadError) = rememberLoad(reload) { api.syncGroups() }
    LaunchedEffect(Unit) { if (sync.group == null) runCatching { sync.restore() } }

    fun join(id: String) {
        busy = true; error = null; nothing = false
        scope.launch {
            runCatching { sync.join(id) }
                .onSuccess { g -> if (!nav.watchGroup(g)) nothing = true }
                .onFailure { error = it }
            busy = false
        }
    }

    LazyColumn(Modifier.fillMaxSize().statusBarsPadding(), contentPadding = PaddingValues(Dims.edge, 8.dp, Dims.edge, 30.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
        item {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (!LocalTv.current) IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Kino.text) }
                Text(stringResource(R.string.watch_together), style = Display.copy(fontSize = 30.sp), color = Kino.text)
            }
        }
        item { Text(stringResource(R.string.together_intro), color = Kino.muted, fontSize = 15.sp, lineHeight = 22.sp) }
        sync.group?.let { g ->
            item {
                Card(stringResource(R.string.together_current)) {
                    GroupLine(g)
                    Row(Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        if (g.current != null) PrimaryButton(stringResource(R.string.together_return), { nav.watchGroup(g) }, Modifier.initialFocus())
                        GlassButton(stringResource(R.string.together_leave), { scope.launch { sync.leave(); reload++ } })
                    }
                }
            }
        }
        item {
            Card(null) {
                OutlinedTextField(link, { link = it; invalid = false }, Modifier.fillMaxWidth(), singleLine = true, shape = CardShape,
                    label = { Text(stringResource(R.string.together_link_hint)) })
                PrimaryButton(stringResource(R.string.together_join), {
                    val id = parseGroupId(link)
                    if (id == null) invalid = true else join(id)
                }, Modifier.fillMaxWidth().padding(top = 8.dp), enabled = !busy && link.isNotBlank())
                if (invalid) Text(stringResource(R.string.together_invalid_link), color = Kino.danger, fontSize = 14.sp)
                if (nothing) Text(stringResource(R.string.together_nothing_queued), color = Kino.muted, fontSize = 14.sp)
                ErrorLine(error)
            }
        }
        item { SectionTitle(stringResource(R.string.together_open_groups), Modifier.padding(top = 6.dp)) }
        when {
            loading && groups == null -> item { Loading() }
            groups == null -> item { ErrorLine(loadError) }
            groups.none { it.id != sync.group?.id } -> item { Text(stringResource(R.string.together_none), color = Kino.faint, fontSize = 15.sp) }
            else -> items(groups.filter { it.id != sync.group?.id }, key = { it.id }) { g ->
                Row(Modifier.fillMaxWidth().clip(CardShape).background(Kino.surface).focusClickable(scale = 1.02f) { if (!busy) join(g.id) }.padding(16.dp),
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    Icon(Icons.Default.Groups, null, tint = Kino.muted)
                    Box(Modifier.weight(1f)) { GroupLine(g) }
                    Text(stringResource(R.string.together_join), color = Kino.text, fontWeight = FontWeight.SemiBold)
                }
            }
        }
    }
    // A link opened from outside (route together/{id}) joins right away.
    LaunchedEffect(linkId) { linkId?.let(::parseGroupId)?.takeIf { it != sync.group?.id }?.let(::join) }
}

@Composable
private fun GroupLine(g: SyncGroup) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(g.name, color = Kino.text, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(listOfNotNull(g.current?.title, stringResource(R.string.together_watching, g.members.count { it.connected })).joinToString("  ·  "),
            color = Kino.muted, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** After starting a group: the invite link, the share sheet and the way into the player. */
@Composable
fun InviteDialog(link: String, title: String, onDismiss: () -> Unit, onStart: () -> Unit) {
    val ctx = LocalContext.current
    val message = stringResource(R.string.together_share_message, title, link)
    val chooser = stringResource(R.string.together_share)
    AlertDialog(onDismissRequest = onDismiss, containerColor = Kino.surface,
        title = { Text(stringResource(R.string.together_invite_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(stringResource(R.string.together_invite_text), color = Kino.muted)
                SelectionContainer { Text(link, color = Kino.text, fontSize = 14.sp, modifier = Modifier.background(Kino.surface3, CardShape).padding(12.dp)) }
                if (!LocalTv.current) GlassButton(chooser, {
                    val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, message)
                    ctx.startActivity(Intent.createChooser(send, chooser))
                }, Modifier.fillMaxWidth(), icon = Icons.Default.Share)
            }
        },
        confirmButton = { TextButton(onClick = onStart) { Text(stringResource(R.string.together_start), color = Kino.text, fontWeight = FontWeight.SemiBold) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.close), color = Kino.muted) } })
}

/** Whom the group waits for, or null when nobody holds it up. */
@Composable
private fun waitingLine(g: SyncGroup): String? {
    if (g.state != "waiting") return null
    val names = g.members.filter { it.connected && !it.ready && !it.lagging && it.id != g.selfId }.map { it.name }
    return if (names.isEmpty()) stringResource(R.string.together_waiting) else stringResource(R.string.together_waiting_for, names.joinToString(", "))
}

/**
 * The group inside the player: a compact pill with the member count and whom it waits for;
 * on a phone it opens to the member list and the leave button.
 */
@Composable
fun SyncOverlay(g: SyncGroup, connected: Boolean, onLeave: (() -> Unit)?, modifier: Modifier = Modifier) {
    var open by remember { mutableStateOf(false) }
    val waiting = waitingLine(g)
    Column(modifier.widthIn(max = 320.dp).clip(CardShape).background(Color.Black.copy(alpha = 0.72f))
        .then(if (onLeave != null) Modifier.clickable { open = !open } else Modifier)
        .animateContentSize().padding(horizontal = 14.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(Icons.Default.Groups, null, tint = Kino.text, modifier = Modifier.size(18.dp))
            Text(stringResource(R.string.together_watching, g.members.count { it.connected }), color = Kino.text, fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
        }
        when {
            !connected -> Text(stringResource(R.string.together_reconnecting), color = Kino.muted, fontSize = 13.sp)
            waiting != null -> Text(waiting, color = Kino.muted, fontSize = 13.sp, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        if (open && onLeave != null) {
            g.members.forEach { m -> MemberRow(m, m.id == g.selfId) }
            TextButton(onClick = onLeave, contentPadding = PaddingValues(0.dp)) { Text(stringResource(R.string.together_leave), color = Kino.danger, fontWeight = FontWeight.SemiBold) }
        }
    }
}

@Composable
private fun MemberRow(m: SyncMember, self: Boolean) {
    val status = when {
        !m.connected -> stringResource(R.string.together_member_offline)
        m.buffering -> stringResource(R.string.together_member_buffering)
        m.lagging -> stringResource(R.string.together_member_lagging)
        else -> null
    }
    val dot = when {
        !m.connected -> Kino.faint
        m.ready -> Kino.success
        else -> Kino.accent
    }
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Box(Modifier.size(8.dp).clip(CircleShape).background(dot))
        Text(m.name + if (self) " (" + stringResource(R.string.together_you) + ")" else "", color = Kino.text, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
        status?.let { Text(it, color = Kino.faint, fontSize = 13.sp) }
    }
}
