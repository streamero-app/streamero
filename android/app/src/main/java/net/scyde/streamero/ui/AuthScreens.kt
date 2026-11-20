package net.scyde.streamero.ui

import android.os.Build
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import android.net.Uri
import androidx.browser.customtabs.CustomTabsIntent
import androidx.compose.ui.platform.LocalContext
import net.scyde.streamero.api.ApiException
import net.scyde.streamero.api.AuthProviders
import net.scyde.streamero.api.Pkce
import net.scyde.streamero.api.SsoBroker
import net.scyde.streamero.api.SsoResult
import net.scyde.streamero.api.ssoAuthorizeUrl
import net.scyde.streamero.api.DeviceInfo
import net.scyde.streamero.api.Profile
import net.scyde.streamero.api.cleanTotpCode
import net.scyde.streamero.api.isCleartextRemote
import net.scyde.streamero.api.normalizeServerUrl

@Composable
internal fun AuthFrame(title: String, subtitle: String?, content: @Composable ColumnScope.() -> Unit) {
    val tv = LocalTv.current
    Box(Modifier.fillMaxSize().background(Brush.radialGradient(listOf(Kino.accent.copy(alpha = 0.22f), Kino.bg), radius = 1400f))) {
        // On a TV the form keeps a readable width on the left instead of spanning the screen.
        Column((if (tv) Modifier.fillMaxHeight().widthIn(max = 720.dp) else Modifier.fillMaxSize()).verticalScroll(rememberScrollState()).systemBarsPadding().imePadding()
            .then(if (tv) Modifier.padding(horizontal = 64.dp, vertical = 8.dp) else Modifier.padding(24.dp)),
            verticalArrangement = Arrangement.spacedBy(16.dp)) {
            Spacer(Modifier.height(if (tv) 24.dp else 40.dp))
            AppMark(if (tv) 64.dp else 56.dp)
            Spacer(Modifier.height(20.dp))
            Text(title, style = Display.copy(fontSize = 36.sp), color = Kino.text)
            if (subtitle != null) Text(subtitle, color = Kino.muted, fontSize = 15.sp)
            content()
        }
    }
}

@Composable
fun ServerScreen(state: AppState, onInvite: () -> Unit = {}, onConnected: () -> Unit) {
    var url by rememberSaveable { mutableStateOf("https://") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    val scope = rememberCoroutineScope()
    // "https://" alone (the prefilled start) is not an address yet.
    val clean = normalizeServerUrl(url)?.takeIf { url.trim().trimEnd('/') !in listOf("https:", "http:") }
    val connect: () -> Unit = connect@{
        val target = clean
        if (busy || target == null) return@connect
        busy = true; error = null
        scope.launch {
            attempt {
                val api = state.session.setServer(target)
                api.info() // proves it is a Streamero server
                state.attach(api)
            }.onSuccess { onConnected() }.onFailure { error = it }
            busy = false
        }
    }
    AuthFrame(stringResource(R.string.server_title), stringResource(R.string.server_intro)) {
        OutlinedTextField(url, { url = it.trim() }, label = { Text(stringResource(R.string.server_address)) }, singleLine = true,
            isError = clean == null && url.length > 8,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go, autoCorrectEnabled = false), keyboardActions = KeyboardActions(onGo = { connect() }), modifier = Modifier.fillMaxWidth().initialFocus().dpadExit(), shape = CardShape)
        if (clean == null && url.length > 8) Text(stringResource(R.string.err_invalid_url), color = Kino.danger, fontSize = 14.sp)
        if (isCleartextRemote(clean)) Notice(Kino.danger) { Text(stringResource(R.string.cleartext_warning), color = Kino.text, fontSize = 14.sp) }
        ErrorLine(error)
        PrimaryButton(stringResource(R.string.connect), enabled = !busy && clean != null, modifier = Modifier.fillMaxWidth(), onClick = connect)
        // An invite link names its server, so it can be used before choosing one.
        if (!LocalTv.current) TextButton(onClick = onInvite) { Text(stringResource(R.string.invite_have), color = Kino.muted) }
    }
}

