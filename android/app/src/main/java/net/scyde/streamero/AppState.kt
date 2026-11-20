package net.scyde.streamero

import android.app.Application
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import coil.ImageLoader
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import net.scyde.streamero.offline.OfflineAccounts
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.ApiException
import net.scyde.streamero.offline.OfflineManager
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
        // Debug builds report disk and network work on the main thread, and leaked resources.
        if (BuildConfig.DEBUG) {
            android.os.StrictMode.setThreadPolicy(android.os.StrictMode.ThreadPolicy.Builder().detectDiskReads().detectDiskWrites().detectNetwork().penaltyLog().build())
            android.os.StrictMode.setVmPolicy(android.os.StrictMode.VmPolicy.Builder().detectLeakedClosableObjects().detectLeakedRegistrationObjects().detectActivityLeaks().penaltyLog().build())
        }
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
    /**
     * One image loader for the life of the app (one memory and disk cache). Its requests go
     * through the current server's client, which adds the session token for that server only.
     */
    private val imageClient = okhttp3.OkHttpClient()
    var images by mutableStateOf<ImageLoader?>(
        ImageLoader.Builder(app)
            .callFactory(object : okhttp3.Call.Factory {
                override fun newCall(request: okhttp3.Request): okhttp3.Call = (api?.http ?: imageClient).newCall(request)
            })
            .crossfade(true)
            .build()
    )
        private set
    val music = MusicController(app)
    val book = AudiobookController(app)
    val sync = SyncSession()
    /** Downloads kept on the device; usable without the server. */
    val offline = OfflineManager(app) { api ?: session.api ?: runCatching { session.restore() }.getOrNull() }
    /** The photos around the one opened in the viewer, to swipe through (set by the grid). */
    var photoStrip by mutableStateOf<List<ItemSummary>>(emptyList())

    init {
        // Music and audiobooks share one background player; whichever starts takes it over.
        music.onStart = { book.release(stopPlayer = false) }
        book.onStart = { music.stop() }
    }

    /** Work that must finish after a screen is gone (the last progress report of a closed player, the reading position). */
    val appScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    /** The server stopped accepting the session after start: the app goes back to sign-in. */
    var sessionExpired by mutableStateOf(false)

    fun attach(a: Api?) {
        api = a
        a?.onUnauthorized = { appScope.launch { if (api === a) expireSession() } }
        // Another server or account: what is cached in memory belongs to the previous one.
        images?.memoryCache?.clear()
        music.api = a
        book.api = a
        sync.api = a
    }

    val isAdmin: Boolean get() = me?.isAdmin == true && me?.currentProfile?.isKid != true
    val isKid: Boolean get() = me?.currentProfile?.isKid == true

    suspend fun refreshMe(): User? {
        val a = api ?: return null
        me = try { a.me() } catch (e: kotlinx.coroutines.CancellationException) { throw e } catch (e: Exception) { null }
        enhancements = try { a.enhancements() } catch (e: kotlinx.coroutines.CancellationException) { throw e } catch (e: Exception) { Enhancements() }
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
            if (e.status == 401) { forgetSession(); Connection.SignedOut } else Connection.Unreachable
        } catch (e: Exception) {
            if (e is kotlinx.coroutines.CancellationException) throw e
            Connection.Unreachable
        }
    }

    suspend fun signOut() {
        // Whatever the server says, the session is forgotten on this device.
        try {
            if (sync.group != null) sync.leave()
            api?.logout()
        } catch (e: kotlinx.coroutines.CancellationException) {
            throw e
        } catch (e: Exception) {
            // Unreachable server or an already revoked session: nothing more to do there.
        } finally {
            forgetSession()
        }
    }

    /** After sign-in: shows (and syncs) the downloads of this account on this server only. */
    suspend fun bindOffline() {
        val a = api ?: return
        val u = me ?: return
        val serverId = runCatching { a.info().id }.getOrElse { a.base.host + "-" + a.base.port }
        offline.bind(OfflineAccounts.key(serverId, u.id))
        sessionExpired = false
    }

    private suspend fun expireSession() {
        if (sessionExpired || me == null) return
        if (sync.group != null) sync.leaveInBackground()
        forgetSession()
        sessionExpired = true
    }

    private suspend fun forgetSession() {
        music.stop()
        book.release(stopPlayer = true)
        session.setToken(null)
        me = null
        offline.bind(null)
    }
}
