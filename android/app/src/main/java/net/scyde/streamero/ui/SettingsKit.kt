package net.scyde.streamero.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import net.scyde.streamero.R
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

// Small building blocks shared by the account and administration pages.

/** A short status label next to a title ("On", "This device", "Kids"). */
@Composable
fun Pill(text: String, color: Color = Kino.muted) {
    Text(text, color = color, fontSize = 11.sp, fontWeight = FontWeight.SemiBold, maxLines = 1,
        modifier = Modifier.border(1.dp, color.copy(alpha = 0.45f), RoundedCornerShape(50)).padding(horizontal = 8.dp, vertical = 2.dp))
}

/** A bordered message box: a warning, a result or something to keep (recovery codes). */
@Composable
fun Notice(color: Color, content: @Composable ColumnScope.() -> Unit) {
    Surface(Modifier.fillMaxWidth(), shape = RoundedCornerShape(8.dp), color = color.copy(alpha = 0.08f), border = BorderStroke(1.dp, color.copy(alpha = 0.4f))) {
        Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp), content = content)
    }
}

/** A text the user should copy (a secret, a link): selectable, monospaced, with a copy button. */
@Composable
fun CopyableText(value: String, display: String = value) {
    val ctx = LocalContext.current
    var copied by remember { mutableStateOf(false) }
    LaunchedEffect(copied) { if (copied) { delay(2000); copied = false } }
    Row(Modifier.fillMaxWidth().background(Kino.bg, CardShape).padding(start = 12.dp), verticalAlignment = Alignment.CenterVertically) {
        androidx.compose.foundation.text.selection.SelectionContainer(Modifier.weight(1f)) {
            Text(display, color = Kino.text, fontSize = 14.sp, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace)
        }
        TextButton(onClick = { copyText(ctx, value); copied = true }) {
            Icon(Icons.Outlined.ContentCopy, null, Modifier.size(16.dp), tint = Kino.muted)
            Spacer(Modifier.width(6.dp))
            Text(stringResource(if (copied) R.string.copied else R.string.copy), color = Kino.muted)
        }
    }
}

fun copyText(ctx: Context, text: String) {
    (ctx.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager)?.setPrimaryClip(ClipData.newPlainText("Streamero", text))
}

/** A red text action (Delete, Remove) that asks first through [ConfirmDialog] at the call site. */
@Composable
fun DangerAction(text: String, enabled: Boolean = true, onClick: () -> Unit) {
    TextButton(onClick = onClick, enabled = enabled) { Text(text, color = if (enabled) Kino.danger else Kino.faint) }
}

/** Server timestamps in the device language ("13 Nov 2026, 14:05"); unparsable ones as they are. */
fun shortDateTime(iso: String?): String {
    if (iso.isNullOrBlank()) return "—"
    return runCatching {
        OffsetDateTime.parse(iso).atZoneSameInstant(ZoneId.systemDefault())
            .format(DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT))
    }.getOrElse { iso.take(16).replace('T', ' ') }
}

/** Loads again whenever [key] changes but keeps showing the last result meanwhile (no flicker). */
@Composable
fun <T> rememberReload(key: Any?, load: suspend () -> T): Pair<T?, Throwable?> {
    var value by remember { mutableStateOf<T?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    LaunchedEffect(key) { runCatching { load() }.onSuccess { value = it; error = null }.onFailure { error = it } }
    return value to error
}

/** A row of action buttons that wraps on narrow phones. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun Actions(content: @Composable () -> Unit) {
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) { content() }
}

/** The intro line under a page or card title. */
@Composable
fun Intro(text: String) = Text(text, color = Kino.muted, fontSize = 14.sp)