@Composable
fun LoginScreen(state: AppState, onChangeServer: () -> Unit, onInvite: () -> Unit = {}, onSignedIn: () -> Unit) {
    var user by rememberSaveable { mutableStateOf("") }
    var pass by remember { mutableStateOf("") }
    var totp by remember { mutableStateOf("") }
    var needTotp by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    // Sent back here because the server ended the session: say so.
    var error by remember { mutableStateOf<Throwable?>(if (state.sessionExpired) ApiException(401, "unauthenticated", "") else null) }
    var disclaimer by remember { mutableStateOf<String?>(null) }
    var serverName by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()
    val tv = LocalTv.current
    // Typing a password with a remote is painful: a TV signs in with Quick Connect first.
    var usePassword by rememberSaveable { mutableStateOf(!tv) }
    var providers by remember { mutableStateOf<AuthProviders?>(null) }
    val context = LocalContext.current
    LaunchedEffect(state.api) {
        val api = state.api ?: return@LaunchedEffect
        attempt { api.info() }.onSuccess { serverName = it.name; disclaimer = it.loginDisclaimer }
        providers = attempt { api.authProviders() }.getOrNull() // older servers: password only
    }
    val device = DeviceInfo(Build.MODEL ?: "Android", if (tv) TV_CLIENT else "Streamero Android")
    // The browser came back through streamero://oidc (MainActivity → SsoBroker).
    val ssoResult by SsoBroker.pending.collectAsState()
    LaunchedEffect(ssoResult, state.api) {
        val api = state.api ?: return@LaunchedEffect
        val (result, verifier) = SsoBroker.take(context) ?: return@LaunchedEffect
        error = null
        when (result) {
            is SsoResult.Failed -> error = ApiException(400, result.error, result.error)
            is SsoResult.Code -> {
                if (verifier == null) { error = ApiException(400, "invalid_state", "invalid_state"); return@LaunchedEffect }
                busy = true
                attempt {
                    val res = api.oidcExchange(result.code, verifier, device)
                    state.session.setToken(res.token)
                    state.refreshMe()
                }.onSuccess { onSignedIn() }.onFailure { error = it }
                busy = false
            }
        }
    }
    if (!usePassword) { TvQuickConnect(state, serverName, onUsePassword = { usePassword = true }, onChangeServer = onChangeServer, onSignedIn = onSignedIn); return }
    AuthFrame(stringResource(R.string.sign_in), serverName.ifBlank { null }) {
        providers?.oidc?.forEachIndexed { i, p ->
            GlassButton(stringResource(R.string.sso_continue, p.label), {
                error = null
                runCatching {
                    val verifier = SsoBroker.begin(context)
                    CustomTabsIntent.Builder().build().launchUrl(context, Uri.parse(ssoAuthorizeUrl(p.authorizeUrl, Pkce.challenge(verifier))))
                }.onFailure { error = ApiException(0, "no_browser", context.getString(R.string.sso_no_browser)) }
            }, if (i == 0 && providers?.passwordLogin == false) Modifier.fillMaxWidth().initialFocus() else Modifier.fillMaxWidth())
        }
        if (providers?.passwordLogin == false) {
            ErrorLine(error)
        } else {
        OutlinedTextField(user, { user = it }, label = { Text(stringResource(R.string.username)) }, singleLine = true,
            keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, capitalization = KeyboardCapitalization.None), modifier = Modifier.fillMaxWidth().initialFocus().dpadExit(), shape = CardShape)
        OutlinedTextField(pass, { pass = it }, label = { Text(stringResource(R.string.password)) }, singleLine = true, visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = PasswordKeyboard, modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
        // A recovery code has letters and dashes, so the field is not numbers only.
        if (needTotp) OutlinedTextField(totp, { totp = cleanTotpCode(it) }, label = { Text(stringResource(R.string.totp_code)) }, singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false), modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
        if (isCleartextRemote(state.api?.base?.toString())) Notice(Kino.danger) { Text(stringResource(R.string.cleartext_warning), color = Kino.text, fontSize = 14.sp) }
        ErrorLine(error)
        PrimaryButton(stringResource(R.string.sign_in), enabled = !busy && user.isNotBlank() && pass.isNotBlank(), modifier = Modifier.fillMaxWidth(), onClick = {
            if (busy) return@PrimaryButton
            busy = true; error = null
            scope.launch {
                attempt {
                    val api = state.api ?: throw ApiException(0, "invalid_url", "no server")
                    val res = api.login(user.trim(), pass, totp, device)
                    state.session.setToken(res.token)
                    state.refreshMe()
                }.onSuccess { onSignedIn() }.onFailure {
                    if ((it as? ApiException)?.code == "totp_required") needTotp = true else error = it
                }
                busy = false
            }
        })
        }
        if (tv) GlassButton(stringResource(R.string.qc_use), { usePassword = false }, Modifier.fillMaxWidth())
        val changeServer: () -> Unit = { scope.launch { attempt { state.session.forgetServer() }; state.attach(null); onChangeServer() } }
        if (!tv) TextButton(onClick = onInvite) { Text(stringResource(R.string.invite_have), color = Kino.muted) }
        if (tv) GlassButton(stringResource(R.string.change_server), changeServer, Modifier.fillMaxWidth())
        else TextButton(onClick = changeServer) { Text(stringResource(R.string.change_server), color = Kino.muted) }
        disclaimer?.let { Text(it, color = Kino.muted, fontSize = 13.sp) }
    }
}

