package net.scyde.streamero

import android.app.Application
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import coil.ImageLoader
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.Enhancements
import net.scyde.streamero.api.User
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
    val sync = SyncSession()

    fun attach(a: Api?) {
        api = a
        images = a?.let { ImageLoader.Builder(app).okHttpClient(it.http).crossfade(true).build() }
        music.api = a
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
        session.setToken(null)
        me = null
    }
}
