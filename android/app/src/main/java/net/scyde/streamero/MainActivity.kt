package net.scyde.streamero

import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import androidx.activity.SystemBarStyle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.appcompat.app.AppCompatDelegate
import androidx.core.os.LocaleListCompat
import net.scyde.streamero.api.SsoBroker
import net.scyde.streamero.ui.StreameroRoot

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val bars = SystemBarStyle.dark(Color.TRANSPARENT)
        enableEdgeToEdge(statusBarStyle = bars, navigationBarStyle = bars)
        val state = (application as StreameroApp).state
        SsoBroker.deliver(intent?.data)
        setContent { StreameroRoot(state) }
    }

    override fun onResume() {
        super.onResume()
        // Offline progress and revoked downloads are synced whenever the app comes back.
        (application as StreameroApp).state.offline.syncSoon()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        SsoBroker.deliver(intent.data)
    }
}

/** Switches the app language (English is the default, Croatian on request). */
fun applyLanguage(lang: String?) {
    val tags = if (lang.isNullOrBlank()) LocaleListCompat.getEmptyLocaleList() else LocaleListCompat.forLanguageTags(lang)
    AppCompatDelegate.setApplicationLocales(tags)
}
