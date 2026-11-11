package net.scyde.streamero.sync

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.Job
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.CreateSyncGroup
import net.scyde.streamero.api.SyncClientMessage
import net.scyde.streamero.api.SyncGroup
import net.scyde.streamero.api.SyncServerMessage
import net.scyde.streamero.api.json
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener

/**
 * The watch-together membership of this device: REST calls to create, join and leave a
 * group, and while in one a socket that is kept open (reconnecting with backoff; the server
 * keeps the membership across reconnects). Everything runs on the main thread; socket
 * callbacks are handed over to it.
 */
class SyncSession {
    private val scope = MainScope()
    var api: Api? = null
        set(value) { if (value !== field) { close(); group = null; engine.onState(null) }; field = value }

    var group by mutableStateOf<SyncGroup?>(null)
        private set
    var connected by mutableStateOf(false)
        private set
    /** Why the server last took this device out of its group (left, removed, closed). */
    var endedReason by mutableStateOf<String?>(null)
    /** The last refused command (rate_limited, forbidden, invalid, ...). */
    var lastError by mutableStateOf<String?>(null)

    val engine = SyncEngine()
    private var socket: WebSocket? = null
    private var generation = 0
    private var attempt = 0
    private var pinger: Job? = null
    private var retry: Job? = null

    private fun now() = System.currentTimeMillis()

    suspend fun create(itemId: String, startMs: Long?): SyncGroup =
        api!!.createSyncGroup(CreateSyncGroup(itemIds = listOf(itemId), index = 0, startMs = startMs?.takeIf { it > 0 })).also(::enter)

    suspend fun join(groupId: String): SyncGroup = api!!.joinSyncGroup(groupId).also(::enter)

    /** Picks up the group this session is still in (after the app was restarted). */
    suspend fun restore(): SyncGroup? = api?.mySyncGroup()?.also(::enter)

    suspend fun leave() {
        val a = api
        close()
        applyGroup(null)
        runCatching { a?.leaveSyncGroup() }
    }

    /** Leaves without the caller waiting (a screen that is going away). */
    fun leaveInBackground() { scope.launch { leave() } }

    /** Forgets the group locally (sign-out); the server drops the member after a while. */
    fun reset() { close(); applyGroup(null) }

    fun send(m: SyncClientMessage) {
        socket?.send(json.encodeToString(SyncClientMessage.serializer(), m))
    }

    /** One tick of the player loop: sends the engine's reports, returns what the player must do. */
    fun step(p: PlayerSnapshot): List<PlayerAction> {
        val s = engine.step(p, now())
        s.messages.forEach(::send)
        return s.actions
    }

    fun nextWakeMs(): Long = engine.nextWakeMs(now())

    private fun enter(g: SyncGroup) {
        endedReason = null
        applyGroup(g)
        if (socket == null) connect()
    }

    private fun applyGroup(g: SyncGroup?) {
        group = g
        engine.onState(g)
    }

    private fun connect() {
        val a = api ?: return
        retry?.cancel()
        val gen = ++generation
        socket = a.syncSocket(object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                val m = runCatching { json.decodeFromString(SyncServerMessage.serializer(), text) }.getOrNull() ?: return
                val receivedAt = now()
                scope.launch { if (gen == generation) handle(m, receivedAt) }
            }
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) { scope.launch { if (gen == generation) dropped() } }
            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) { scope.launch { if (gen == generation) dropped() } }
        })
    }

    private fun handle(m: SyncServerMessage, receivedAt: Long) {
        when (m.type) {
            "hello" -> {
                connected = true
                attempt = 0
                pinger?.cancel()
                // A few quick pings for a first clock estimate, then every 5 s.
                pinger = scope.launch {
                    var n = 0
                    while (isActive) {
                        send(engine.ping(now()))
                        delay(if (++n < 4) 400 else 5000)
                    }
                }
            }
            "pong" -> m.clientTime?.let { engine.clock.onPong(it, m.serverTime, receivedAt) }
            "state" -> {
                val g = m.group
                if (g == null) {
                    endedReason = m.reason ?: "left"
                    close()
                    applyGroup(null)
                } else applyGroup(g)
            }
            "correction" -> m.correction?.let { engine.onCorrection(it, m.serverTime) }
            "error" -> lastError = m.code
        }
    }

    private fun dropped() {
        connected = false
        pinger?.cancel()
        socket = null
        if (group == null) return
        val wait = (1000L shl attempt.coerceAtMost(5)).coerceAtMost(30_000)
        attempt++
        retry = scope.launch { delay(wait); if (group != null && socket == null) connect() }
    }

    private fun close() {
        generation++
        retry?.cancel()
        pinger?.cancel()
        socket?.close(1000, null)
        socket = null
        connected = false
    }
}
