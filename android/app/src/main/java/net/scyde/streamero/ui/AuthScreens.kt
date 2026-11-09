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
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.ApiException
import net.scyde.streamero.api.DeviceInfo
import net.scyde.streamero.api.Profile

@Composable
private fun AuthFrame(title: String, subtitle: String?, content: @Composable ColumnScope.() -> Unit) {
    Box(Modifier.fillMaxSize().background(Brush.radialGradient(listOf(Kino.accent.copy(alpha = 0.22f), Kino.bg), radius = 1400f))) {
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).systemBarsPadding().imePadding().padding(24.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp)) {
            Spacer(Modifier.height(40.dp))
            Text("STREAMERO", style = Head.copy(fontSize = 18.sp), color = Kino.text)
            Spacer(Modifier.height(20.dp))
            Text(title.uppercase(), style = Display.copy(fontSize = 36.sp), color = Kino.text)
            if (subtitle != null) Text(subtitle, color = Kino.muted, fontSize = 15.sp)
            content()
        }
    }
}

@Composable
fun ServerScreen(state: AppState, onConnected: () -> Unit) {
    var url by remember { mutableStateOf("https://") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    val scope = rememberCoroutineScope()
    AuthFrame(stringResource(R.string.server_title), stringResource(R.string.server_intro)) {
        OutlinedTextField(url, { url = it.trim() }, label = { Text(stringResource(R.string.server_address)) }, singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri), modifier = Modifier.fillMaxWidth(), shape = CardShape)
        ErrorLine(error)
        PrimaryButton(stringResource(R.string.connect), enabled = !busy && url.length > 8, modifier = Modifier.fillMaxWidth(), onClick = {
            busy = true; error = null
            scope.launch {
                val clean = url.trimEnd('/').let { if (it.startsWith("http")) it else "https://$it" }
                runCatching {
                    val api = state.session.setServer(clean)
                    api.info() // proves it is a Streamero server
                    state.attach(api)
                }.onSuccess { onConnected() }.onFailure { error = it }
                busy = false
            }
        })
    }
}

@Composable
fun LoginScreen(state: AppState, onChangeServer: () -> Unit, onSignedIn: () -> Unit) {
    var user by remember { mutableStateOf("") }
    var pass by remember { mutableStateOf("") }
    var totp by remember { mutableStateOf("") }
    var needTotp by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var disclaimer by remember { mutableStateOf<String?>(null) }
    var serverName by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()
    LaunchedEffect(state.api) {
        runCatching { state.api!!.info() }.onSuccess { serverName = it.name; disclaimer = it.loginDisclaimer }
    }
    AuthFrame(stringResource(R.string.sign_in), serverName.ifBlank { null }) {
        OutlinedTextField(user, { user = it }, label = { Text(stringResource(R.string.username)) }, singleLine = true, modifier = Modifier.fillMaxWidth(), shape = CardShape)
        OutlinedTextField(pass, { pass = it }, label = { Text(stringResource(R.string.password)) }, singleLine = true, visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password), modifier = Modifier.fillMaxWidth(), shape = CardShape)
        if (needTotp) OutlinedTextField(totp, { totp = it }, label = { Text(stringResource(R.string.totp_code)) }, singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword), modifier = Modifier.fillMaxWidth(), shape = CardShape)
        ErrorLine(error)
        PrimaryButton(stringResource(R.string.sign_in), enabled = !busy && user.isNotBlank() && pass.isNotBlank(), modifier = Modifier.fillMaxWidth(), onClick = {
            busy = true; error = null
            scope.launch {
                runCatching {
                    val res = state.api!!.login(user.trim(), pass, totp, DeviceInfo(Build.MODEL ?: "Android", "Streamero Android"))
                    state.session.setToken(res.token)
                    state.refreshMe()
                }.onSuccess { onSignedIn() }.onFailure {
                    if ((it as? ApiException)?.code == "totp_required") needTotp = true else error = it
                }
                busy = false
            }
        })
        TextButton(onClick = { scope.launch { state.session.forgetServer(); state.attach(null); onChangeServer() } }) {
            Text(stringResource(R.string.change_server), color = Kino.muted)
        }
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
    LaunchedEffect(Unit) { profiles = runCatching { api.profiles() }.getOrDefault(emptyList()) }
    fun choose(p: Profile?) = scope.launch {
        error = null
        runCatching {
            if (p == null) {
                if (me.currentProfile != null) { if (!mainTarget) { mainTarget = true; return@launch }; api.selectMain(secret) }
            } else if (p.hasPin && target?.id != p.id) { target = p; return@launch } else api.selectProfile(p.id, secret.ifBlank { null })
            state.refreshMe()
        }.onSuccess { onDone() }.onFailure { error = it }
    }
    AuthFrame(stringResource(R.string.who_is_watching), null) {
        val all: List<Profile?> = listOf(null) + profiles
        all.chunked(2).forEach { row ->
            Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                row.forEach { p ->
                    Column(Modifier.weight(1f).clickable { choose(p) }, horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Avatar(p?.name ?: me.displayName ?: me.username, p?.color ?: "#8A5A3A",
                            if (p?.hasAvatar == true) api.avatarUrl(p.id) else if (p == null && me.hasAvatar) api.avatarUrl(me.id) else null, 120.dp)
                        Text(p?.name ?: me.displayName ?: me.username, color = Kino.text)
                    }
                }
                if (row.size == 1) Spacer(Modifier.weight(1f))
            }
        }
        if (target != null || mainTarget) {
            OutlinedTextField(secret, { secret = it }, label = { Text(stringResource(if (mainTarget) R.string.password else R.string.pin)) }, singleLine = true,
                visualTransformation = PasswordVisualTransformation(), modifier = Modifier.fillMaxWidth(), shape = CardShape)
            PrimaryButton(stringResource(R.string.open), onClick = { choose(target) }, modifier = Modifier.fillMaxWidth())
        }
        ErrorLine(error)
    }
}

@Suppress("unused") private val unusedShape = RoundedCornerShape(0.dp)
