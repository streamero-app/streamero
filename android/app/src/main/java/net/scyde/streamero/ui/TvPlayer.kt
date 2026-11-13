package net.scyde.streamero.ui

import net.scyde.streamero.player.SubtitleChoice
import androidx.compose.foundation.lazy.items

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.key.*
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import net.scyde.streamero.R
import net.scyde.streamero.api.PlaybackInfo

/** Where a seek of [deltaMs] from [fromMs] lands, kept inside the film (when its length is known). */
fun seekTarget(fromMs: Long, deltaMs: Long, durationMs: Long): Long {
    val t = (fromMs + deltaMs).coerceAtLeast(0)
    return if (durationMs > 0) t.coerceAtMost(durationMs) else t
}

/** The remote's media keys mapped to what the player does. */
enum class RemoteAction { Toggle, Play, Pause, Forward, Rewind, Next, Stop }

fun remoteAction(key: Key): RemoteAction? = when (key) {
    Key.MediaPlayPause, Key.Spacebar -> RemoteAction.Toggle
    Key.MediaPlay -> RemoteAction.Play
    Key.MediaPause -> RemoteAction.Pause
    Key.MediaFastForward -> RemoteAction.Forward
    Key.MediaRewind -> RemoteAction.Rewind
    Key.MediaNext -> RemoteAction.Next
    Key.MediaStop -> RemoteAction.Stop
    else -> null
}

/** The progress line with the time on either side; [targetMs] is a seek still being chosen. */
@Composable
fun TvTimeline(positionMs: Long, durationMs: Long, targetMs: Long?, modifier: Modifier = Modifier, focused: Boolean = false) {
    val accent = LocalAccent.current
    val shown = targetMs ?: positionMs
    Column(modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Box(Modifier.fillMaxWidth().height(if (focused) 8.dp else 5.dp).clip(CircleShape).background(Color.White.copy(alpha = 0.22f))) {
            val pct = if (durationMs > 0) (positionMs.toFloat() / durationMs).coerceIn(0f, 1f) else 0f
            Box(Modifier.fillMaxHeight().fillMaxWidth(pct).background(if (targetMs != null) Color.White.copy(alpha = 0.45f) else accent))
            if (targetMs != null && durationMs > 0) Box(Modifier.fillMaxHeight().fillMaxWidth((targetMs.toFloat() / durationMs).coerceIn(0f, 1f)).background(accent))
        }
        Row(Modifier.fillMaxWidth()) {
            Text(clock(shown), color = Kino.text, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.weight(1f))
            if (durationMs > 0) Text("-" + clock((durationMs - shown).coerceAtLeast(0)), color = Kino.muted, fontSize = 15.sp)
        }
    }
}

/** A round player button for the remote: paper-white when focused. */
@Composable
fun TvPlayerButton(icon: ImageVector, label: String, modifier: Modifier = Modifier, wide: Boolean = false, onClick: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    Row(modifier.height(52.dp).then(if (wide) Modifier else Modifier.width(52.dp)).focusRing(focused, CircleShape, scale = 1.1f, width = 0.dp)
        .clip(CircleShape).background(if (focused) Kino.text else Color.White.copy(alpha = 0.14f))
        .clickable(interactionSource = src, indication = null, onClick = onClick).padding(horizontal = if (wide) 20.dp else 0.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally), verticalAlignment = Alignment.CenterVertically) {
        Icon(icon, label, tint = if (focused) Kino.bg else Kino.text, modifier = Modifier.size(26.dp))
        if (wide) Text(label, color = if (focused) Kino.bg else Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 15.sp, maxLines = 1)
    }
}

/** Large "+30 s" / "-10 s" note in the middle while seeking with the remote. */
@Composable
fun BoxScope.SeekNote(deltaMs: Long?) {
    AnimatedVisibility(deltaMs != null, Modifier.align(Alignment.Center), enter = fadeIn(), exit = fadeOut()) {
        val d = deltaMs ?: 0L
        Row(Modifier.clip(CircleShape).background(Color.Black.copy(alpha = 0.6f)).padding(horizontal = 26.dp, vertical = 14.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Icon(if (d < 0) Icons.Default.FastRewind else Icons.Default.FastForward, null, tint = Kino.text, modifier = Modifier.size(30.dp))
            Text((if (d < 0) "−" else "+") + clock(kotlin.math.abs(d)), color = Kino.text, fontSize = 26.sp, fontWeight = FontWeight.Bold)
        }
    }
}

/** Audio and subtitle choice as a side panel the remote moves through. */
@Composable
fun TvTracksPanel(info: PlaybackInfo, audio: Int?, subtitle: SubtitleChoice, onAudio: (Int) -> Unit, onSubtitle: (SubtitleChoice) -> Unit, modifier: Modifier = Modifier) {
    val subs = subtitleOptions(info)
    LazyColumn(modifier.fillMaxHeight().width(380.dp).background(Brush.horizontalGradient(listOf(Color.Transparent, Kino.bg.copy(alpha = 0.92f), Kino.bg), endX = 120f))
        .padding(start = 40.dp, end = 28.dp), contentPadding = PaddingValues(vertical = 36.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (subs.size > 1) {
            item { Text(stringResource(R.string.subtitles).uppercase(), style = Head, color = Kino.text, modifier = Modifier.padding(bottom = 6.dp)) }
            items(subs) { (choice, label) -> ChoiceRow(label, subtitle == choice, Modifier.initialFocus(subtitle == choice)) { onSubtitle(choice) } }
        }
        if (info.audio.size > 1) {
            item { Text(stringResource(R.string.audio).uppercase(), style = Head, color = Kino.text, modifier = Modifier.padding(top = 18.dp, bottom = 6.dp)) }
            itemsIndexed(info.audio) { i, a ->
                val chosen = (audio ?: info.audioIndex ?: 0) == i
                ChoiceRow(audioLabel(a), chosen, Modifier.initialFocus(chosen && subs.size <= 1)) { onAudio(i) }
            }
        }
    }
}
