package net.scyde.streamero.ui

import android.os.Build
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Password
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.DeviceInfo
import net.scyde.streamero.api.QuickConnectState
import net.scyde.streamero.api.quickConnect
import net.scyde.streamero.api.quickConnectFlow

/** The TV client name the server shows in its device list. */
val TV_CLIENT = "Streamero Android TV"

/**
 * Sign-in on a TV without typing: the TV shows a Quick Connect code that a signed-in phone
 * or browser approves (Settings → Quick Connect). Password sign-in stays one click away.
 */
@Composable
fun TvQuickConnect(state: AppState, serverName: String, onUsePassword: () -> Unit, onChangeServer: () -> Unit, onSignedIn: () -> Unit) {
    val api = state.api ?: return
    var attempt by remember { mutableIntStateOf(0) }
    var current by remember { mutableStateOf<QuickConnectState?>(null) }
    var signInError by remember { mutableStateOf<Throwable?>(null) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(attempt) {
        current = null
        quickConnectFlow(api.quickConnect(DeviceInfo(Build.MODEL ?: "Android TV", TV_CLIENT))).collect { s ->
            current = s
            if (s is QuickConnectState.Approved) {
                runCatching {
                    state.session.setToken(s.auth.token)
                    state.refreshMe() ?: error("no user")
                }.onSuccess { onSignedIn() }.onFailure { signInError = it }
            }
        }
    }
    Box(Modifier.fillMaxSize().background(Brush.radialGradient(listOf(Kino.accent.copy(alpha = 0.22f), Kino.bg), radius = 1600f))) {
        Row(Modifier.fillMaxSize().padding(horizontal = 64.dp, vertical = 40.dp), horizontalArrangement = Arrangement.spacedBy(56.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                Text("STREAMERO", style = Head.copy(fontSize = 18.sp), color = Kino.text)
                Text(stringResource(R.string.sign_in), style = Display.copy(fontSize = 44.sp), color = Kino.text, modifier = Modifier.padding(top = 12.dp))
                if (serverName.isNotBlank()) Text(serverName, color = Kino.muted, fontSize = 16.sp)
                Text(stringResource(R.string.qc_tv_intro), color = Kino.text.copy(alpha = 0.85f), fontSize = 17.sp, lineHeight = 25.sp, modifier = Modifier.padding(top = 10.dp))
                Column(Modifier.padding(top = 18.dp).width(IntrinsicSize.Max), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    val expired = current is QuickConnectState.Expired || current is QuickConnectState.Failed
                    if (expired) PrimaryButton(stringResource(R.string.qc_new_code), { attempt++ }, Modifier.fillMaxWidth().initialFocus(), icon = Icons.Outlined.Refresh)
                    GlassButton(stringResource(R.string.qc_use_password), onUsePassword, Modifier.fillMaxWidth().initialFocus(!expired), icon = Icons.Outlined.Password)
                    GlassButton(stringResource(R.string.change_server), { scope.launch { state.session.forgetServer(); state.attach(null); onChangeServer() } }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Dns)
                }
            }
            Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(18.dp)) {
                Text(stringResource(R.string.qc_code).uppercase(), style = Label.copy(fontSize = 14.sp), color = Kino.faint)
                when (val s = current) {
                    is QuickConnectState.Code -> {
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            s.code.forEachIndexed { i, c ->
                                if (i == 3) Spacer(Modifier.width(12.dp))
                                Box(Modifier.size(width = 50.dp, height = 72.dp).background(Kino.surface, RoundedCornerShape(8.dp)).border(1.dp, Kino.border, RoundedCornerShape(8.dp)),
                                    contentAlignment = Alignment.Center) {
                                    Text(c.toString(), style = Display.copy(fontSize = 40.sp), color = Kino.text)
                                }
                            }
                        }
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                            CircularProgressIndicator(Modifier.size(16.dp), color = LocalAccent.current, strokeWidth = 2.dp)
                            Text(stringResource(R.string.qc_waiting), color = Kino.muted, fontSize = 15.sp)
                        }
                    }
                    is QuickConnectState.Approved -> {
                        Text(stringResource(R.string.qc_approved), color = Kino.success, fontSize = 20.sp, fontWeight = FontWeight.SemiBold)
                        ErrorLine(signInError)
                    }
                    is QuickConnectState.Expired -> Text(stringResource(R.string.qc_expired), color = Kino.muted, fontSize = 16.sp)
                    is QuickConnectState.Failed -> ErrorLine(s.error)
                    null -> Loading()
                }
            }
        }
    }
}
