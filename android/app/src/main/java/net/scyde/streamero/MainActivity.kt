package net.scyde.streamero

import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import androidx.activity.SystemBarStyle
import androidx.appcompat.app.AppCompatActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.appcompat.app.AppCompatDelegate
import androidx.core.os.LocaleListCompat
import net.scyde.streamero.api.SsoBroker
import net.scyde.streamero.ui.StreameroRoot

/**
 * An AppCompatActivity (not a plain ComponentActivity) so the in-app language applies on
 * Android 8–12 too: AppCompat stores the choice and applies it to the activity before Android 13.
 */
class MainActivity : AppCompatActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val bars = SystemBarStyle.dark(Color.TRANSPARENT)
        enableEdgeToEdge(statusBarStyle = bars, navigationBarStyle = bars)
        val state = (application as StreameroApp).state
        // A recreated activity (process death, language change) or one reopened from recents gets
        // the intent it was first started with again: its sign-in code was already used and its
        // invite already seen, so only a fresh start hands the link on.
        val fromHistory = ((intent?.flags ?: 0) and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY) != 0
        if (isFreshLink(savedInstanceState != null, fromHistory)) deliver(intent?.data)
        setContent { StreameroRoot(state) }
    }

    override fun onResume() {
        super.onResume()
        // Offline progress and revoked downloads are synced whenever the app comes back.
        (application as StreameroApp).state.offline.syncSoon()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        deliver(intent.data)
    }

    private fun deliver(uri: android.net.Uri?) {
        SsoBroker.deliver(uri)
        net.scyde.streamero.ui.InviteBroker.deliver(uri)
    }
}

/** Whether the link an activity was started with is new (not a restored or recents relaunch). */
fun isFreshLink(restored: Boolean, launchedFromHistory: Boolean): Boolean = !restored && !launchedFromHistory

/** Switches the app language (English is the default, Croatian on request). */
fun applyLanguage(lang: String?) {
    val tags = if (lang.isNullOrBlank()) LocaleListCompat.getEmptyLocaleList() else LocaleListCompat.forLanguageTags(lang)
    AppCompatDelegate.setApplicationLocales(tags)
}
