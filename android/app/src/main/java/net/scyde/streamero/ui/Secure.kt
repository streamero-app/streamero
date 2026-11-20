package net.scyde.streamero.ui

import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.ContextWrapper
import android.os.PersistableBundle
import android.view.WindowManager
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.ui.platform.LocalContext

/** The activity a composable runs in (null in previews and tests). */
tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}

/**
 * How many secrets are on screen right now. The window flag is shared by the whole activity,
 * so it is only cleared when the last screen showing a secret is gone. Main thread only.
 */
private var secretsShown = 0

/**
 * Keeps the screen out of screenshots, screen recordings and the recents thumbnail while a
 * secret (recovery codes, a new API key, an authenticator secret) is displayed.
 */
@Composable
fun SecureScreen() {
    val activity = LocalContext.current.findActivity() ?: return
    DisposableEffect(activity) {
        if (secretsShown++ == 0) activity.window.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
        onDispose {
            if (--secretsShown == 0) activity.window.clearFlags(WindowManager.LayoutParams.FLAG_SECURE)
        }
    }
}

/**
 * `ClipDescription.EXTRA_IS_SENSITIVE`. The platform constant exists from API 33; the key is
 * simply ignored by older versions, which show no clipboard preview anyway.
 */
internal const val SENSITIVE_CLIP_EXTRA = "android.content.extra.IS_SENSITIVE"

/**
 * Puts [text] on the clipboard. A [sensitive] value (a key, a recovery code) is flagged so
 * Android 13+ hides it in the clipboard preview.
 */
fun copyText(ctx: Context, text: String, sensitive: Boolean = false) {
    val clipboard = ctx.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager ?: return
    val clip = ClipData.newPlainText("Streamero", text)
    if (sensitive) clip.description.extras = PersistableBundle().apply { putBoolean(SENSITIVE_CLIP_EXTRA, true) }
    // Vendor clipboards can throw (app not focused, clip too large); copying must never crash a screen.
    runCatching { clipboard.setPrimaryClip(clip) }
}
