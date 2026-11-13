package net.scyde.streamero.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil.ImageLoader
import coil.compose.AsyncImage
import coil.request.ImageRequest
import net.scyde.streamero.api.ApiException
import net.scyde.streamero.api.ItemSummary
import net.scyde.streamero.R

val LocalImages = staticCompositionLocalOf<ImageLoader?> { null }

/** An image from the server, loaded with the signed-in session. */
@Composable
fun ServerImage(url: String?, modifier: Modifier = Modifier, contentScale: ContentScale = ContentScale.Crop) {
    val loader = LocalImages.current ?: return
    if (url == null) return
    AsyncImage(
        model = ImageRequest.Builder(LocalContext.current).data(url).crossfade(true).build(),
        imageLoader = loader, contentDescription = null, contentScale = contentScale, modifier = modifier,
    )
}

val CardShape = RoundedCornerShape(6.dp)

@Composable
fun PrimaryButton(text: String, onClick: () -> Unit, modifier: Modifier = Modifier, icon: ImageVector? = null, enabled: Boolean = true) {
    val (src, focused) = rememberFocusSource()
    Button(onClick = onClick, enabled = enabled, modifier = modifier.focusRing(focused, scale = 1.05f).heightIn(min = 50.dp), shape = CardShape, interactionSource = src,
        colors = ButtonDefaults.buttonColors(containerColor = Kino.text, contentColor = Kino.bg)) {
        if (icon != null) { Icon(icon, null, Modifier.size(20.dp)); Spacer(Modifier.width(8.dp)) }
        Text(text, fontWeight = FontWeight.SemiBold, fontSize = 16.sp)
    }
}

