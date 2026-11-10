package net.scyde.streamero.ui

import android.app.UiModeManager
import android.content.Context
import android.content.pm.PackageManager
import android.content.res.Configuration
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.composed
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.focus.FocusDirection
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import kotlinx.coroutines.delay

/**
 * Whether the app runs on a television: the system says so (UI mode) or the device
 * has the leanback feature. Pure, so it can be unit tested.
 */
fun isTelevision(uiModeType: Int, hasLeanback: Boolean): Boolean =
    uiModeType == Configuration.UI_MODE_TYPE_TELEVISION || hasLeanback

fun isTelevision(context: Context): Boolean {
    val mode = (context.getSystemService(Context.UI_MODE_SERVICE) as? UiModeManager)?.currentModeType ?: Configuration.UI_MODE_TYPE_NORMAL
    val pm = context.packageManager
    return isTelevision(mode, pm.hasSystemFeature(PackageManager.FEATURE_LEANBACK) || pm.hasSystemFeature(PackageManager.FEATURE_TELEVISION))
}

/** True when the interface is laid out for a TV and driven by a remote. */
val LocalTv = staticCompositionLocalOf { false }

/** Layout measures that differ between a phone and a 10-foot TV screen. */
object Dims {
    /** Horizontal page margin; TV keeps clear of overscan. */
    val edge: Dp @Composable get() = if (LocalTv.current) 40.dp else 20.dp
    val poster: Dp @Composable get() = if (LocalTv.current) 136.dp else 130.dp
    val gridCell: Dp @Composable get() = if (LocalTv.current) 128.dp else 118.dp
    val wide: Dp @Composable get() = if (LocalTv.current) 300.dp else 280.dp
    /** Room around a row of cards so a focused card can grow without being clipped. */
    val rowPad: PaddingValues @Composable get() = if (LocalTv.current) PaddingValues(horizontal = 40.dp, vertical = 14.dp) else PaddingValues(horizontal = 20.dp)
}

/** An interaction source that also reports whether it is focused. */
@Composable
fun rememberFocusSource(): Pair<MutableInteractionSource, Boolean> {
    val src = remember { MutableInteractionSource() }
    val focused by src.collectIsFocusedAsState()
    return src to focused
}

/**
 * The "Kino platno" focus state: the element grows a little, lifts, and gets a paper-white
 * outline. Invisible until focused, so on a phone (touch) nothing changes.
 */
fun Modifier.focusRing(focused: Boolean, shape: Shape = CardShape, scale: Float = 1.06f, width: Dp = 3.dp): Modifier = composed {
    val s by animateFloatAsState(if (focused) scale else 1f, tween(160), label = "focus-scale")
    this
        .zIndex(if (focused) 1f else 0f)
        .graphicsLayer { scaleX = s; scaleY = s }
        .then(if (focused) Modifier.shadow(18.dp, shape, ambientColor = Color.Black, spotColor = Color.Black) else Modifier)
        .border(width, if (focused) Kino.text else Color.Transparent, shape)
}

/** Clickable with the focus ring on the element itself (rows, tiles, links). */
fun Modifier.focusClickable(shape: Shape = CardShape, scale: Float = 1.04f, onClick: () -> Unit): Modifier = composed {
    val (src, focused) = rememberFocusSource()
    focusRing(focused, shape, scale).clickable(interactionSource = src, indication = tapIndication(), onClick = onClick)
}

/** Touch ripple on a phone; on a TV the focus ring is the feedback. */
@Composable
fun tapIndication(): androidx.compose.foundation.Indication? = if (LocalTv.current) null else androidx.compose.foundation.LocalIndication.current

/** Takes the focus once when it first appears on a TV (the screen's starting point). */
fun Modifier.initialFocus(enabled: Boolean = true): Modifier = composed {
    val tv = LocalTv.current
    if (!tv || !enabled) return@composed this
    val req = remember { FocusRequester() }
    LaunchedEffect(Unit) {
        // The node must be laid out before it can take focus.
        repeat(5) { delay(40); if (runCatching { req.requestFocus() }.isSuccess) return@LaunchedEffect }
    }
    focusRequester(req)
}

/** Which element of a screen had the focus last, so coming back to the screen returns there. */
class FocusMemory(var key: String?)

private val FocusMemorySaver = Saver<FocusMemory, String>(save = { it.key ?: "" }, restore = { FocusMemory(it.ifEmpty { null }) })

@Composable
fun rememberFocusMemory(): FocusMemory = rememberSaveable(saver = FocusMemorySaver) { FocusMemory(null) }

/**
 * Takes the focus when the screen opens if this element is the one remembered in [memory]
 * (or, when nothing is remembered yet, if it is the screen's [default] element), and records
 * itself in [memory] whenever it gets the focus.
 */
fun Modifier.rememberFocus(memory: FocusMemory, key: String, default: Boolean = false): Modifier = composed {
    val tv = LocalTv.current
    if (!tv) return@composed this
    val start = remember { memory.key == key || (memory.key == null && default) }
    this.initialFocus(start).onFocusChanged { if (it.isFocused) memory.key = key }
}

/**
 * A text field keeps the D-pad for its cursor; on a TV up and down leave it instead, so the
 * remote can reach the buttons around it.
 */
fun Modifier.dpadExit(): Modifier = composed {
    if (!LocalTv.current) return@composed this
    val focus = androidx.compose.ui.platform.LocalFocusManager.current
    onPreviewKeyEvent { e ->
        if (e.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
        when (e.key) {
            Key.DirectionDown -> focus.moveFocus(FocusDirection.Down)
            Key.DirectionUp -> focus.moveFocus(FocusDirection.Up)
            else -> false
        }
    }
}
