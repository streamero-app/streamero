package net.scyde.streamero

import android.app.Application
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import coil.ImageLoader
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.ApiException
import net.scyde.streamero.offline.OfflineManager
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
        state.offline.start()
    }
}

/** How reaching the server at start went. */
enum class Connection { Ok, SignedOut, Unreachable }

/** Everything the screens share: the server connection, who is signed in, and music. */
class AppState(val app: Application) {
    val session = Session(app)
    var api by mutableStateOf<Api?>(null)
    var me by mutableStateOf<User?>(null)
    var enhancements by mutableStateOf(Enhancements())
    var images by mutableStateOf<ImageLoader?>(null)
    val music = MusicController(app)
    val sync = SyncSession()
    /** Downloads kept on the device; usable without the server. */
    val offline = OfflineManager(app) { api ?: session.api ?: runCatching { session.restore() }.getOrNull() }

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

    /** Signs in with the stored token: a refused token means signed out, anything else unreachable. */
    suspend fun connect(): Connection {
        val a = api ?: return Connection.SignedOut
        return try {
            me = a.me()
            enhancements = runCatching { a.enhancements() }.getOrDefault(Enhancements())
            Connection.Ok
        } catch (e: ApiException) {
            if (e.status == 401) Connection.SignedOut else Connection.Unreachable
        } catch (e: Exception) {
            if (e is kotlinx.coroutines.CancellationException) throw e
            Connection.Unreachable
        }
    }

    suspend fun signOut() {
        if (sync.group != null) sync.leave()
        runCatching { api?.logout() }
        music.stop()
        session.setToken(null)
        me = null
    }
}