@Composable
fun GlassButton(text: String?, onClick: () -> Unit, modifier: Modifier = Modifier, icon: ImageVector? = null, active: Boolean = false, enabled: Boolean = true) {
    val (src, focused) = rememberFocusSource()
    Button(onClick = onClick, enabled = enabled, modifier = modifier.focusRing(focused, scale = 1.05f).heightIn(min = 50.dp), shape = CardShape, interactionSource = src,
        contentPadding = if (text == null) PaddingValues(0.dp) else ButtonDefaults.ContentPadding,
        colors = ButtonDefaults.buttonColors(containerColor = if (active || focused) Kino.text else Color.White.copy(alpha = 0.12f),
            contentColor = if (active || focused) Kino.bg else Kino.text)) {
        if (icon != null) Icon(icon, text, Modifier.size(20.dp))
        if (icon != null && text != null) Spacer(Modifier.width(8.dp))
        if (text != null) Text(text, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
    }
}

@Composable
fun SectionTitle(text: String, modifier: Modifier = Modifier) {
    Text(text.uppercase(), style = Head, color = Kino.text, modifier = modifier.padding(horizontal = Dims.edge))
}

@Composable
fun ProgressLine(value: Float, modifier: Modifier = Modifier) {
    if (value <= 0f) return
    val accent = LocalAccent.current
    Box(modifier.fillMaxWidth().height(3.dp).background(Color.White.copy(alpha = 0.25f))) {
        Box(Modifier.fillMaxHeight().fillMaxWidth(value).background(accent))
    }
}

/** A poster card (2:3) with name and year. */
@Composable
fun PosterCard(item: ItemSummary, imageUrl: (String, String) -> String, width: Dp, qualityTags: Boolean = true, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    Column(modifier.width(width).clickable(interactionSource = src, indication = tapIndication(), onClick = onClick)) {
        Box(Modifier.fillMaxWidth().focusRing(focused).aspectRatio(2f / 3f).clip(CardShape).background(Kino.surface2)) {
            when {
                item.hasPoster -> ServerImage(imageUrl(item.id, if (item.kind == "episode") "still" else "poster"), Modifier.fillMaxSize())
                item.hasBackdrop -> ServerImage(imageUrl(item.id, "backdrop"), Modifier.fillMaxSize())
                else -> Text(item.title, style = Display.copy(fontSize = 20.sp), color = Kino.text, modifier = Modifier.align(Alignment.BottomStart).padding(10.dp))
            }
            if (item.played) Icon(Icons.Default.Check, null, tint = Kino.text, modifier = Modifier.align(Alignment.TopEnd).padding(6.dp).background(Color.Black.copy(alpha = 0.7f), RoundedCornerShape(4.dp)).padding(3.dp).size(14.dp))
            if (qualityTags) Row(Modifier.align(Alignment.BottomStart).padding(6.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                qualityTags(item.videoHeight, item.hdr).forEach { Tag(it) }
            }
            ProgressLine(item.progress, Modifier.align(Alignment.BottomCenter))
        }
        Spacer(Modifier.height(if (focused) 12.dp else 8.dp))
        Text(item.title, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(listOfNotNull(item.artist?.takeIf { item.kind == "album" }, item.year?.toString()).joinToString(" · "), color = Kino.faint, fontSize = 13.sp, maxLines = 1)
    }
}

@Composable
fun Tag(text: String) {
    Text(text, color = Kino.text, fontSize = 10.sp, fontWeight = FontWeight.Bold,
        modifier = Modifier.background(Color.Black.copy(alpha = 0.75f), RoundedCornerShape(3.dp)).padding(horizontal = 5.dp, vertical = 1.dp))
}

fun qualityTags(height: Int?, hdr: String?): List<String> = buildList {
    if (height != null && height >= 2000) add("4K") else if (height != null && height >= 700) add("HD")
    if (!hdr.isNullOrBlank()) add(if (Regex("dolby|dovi|dv", RegexOption.IGNORE_CASE).containsMatchIn(hdr)) "DV" else if (hdr.contains("HLG", true)) "HLG" else "HDR")
}

/** A landscape card (continue watching): a frame from the film, title over it. */
@Composable
@OptIn(androidx.compose.foundation.ExperimentalFoundationApi::class)
fun WideCard(item: ItemSummary, imageUrl: (String, String) -> String, width: Dp, modifier: Modifier = Modifier, onLongClick: (() -> Unit)? = null, onClick: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    Box(modifier.width(width).focusRing(focused).aspectRatio(16f / 9f).clip(CardShape).background(Kino.surface2)
        .combinedClickable(interactionSource = src, indication = tapIndication(), onLongClick = onLongClick, onClick = onClick)) {
        val src = when {
            item.hasBackdrop -> imageUrl(item.id, "backdrop")
            item.kind == "episode" && item.hasPoster -> imageUrl(item.id, "still")
            item.seriesId != null -> imageUrl(item.seriesId, "backdrop")
            else -> null
        }
        ServerImage(src, Modifier.fillMaxSize())
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Color.Transparent, Color.Black.copy(alpha = 0.85f)))))
        Column(Modifier.align(Alignment.BottomStart).padding(12.dp)) {
            Text(item.seriesTitle ?: item.title, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val sub = if (item.seriesTitle != null) androidx.compose.ui.res.stringResource(R.string.episode_code_title, item.seasonNumber ?: 0, item.episodeNumber ?: 0, item.title) else item.year?.toString().orEmpty()
            Text(sub, color = Kino.muted, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        ProgressLine(item.progress, Modifier.align(Alignment.BottomCenter))
    }
}

@Composable
fun <T> Shelf(title: String, items: List<T>, content: @Composable (T) -> Unit) {
    if (items.isEmpty()) return
    val tv = LocalTv.current
    Column(verticalArrangement = Arrangement.spacedBy(if (tv) 2.dp else 12.dp)) {
        SectionTitle(title)
        LazyRow(contentPadding = Dims.rowPad, horizontalArrangement = Arrangement.spacedBy(if (tv) 18.dp else 14.dp)) {
            items(items) { content(it) }
        }
    }
}

@Composable
fun Avatar(name: String, color: String?, imageUrl: String?, size: Dp, round: Boolean = false) {
    val shape = if (round) CircleShape else RoundedCornerShape(6.dp)
    Box(Modifier.size(size).clip(shape).background(Brush.linearGradient(listOf(hexColor(color, Color(0xFF8A5A3A)), hexColor(color, Color(0xFF8A5A3A)).copy(alpha = 0.55f)))),
        contentAlignment = Alignment.Center) {
        Text(name.take(1).uppercase(), color = Color.White, fontWeight = FontWeight.Bold, fontSize = (size.value * 0.42f).sp)
        if (imageUrl != null) ServerImage(imageUrl, Modifier.fillMaxSize())
    }
}

/**
 * The message for a server error code (the `code` of the error body, as in the web client's
 * errors.*); null for codes this app has no wording for, which then get a generic message.
 */
fun errorMessageRes(code: String): Int? = when (code) {
    "invalid_credentials" -> R.string.err_credentials
    "totp_required" -> R.string.err_totp
    "invalid_totp" -> R.string.err_invalid_totp
    "forbidden" -> R.string.err_forbidden
    "parental_limit" -> R.string.err_parental_limit
    "upstream_failed" -> R.string.err_upstream
    "invalid_state" -> R.string.err_sso_invalid_state
    "provider_error", "internal" -> R.string.err_sso_provider
    "no_account", "identity_taken" -> R.string.err_sso_no_account
    "not_allowed" -> R.string.err_sso_not_allowed
    "account_disabled" -> R.string.err_sso_disabled
    "not_configured", "unknown_provider" -> R.string.err_sso_not_configured
    "password_login_disabled" -> R.string.err_password_disabled
    "tuners_busy" -> R.string.livetv_tuners_busy
    "unauthenticated" -> R.string.err_session_expired
    "account_locked" -> R.string.err_account_locked
    "rate_limited" -> R.string.err_rate_limited
    "not_found" -> R.string.err_not_found
    "invalid_request" -> R.string.err_invalid_request
    "username_taken" -> R.string.err_username_taken
    "invalid_username" -> R.string.err_invalid_username
    "last_admin" -> R.string.err_last_admin
    "self" -> R.string.err_self
    "invalid_name" -> R.string.err_invalid_name
    "session_required" -> R.string.err_session_required
    "pin_required" -> R.string.err_pin_required
    "invalid_pin" -> R.string.err_invalid_pin
    "profile_locked" -> R.string.err_profile_locked
    "kid_profile" -> R.string.err_kid_profile
    "invalid_path" -> R.string.err_invalid_path
    "invalid_tmdb_key" -> R.string.err_invalid_tmdb_key
    "tmdb_unreachable" -> R.string.err_tmdb_unreachable
    "transcode_not_allowed", "transcode_denied" -> R.string.err_transcode_not_allowed
    "transcode_failed" -> R.string.err_transcode_failed
    "transcoder_busy" -> R.string.err_transcoder_busy
    "not_playable" -> R.string.err_not_playable
    "file_missing" -> R.string.err_file_missing
    "channel_unavailable" -> R.string.err_channel_unavailable
    "invalid_email" -> R.string.err_invalid_email
    "invalid_image" -> R.string.err_invalid_image
    "image_too_large" -> R.string.err_image_too_large
    "invalid_url" -> R.string.err_invalid_url
    "not_downloadable" -> R.string.err_not_downloadable
    "too_many_profiles" -> R.string.err_too_many_profiles
    "totp_enabled" -> R.string.err_totp_enabled
    "totp_not_started" -> R.string.err_totp_not_started
    "no_authenticator" -> R.string.err_no_authenticator
    "backup_newer_schema" -> R.string.err_backup_newer_schema
    "invalid_backup" -> R.string.err_invalid_backup
    "backup_busy" -> R.string.err_backup_busy
    "backup_confirm_mismatch" -> R.string.err_backup_confirm_mismatch
    "no_sso_admin" -> R.string.err_no_sso_admin
    "no_local_password" -> R.string.err_no_local_password
    else -> null
}

/** A translated message for an error: a server code, a player failure or no connection. */
@Composable
fun errorText(e: Throwable?): String? {
    if (e == null) return null
    val res = when (e) {
        is ApiException -> errorMessageRes(e.code) ?: R.string.err_generic
        is androidx.media3.common.PlaybackException -> R.string.err_playback
        is java.io.IOException -> R.string.err_network
        else -> R.string.err_generic
    }
    return androidx.compose.ui.res.stringResource(res)
}

/** Asks before something that cannot be undone. */
@Composable
fun ConfirmDialog(text: String, confirm: String, onDismiss: () -> Unit, onConfirm: () -> Unit) {
    AlertDialog(onDismissRequest = onDismiss, containerColor = Kino.surface,
        text = { Text(text, color = Kino.text) },
        confirmButton = { TextButton(onClick = { onDismiss(); onConfirm() }) { Text(confirm, color = Kino.danger) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(androidx.compose.ui.res.stringResource(R.string.cancel), color = Kino.muted) } })
}

@Composable
fun ErrorLine(e: Throwable?) {
    val t = errorText(e) ?: return
    Text(t, color = Kino.danger, fontSize = 14.sp)
}

@Composable
fun Loading(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth().padding(40.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(color = Kino.text, strokeWidth = 2.dp) }
}

@Composable
fun FieldRow(label: String, hint: String? = null, content: @Composable () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(label, color = Kino.text, fontSize = 15.sp, fontWeight = FontWeight.Medium)
        if (hint != null) Text(hint, color = Kino.faint, fontSize = 13.sp)
        content()
    }
}

@Composable
fun Card(title: String?, modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Surface(modifier.fillMaxWidth(), shape = RoundedCornerShape(8.dp), color = Kino.surface, border = BorderStroke(1.dp, Kino.border)) {
        Column(Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            if (title != null) Text(title.uppercase(), style = Head.copy(fontSize = 16.sp), color = Kino.text, modifier = Modifier.padding(bottom = 6.dp))
            content()
        }
    }
}

/** A single-choice dropdown. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun <T> Picker(value: T, options: List<Pair<T, String>>, onChange: (T) -> Unit, modifier: Modifier = Modifier) {
    var open by remember { mutableStateOf(false) }
    if (LocalTv.current) { TvPicker(value, options, onChange, modifier); return }
    ExposedDropdownMenuBox(expanded = open, onExpandedChange = { open = it }, modifier = modifier) {
        OutlinedTextField(value = options.firstOrNull { it.first == value }?.second.orEmpty(), onValueChange = {}, readOnly = true,
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(open) },
            modifier = Modifier.menuAnchor(MenuAnchorType.PrimaryNotEditable).fillMaxWidth(), shape = CardShape)
        ExposedDropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            options.forEach { (v, label) -> DropdownMenuItem(text = { Text(label) }, onClick = { onChange(v); open = false }) }
        }
    }
}

/** "1 item", "2 items", with the plural forms of the current language. */
@Composable
fun itemsCount(n: Int): String = androidx.compose.ui.platform.LocalContext.current.resources.getQuantityString(R.plurals.items_count, n, n)

/** On a TV a dropdown is awkward with a remote: a button that opens a focusable list instead. */
@Composable
private fun <T> TvPicker(value: T, options: List<Pair<T, String>>, onChange: (T) -> Unit, modifier: Modifier) {
    var open by remember { mutableStateOf(false) }
    val (src, focused) = rememberFocusSource()
    Row(modifier.fillMaxWidth().focusRing(focused, scale = 1.02f).clip(CardShape).background(if (focused) Kino.surface3 else Kino.surface2)
        .clickable(interactionSource = src, indication = tapIndication()) { open = true }.padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically) {
        Text(options.firstOrNull { it.first == value }?.second.orEmpty(), color = Kino.text, fontSize = 16.sp, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
        Icon(Icons.Default.ExpandMore, null, tint = Kino.muted)
    }
    if (open) ChoiceDialog(options, value, onDismiss = { open = false }) { onChange(it); open = false }
}

/** A list of choices in a dialog, the current one focused first. */
@Composable
fun <T> ChoiceDialog(options: List<Pair<T, String>>, selected: T, title: String? = null, onDismiss: () -> Unit, onChoose: (T) -> Unit) {
    androidx.compose.ui.window.Dialog(onDismissRequest = onDismiss) {
        Surface(shape = RoundedCornerShape(10.dp), color = Kino.surface, border = BorderStroke(1.dp, Kino.border)) {
            androidx.compose.foundation.lazy.LazyColumn(Modifier.widthIn(min = 320.dp, max = 480.dp).heightIn(max = 460.dp), contentPadding = PaddingValues(14.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp)) {
                if (title != null) item { Text(title.uppercase(), style = Head, color = Kino.text, modifier = Modifier.padding(6.dp, 4.dp, 6.dp, 10.dp)) }
                items(options) { (v, label) -> ChoiceRow(label, v == selected, Modifier.initialFocus(v == selected)) { onChoose(v) } }
            }
        }
    }
}

/** One selectable line (a language, a subtitle track...) with a check when chosen. */
@Composable
fun ChoiceRow(label: String, selected: Boolean, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    Row(modifier.fillMaxWidth().focusRing(focused, scale = 1.02f, width = 2.dp).clip(CardShape)
        .background(if (focused) Kino.text else Color.Transparent).clickable(interactionSource = src, indication = tapIndication(), onClick = onClick)
        .padding(horizontal = 14.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Box(Modifier.size(20.dp)) { if (selected) Icon(Icons.Default.Check, null, tint = if (focused) Kino.bg else Kino.text) }
        Text(label, color = if (focused) Kino.bg else Kino.text, fontSize = 16.sp, fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}