@Composable
fun ProfileGate(state: AppState, onDone: () -> Unit) {
    val api = state.api ?: return
    val me = state.me ?: return
    var profiles by remember { mutableStateOf<List<Profile>>(emptyList()) }
    var target by remember { mutableStateOf<Profile?>(null) }
    var mainTarget by remember { mutableStateOf(false) }
    var secret by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<Throwable?>(null) }
    val scope = rememberCoroutineScope()
    var busy by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { attempt { api.profiles() }.onSuccess { profiles = it }.onFailure { error = it } }
    suspend fun select(p: Profile?) {
        error = null
        attempt {
            if (p == null) {
                if (me.currentProfile != null) { if (!mainTarget) { target = null; secret = ""; mainTarget = true; return }; api.selectMain(secret) }
            } else if (p.hasPin && target?.id != p.id) { target = p; mainTarget = false; secret = ""; return } else api.selectProfile(p.id, secret.ifBlank { null })
            state.refreshMe()
        }.onSuccess { onDone() }.onFailure { error = it }
    }
    // One choice at a time: a double tap does not select twice.
    fun choose(p: Profile?) {
        if (busy) return
        busy = true
        scope.launch { try { select(p) } finally { busy = false } }
    }
    AuthFrame(stringResource(R.string.who_is_watching), null) {
        val all: List<Profile?> = listOf(null) + profiles
        all.chunked(2).forEach { row ->
            Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                row.forEach { p ->
                    Column(Modifier.weight(1f).initialFocus(p == null).focusClickable(scale = 1.05f) { choose(p) }.padding(8.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Avatar(p?.name ?: me.displayName ?: me.username, p?.color ?: "#8A5A3A",
                            if (p?.hasAvatar == true) api.avatarUrl(p.id) else if (p == null && me.hasAvatar) api.avatarUrl(me.id) else null, 120.dp)
                        Text(p?.name ?: me.displayName ?: me.username, color = Kino.text)
                    }
                }
                if (row.size == 1) Spacer(Modifier.weight(1f))
            }
        }
        if (target != null || mainTarget) {
            OutlinedTextField(secret, { v -> secret = if (mainTarget) v else v.filter { it in '0'..'9' }.take(6) }, label = { Text(stringResource(if (mainTarget) R.string.password else R.string.pin)) }, singleLine = true,
                visualTransformation = PasswordVisualTransformation(), keyboardOptions = if (mainTarget) PasswordKeyboard else KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
                modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
            PrimaryButton(stringResource(R.string.open), enabled = !busy, onClick = { choose(if (mainTarget) null else target) }, modifier = Modifier.fillMaxWidth())
        }
        ErrorLine(error)
    }
}

/** Password fields: the password keyboard, no suggestions or autocorrect learning what is typed. */
internal val PasswordKeyboard = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false)
