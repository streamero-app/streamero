package net.scyde.streamero.ui

import android.content.Intent
import android.graphics.BitmapFactory
import android.net.Uri
import android.util.Base64
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.FilterQuality
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*

/**
 * Turn TOTP two-factor sign-in on (QR code, authenticator link or the key by hand, then a code)
 * or off (with the password). The QR code is the PNG the server renders from the otpauth link,
 * so the app needs no QR encoder. Recovery codes are shown once, right after turning it on.
 */
@Composable
fun TwoFactorCard(state: AppState) {
    val api = state.api ?: return
    val me = state.me ?: return
    val scope = rememberCoroutineScope()
    val ctx = LocalContext.current
    var setup by remember { mutableStateOf<TotpSetup?>(null) }
    var code by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var recovery by remember { mutableStateOf<List<String>?>(null) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    fun act(block: suspend () -> Unit) {
        busy = true; error = null
        scope.launch { runCatching { block() }.onFailure { error = it }; busy = false }
    }
    Card(stringResource(R.string.twofa_title)) {
        Pill(stringResource(if (me.totpEnabled) R.string.twofa_on else R.string.twofa_off), if (me.totpEnabled) Kino.success else Kino.muted)
        recovery?.let { codes ->
            Notice(Kino.success) {
                Text(stringResource(R.string.twofa_recovery_title), color = Kino.text, fontSize = 15.sp)
                Text(stringResource(R.string.twofa_recovery_text), color = Kino.muted, fontSize = 13.sp)
                codes.chunked(2).forEach { row ->
                    Row { row.forEach { Text(it, color = Kino.text, fontFamily = FontFamily.Monospace, fontSize = 15.sp, modifier = Modifier.weight(1f)) } }
                }
                Actions {
                    GlassButton(stringResource(R.string.copy), { copyText(ctx, codes.joinToString("\n")) })
                    GlassButton(stringResource(R.string.done), { recovery = null })
                }
            }
        }
        val s = setup
        when {
            me.totpEnabled -> {
                Intro(stringResource(R.string.twofa_enabled_text))
                Field(stringResource(R.string.password), password, { password = it }, secret = true)
                PrimaryButton(stringResource(R.string.twofa_disable), enabled = !busy && password.isNotEmpty(), onClick = {
                    act { api.disableTotp(password); password = ""; state.refreshMe() }
                })
            }
            s == null -> {
                Intro(stringResource(R.string.twofa_intro))
                PrimaryButton(stringResource(R.string.twofa_enable), enabled = !busy, onClick = { act { setup = api.totpSetup(); code = "" } })
            }
            else -> {
                Text(stringResource(R.string.twofa_step1), color = Kino.text, fontSize = 14.sp)
                val qr = remember(s.qrPng) {
                    runCatching { Base64.decode(s.qrPng, Base64.DEFAULT).let { BitmapFactory.decodeByteArray(it, 0, it.size) }?.asImageBitmap() }.getOrNull()
                }
                // White quiet zone so every scanner reads it on the dark theme.
                if (qr != null) Box(Modifier.align(Alignment.CenterHorizontally).background(Color.White, RoundedCornerShape(8.dp)).padding(10.dp)) {
                    Image(qr, stringResource(R.string.twofa_qr_alt), Modifier.size(200.dp), filterQuality = FilterQuality.None)
                }
                val otp = remember(s.otpauthUri) { parseOtpAuth(s.otpauthUri) }
                if (otp != null) Text(listOfNotNull(otp.issuer, otp.account.ifBlank { null }).joinToString(" · "), color = Kino.faint, fontSize = 13.sp)
                if (!LocalTv.current) GlassButton(stringResource(R.string.twofa_open_app), {
                    runCatching { ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(s.otpauthUri))) }
                        .onFailure { error = ApiException(0, "no_authenticator", "") }
                }, Modifier.fillMaxWidth())
                Text(stringResource(R.string.twofa_manual), color = Kino.muted, fontSize = 13.sp)
                CopyableText(s.secret, groupSecret(s.secret))
                OutlinedTextField(code, { code = cleanTotpCode(it).take(8) }, label = { Text(stringResource(R.string.twofa_code)) }, singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
                    modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
                Actions {
                    PrimaryButton(stringResource(R.string.twofa_confirm), enabled = !busy && code.length >= 6, onClick = {
                        act { recovery = api.enableTotp(code).recoveryCodes; setup = null; code = ""; state.refreshMe() }
                    })
                    GlassButton(stringResource(R.string.cancel), { setup = null; code = "" })
                }
            }
        }
        ErrorLine(error)
    }
}
