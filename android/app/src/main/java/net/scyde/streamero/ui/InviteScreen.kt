package net.scyde.streamero.ui

import android.os.Build
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
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
        if (uri?.scheme.equals("streamero", true) && uri?.host.equals("invite", true)) parseInviteDeepLink(uri.toString())?.let { pending.value = it }
    }
}

/**
 * Accept an invite (web: /invite/<token>): paste the link the administrator shared (or just the
 * token, for the server this app is connected to), then choose a username and password.
 */
@Composable
fun InviteScreen(state: AppState, initial: InviteLink?, onBack: () -> Unit, onSignedIn: () -> Unit) {
    var text by remember { mutableStateOf("") }
    var link by remember { mutableStateOf<InviteLink?>(null) }
    var info by remember { mutableStateOf<InviteInfo?>(null) }
    var invalid by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var username by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var confirm by remember { mutableStateOf("") }
    var showProblems by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val tv = LocalTv.current
    val device = DeviceInfo(Build.MODEL ?: "Android", if (tv) TV_CLIENT else "Streamero Android")

    fun open(l: InviteLink) {
        busy = true; error = null; invalid = false
        scope.launch {
            runCatching {
                // A link names its server: connect to it first (replacing a different one).
                val current = state.api?.base?.toString()?.trimEnd('/')
                val api = if (l.server != null && l.server != current) state.session.setServer(l.server).also { state.attach(it) } else state.api!!
                api.inviteInfo(l.token)
            }.onSuccess { info = it; link = l }.onFailure { e -> if (e is ApiException && e.status in 400..499) invalid = true else error = e }
            busy = false
        }
    }
    LaunchedEffect(initial) { if (initial != null) { text = initial.token; open(initial) } }

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
            ErrorLine(error)
            PrimaryButton(stringResource(R.string.continue_), enabled = !busy && usable, modifier = Modifier.fillMaxWidth(), onClick = { parsed?.let { open(it) } })
            TextButton(onClick = onBack) { Text(stringResource(R.string.back), color = Kino.muted) }
        }
        return
    }
    AuthFrame(stringResource(R.string.invite_join_title, i.serverName), stringResource(R.string.invite_join_intro)) {
        val problem = signUpProblem(username, password, confirm)
        OutlinedTextField(username, { username = it.take(64) }, label = { Text(stringResource(R.string.username)) }, singleLine = true,
            modifier = Modifier.fillMaxWidth().initialFocus().dpadExit(), shape = CardShape)
        OutlinedTextField(password, { password = it }, label = { Text(stringResource(R.string.password)) }, singleLine = true,
            supportingText = { Text(stringResource(R.string.password_hint)) }, visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password), modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
        OutlinedTextField(confirm, { confirm = it }, label = { Text(stringResource(R.string.confirm_password)) }, singleLine = true,
            visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            isError = showProblems && problem == SignUpProblem.Mismatch, modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
        if (showProblems && problem != null) Text(stringResource(when (problem) {
            SignUpProblem.Username -> R.string.err_invalid_username
            SignUpProblem.PasswordShort -> R.string.password_hint
            SignUpProblem.Mismatch -> R.string.err_password_mismatch
        }), color = Kino.danger, fontSize = 14.sp)
        ErrorLine(error)
        PrimaryButton(stringResource(R.string.invite_join), enabled = !busy, modifier = Modifier.fillMaxWidth(), onClick = {
            showProblems = true
            if (problem != null) return@PrimaryButton
            busy = true; error = null
            scope.launch {
                runCatching {
                    val res = state.api!!.acceptInvite(l.token, AcceptInvite(username.trim(), password, device))
                    state.session.setToken(res.token)
                    state.refreshMe()
                }.onSuccess { onSignedIn() }.onFailure { error = it }
                busy = false
            }
        })
        TextButton(onClick = onBack) { Text(stringResource(R.string.back), color = Kino.muted) }
    }
}
