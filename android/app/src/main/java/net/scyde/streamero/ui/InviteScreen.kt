package net.scyde.streamero.ui

import android.os.Build
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*

/** A `streamero://invite?server=…&token=…` link the app was opened with, until the invite screen takes it. */
object InviteBroker {
    val pending = MutableStateFlow<InviteLink?>(null)
    fun deliver(uri: android.net.Uri?) {
        if (isInviteScheme(uri?.scheme) && uri?.host.equals("invite", true)) parseInviteDeepLink(uri.toString())?.let { pending.value = it }
    }
}

/**
 * Accept an invite (web: /invite/<token>): paste the link the administrator shared (or just the
 * token, for the server this app is connected to), then choose a username and password.
 */
@Composable
fun InviteScreen(state: AppState, initial: InviteLink?, onBack: () -> Unit, onSignedIn: () -> Unit) {
    var text by rememberSaveable { mutableStateOf("") }
    var link by remember { mutableStateOf<InviteLink?>(null) }
    var info by remember { mutableStateOf<InviteInfo?>(null) }
    var invalid by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var username by rememberSaveable { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var confirm by remember { mutableStateOf("") }
    var showProblems by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val tv = LocalTv.current
    val device = DeviceInfo(Build.MODEL ?: "Android", if (tv) TV_CLIENT else "Streamero Android")

    fun open(l: InviteLink) {
        if (busy) return
        busy = true; error = null; invalid = false
        scope.launch {
            attempt {
                // A link names its server: connect to it first (replacing a different one). Only
                // after the user saw that server in the field and chose to continue.
                val current = state.api?.base?.toString()?.trimEnd('/')
                val api = if (l.server != null && l.server != current) state.session.setServer(l.server).also { state.attach(it) }
                    else state.api ?: throw ApiException(0, "invalid_url", "no server")
                api.inviteInfo(l.token)
            }.onSuccess { info = it; link = l }.onFailure { e -> if (e is ApiException && e.status in 400..499) invalid = true else error = e }
            busy = false
        }
    }
    // A link that opened the app only fills the field: nothing is sent until the user has seen
    // which server it is for and taps Continue.
    LaunchedEffect(initial) { if (initial != null && text.isEmpty()) text = inviteText(initial) }

    val i = info
    val l = link
    if (i == null || l == null) {
        AuthFrame(stringResource(if (invalid) R.string.invite_invalid_title else R.string.invite_have), stringResource(if (invalid) R.string.invite_invalid_text else R.string.invite_paste_intro)) {
            val parsed = parseInviteLink(text)
            // Only a full link says which server; a bare token needs the one already chosen.
            val usable = parsed != null && (parsed.server != null || state.api != null)
            OutlinedTextField(text, { text = it.trim(); invalid = false }, label = { Text(stringResource(R.string.invite_link)) }, singleLine = true,
                placeholder = { Text("https://media.example.com/invite/…", color = Kino.faint) },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri), isError = text.isNotEmpty() && !usable,
                modifier = Modifier.fillMaxWidth().initialFocus().dpadExit(), shape = CardShape)
            if (text.isNotEmpty() && !usable) Text(stringResource(if (parsed != null) R.string.invite_need_link else R.string.invite_not_a_link), color = Kino.danger, fontSize = 14.sp)
            val server = parsed?.server ?: state.api?.base?.toString()?.trimEnd('/')
            if (usable && server != null) {
                Text(stringResource(R.string.invite_server_is, server.toHttpUrlOrNull()?.host ?: server), color = Kino.text, fontSize = 15.sp)
                if (isCleartextRemote(server)) Notice(Kino.danger) { Text(stringResource(R.string.cleartext_warning), color = Kino.text, fontSize = 14.sp) }
            }
            ErrorLine(error)
            PrimaryButton(stringResource(R.string.continue_), enabled = !busy && usable, modifier = Modifier.fillMaxWidth(), onClick = { parsed?.let { open(it) } })
            TextButton(onClick = onBack) { Text(stringResource(R.string.back), color = Kino.muted) }
        }
        return
    }
    AuthFrame(stringResource(R.string.invite_join_title, i.serverName), stringResource(R.string.invite_join_intro)) {
        val server = l.server ?: state.api?.base?.toString()?.trimEnd('/')
        if (server != null) Text(stringResource(R.string.invite_server_is, server.toHttpUrlOrNull()?.host ?: server), color = Kino.muted, fontSize = 14.sp)
        val problem = signUpProblem(username, password, confirm)
        OutlinedTextField(username, { username = it.take(64) }, label = { Text(stringResource(R.string.username)) }, singleLine = true,
            keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, capitalization = KeyboardCapitalization.None),
            modifier = Modifier.fillMaxWidth().initialFocus().dpadExit(), shape = CardShape)
        OutlinedTextField(password, { password = it }, label = { Text(stringResource(R.string.password)) }, singleLine = true,
            supportingText = { Text(stringResource(R.string.password_hint)) }, visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = PasswordKeyboard, modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
        OutlinedTextField(confirm, { confirm = it }, label = { Text(stringResource(R.string.confirm_password)) }, singleLine = true,
            visualTransformation = PasswordVisualTransformation(), keyboardOptions = PasswordKeyboard,
            isError = showProblems && problem == SignUpProblem.Mismatch, modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
        if (showProblems && problem != null) Text(stringResource(when (problem) {
            SignUpProblem.Username -> R.string.err_invalid_username
            SignUpProblem.PasswordShort -> R.string.password_hint
            SignUpProblem.Mismatch -> R.string.err_password_mismatch
        }), color = Kino.danger, fontSize = 14.sp)
        ErrorLine(error)
        PrimaryButton(stringResource(R.string.invite_join), enabled = !busy, modifier = Modifier.fillMaxWidth(), onClick = {
            showProblems = true
            if (problem != null || busy) return@PrimaryButton
            busy = true; error = null
            scope.launch {
                attempt {
                    val api = state.api ?: throw ApiException(0, "invalid_url", "no server")
                    val res = api.acceptInvite(l.token, AcceptInvite(username.trim(), password, device))
                    state.session.setToken(res.token)
                    state.refreshMe()
                }.onSuccess { onSignedIn() }.onFailure { error = it }
                busy = false
            }
        })
        TextButton(onClick = onBack) { Text(stringResource(R.string.back), color = Kino.muted) }
    }
}
