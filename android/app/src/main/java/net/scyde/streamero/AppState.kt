package net.scyde.streamero

import android.app.Application
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import coil.ImageLoader
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.Enhancements
import net.scyde.streamero.api.ItemSummary
import net.scyde.streamero.api.User
import net.scyde.streamero.books.AudiobookController
import net.scyde.streamero.data.Session
import net.scyde.streamero.music.MusicController
import net.scyde.streamero.sync.SyncSession

class StreameroApp : Application() {
    lateinit var state: AppState
    override fun onCreate() {
        super.onCreate()
        state = AppState(this)
    }
}

/** Everything the screens share: the server connection, who is signed in, and music. */
class AppState(val app: Application) {
    val session = Session(app)
    var api by mutableStateOf<Api?>(null)
    var me by mutableStateOf<User?>(null)
    var enhancements by mutableStateOf(Enhancements())
    var images by mutableStateOf<ImageLoader?>(null)
    val music = MusicController(app)
    val book = AudiobookController(app)
    /** For work that must finish after a screen is gone (saving the reading position). */
    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    val sync = SyncSession()
    /** The photos around the one opened in the viewer, to swipe through (set by the grid). */
    var photoStrip by mutableStateOf<List<ItemSummary>>(emptyList())

    init {
        // Music and audiobooks share one background player; whichever starts takes it over.
        music.onStart = { book.release(stopPlayer = false) }
        book.onStart = { music.stop() }
    }

    fun attach(a: Api?) {
        api = a
        images = a?.let { ImageLoader.Builder(app).okHttpClient(it.http).crossfade(true).build() }
        music.api = a
        book.api = a
        sync.api = a
    }

    val isAdmin: Boolean get() = me?.isAdmin == true && me?.currentProfile?.isKid != true
    val isKid: Boolean get() = me?.currentProfile?.isKid == true

    suspend fun refreshMe(): User? {
        val a = api ?: return null
        me = runCatching { a.me() }.getOrNull()
        enhancements = runCatching { a.enhancements() }.getOrDefault(Enhancements())
        return me
    }

    suspend fun signOut() {
        if (sync.group != null) sync.leave()
        runCatching { api?.logout() }
        music.stop()
        book.release(stopPlayer = true)
        session.setToken(null)
        me = null
    }
}
