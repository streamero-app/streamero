package net.scyde.streamero.sync

import net.scyde.streamero.api.SyncClientMessage
import net.scyde.streamero.api.SyncCorrection
import net.scyde.streamero.api.SyncGroup
import net.scyde.streamero.api.SyncQueueEntry
import kotlin.math.abs
import kotlin.math.max

// The client side of watch together (D-036), free of Android so it can be unit tested.
// The server owns the group state and judges drift; the engine turns that state into
// what the local player should do, and the player's own state into reports.

/**
 * Estimates the server's clock from ping/pong round trips: offset = serverTime −
 * (sent + received) / 2, taking the sample with the smallest round trip among the
 * recent ones (the least delayed answer is the most accurate).
 */
class ClockSync(private val window: Int = 8) {
    private val samples = ArrayDeque<Pair<Long, Long>>() // round trip, offset
    var offsetMs: Long = 0
        private set
    val hasSample: Boolean get() = samples.isNotEmpty()

    fun onPong(clientTime: Long, serverTime: Long, receivedAt: Long) {
        val rtt = receivedAt - clientTime
        if (rtt < 0) return
        samples.addLast(rtt to serverTime - (clientTime + receivedAt) / 2)
        while (samples.size > window) samples.removeFirst()
        offsetMs = samples.minBy { it.first }.second
    }

    fun serverNow(localNow: Long): Long = localNow + offsetMs
}

/** The group's shared position at server time [serverNow]. */
fun sharedPosition(g: SyncGroup, serverNow: Long): Long =
    if (g.state == PLAYING) g.positionMs + (max(0L, serverNow - g.anchorAt) * g.speed).toLong() else g.positionMs

/** What the local player is doing right now. Positions are absolute media positions. */
data class PlayerSnapshot(
    /** The queue entry the player has loaded or is loading (null = nothing yet). */
    val entryId: String?,
    /** The entry finished loading since the last (re)load. */
    val prepared: Boolean,
    val positionMs: Long,
    /** Wants to play (play-when-ready), even while it buffers. */
    val playing: Boolean,
    val buffering: Boolean,
    val ended: Boolean = false,
    val speed: Double = 1.0,
)

/** What the engine asks the player to do. */
sealed interface PlayerAction {
    data class Load(val entry: SyncQueueEntry, val startMs: Long) : PlayerAction
    data class Seek(val positionMs: Long) : PlayerAction
    data object Play : PlayerAction
    data object Pause : PlayerAction
    data class Speed(val speed: Double) : PlayerAction
}

data class SyncStep(val actions: List<PlayerAction>, val messages: List<SyncClientMessage>)

const val PLAYING = "playing"

class SyncEngine(val clock: ClockSync = ClockSync()) {
    companion object {
        /** A playing player further than this from the shared position seeks (smaller drift is the server's job). */
        const val PLAYING_TOLERANCE_MS = 1000L
        /** A stopped player is placed this precisely before it says it is ready. */
        const val STOPPED_TOLERANCE_MS = 250L
        /** A stall this long while the group plays is reported as buffering. */
        const val STALL_MS = 1000L
        const val POSITION_EVERY_MS = 1000L
        const val TICK_MS = 200L
    }

    var group: SyncGroup? = null
        private set

    private var alignedVersion = -1L
    private var alignedEntry: String? = null
    private var alignedPrepared = false
    private var loadedFor: Pair<String, Long>? = null // entry, version of the last Load
    /** Server time at which a synchronised start happens. */
    private var startAt: Long? = null
    private var readySent = -1L // group version a ready report was sent for
    private var bufferingSent = -1L
    private var stallSince: Long? = null
    private var lastPosition: Long? = null
    /** Local time when a rate correction ends. */
    private var rateUntil: Long? = null
    private var correction: Pair<SyncCorrection, Long>? = null
    private var nextSentFor: String? = null

    fun onState(g: SyncGroup?) {
        group = g
        if (g == null) {
            alignedVersion = -1; alignedEntry = null; alignedPrepared = false; loadedFor = null
            startAt = null; rateUntil = null; correction = null; stallSince = null
        }
    }

    /** A drift correction from the server, [serverTime] being when it was sent. */
    fun onCorrection(c: SyncCorrection, serverTime: Long) { correction = c to serverTime }

    /** How long the player loop may sleep before the next [step]. */
    fun nextWakeMs(localNow: Long): Long {
        val at = startAt ?: return TICK_MS
        return (at - clock.serverNow(localNow)).coerceIn(0, TICK_MS)
    }

    /** Decides what the player does now and what to tell the server. Called every tick and after every message. */
    fun step(p: PlayerSnapshot, localNow: Long): SyncStep {
        val g = group ?: return SyncStep(emptyList(), emptyList())
        val now = clock.serverNow(localNow)
        val actions = mutableListOf<PlayerAction>()
        val out = mutableListOf<SyncClientMessage>()
        val entry = g.current
        if (entry == null) {
            if (p.playing) actions += PlayerAction.Pause
            startAt = null
            return SyncStep(actions, out)
        }
        val playingNow = g.state == PLAYING && g.anchorAt <= now

        // 1. The right entry: open it at the shared position.
        if (p.entryId != entry.id) {
            if (loadedFor != entry.id to g.version) {
                loadedFor = entry.id to g.version
                alignedPrepared = false
                startAt = null
                actions += PlayerAction.Load(entry, max(0L, sharedPosition(g, now)))
            }
            return SyncStep(actions, out)
        }
        if (!p.prepared) {
            alignedPrepared = false
            return SyncStep(actions, out)
        }

        // 2. Follow a new state (or a freshly loaded entry): position, play/pause, speed.
        if (g.version != alignedVersion || entry.id != alignedEntry || !alignedPrepared) {
            alignedVersion = g.version; alignedEntry = entry.id; alignedPrepared = true
            align(g, p, now, actions)
        }

        // 3. A synchronised start that is due.
        startAt?.let { at ->
            if (now >= at) {
                startAt = null
                val target = sharedPosition(g, now)
                if (abs(p.positionMs - target) > PLAYING_TOLERANCE_MS) actions += PlayerAction.Seek(target)
                actions += PlayerAction.Play
            }
        }

        // 4. Drift correction from the server.
        correction?.let { (c, sentAt) ->
            correction = null
            if (playingNow) when (c.mode) {
                "rate" -> if (c.rate != null && c.rate > 0) {
                    actions += PlayerAction.Speed(c.rate)
                    rateUntil = localNow + (c.durationMs ?: 0L)
                }
                "seek" -> c.positionMs?.let { pos ->
                    actions += PlayerAction.Seek(pos + (max(0L, now - sentAt) * g.speed).toLong())
                }
            }
        }
        rateUntil?.let { if (localNow >= it) { rateUntil = null; actions += PlayerAction.Speed(g.speed) } }

        val self = g.self
        val moving = actions.any { it is PlayerAction.Seek || it is PlayerAction.Load }

        // 5. Ready: loaded and placed at the shared position.
        if (self != null && !self.ready && readySent != g.version && !p.buffering && !moving) {
            val target = if (playingNow) sharedPosition(g, now) else g.positionMs
            val tolerance = if (playingNow) PLAYING_TOLERANCE_MS else STOPPED_TOLERANCE_MS
            if (abs(p.positionMs - target) <= tolerance) {
                readySent = g.version
                out += SyncClientMessage("ready", entryId = entry.id, positionMs = p.positionMs, at = now)
            }
        }

        // 6. Buffering: only a ready player that stalls while the group plays.
        if (self?.ready == true && playingNow && p.buffering && !moving) {
            val since = stallSince ?: localNow.also { stallSince = it }
            if (localNow - since >= STALL_MS && bufferingSent != g.version) {
                bufferingSent = g.version
                out += SyncClientMessage("buffering", entryId = entry.id, positionMs = p.positionMs, at = now)
            }
        } else stallSince = null

        // 7. Position about once a second while playing, for the server's drift check.
        if (self?.ready == true && playingNow && startAt == null && p.playing && !p.buffering && !moving &&
            localNow - (lastPosition ?: Long.MIN_VALUE / 2) >= POSITION_EVERY_MS) {
            lastPosition = localNow
            out += SyncClientMessage("position", entryId = entry.id, positionMs = p.positionMs, at = now, playing = true)
        }

        // 8. The end of the entry moves the group on (the server skips once for everybody).
        if (p.ended && g.state == PLAYING && nextSentFor != entry.id) {
            nextSentFor = entry.id
            out += SyncClientMessage("next", entryId = entry.id)
        }
        return SyncStep(actions, out)
    }

    private fun align(g: SyncGroup, p: PlayerSnapshot, now: Long, actions: MutableList<PlayerAction>) {
        if (rateUntil == null && abs(p.speed - g.speed) > 0.001) actions += PlayerAction.Speed(g.speed)
        if (g.state == PLAYING) {
            if (g.anchorAt > now) {
                // Start together: wait at positionMs until anchorAt.
                startAt = g.anchorAt
                if (p.playing) actions += PlayerAction.Pause
                if (abs(p.positionMs - g.positionMs) > STOPPED_TOLERANCE_MS) actions += PlayerAction.Seek(g.positionMs)
            } else {
                startAt = null
                val target = sharedPosition(g, now)
                if (abs(p.positionMs - target) > PLAYING_TOLERANCE_MS) actions += PlayerAction.Seek(target)
                if (!p.playing) actions += PlayerAction.Play
            }
        } else {
            startAt = null
            rateUntil = null
            if (p.playing) actions += PlayerAction.Pause
            if (abs(p.positionMs - g.positionMs) > STOPPED_TOLERANCE_MS) actions += PlayerAction.Seek(g.positionMs)
        }
    }

    // Group commands from the local controls (the server applies them for everybody).
    fun play() = SyncClientMessage("play")
    fun pause() = SyncClientMessage("pause")
    fun seek(positionMs: Long) = SyncClientMessage("seek", positionMs = max(0L, positionMs))
    fun speed(speed: Double) = SyncClientMessage("speed", speed = speed)
    fun next() = SyncClientMessage("next", entryId = group?.current?.id)
    fun previous() = SyncClientMessage("previous", entryId = group?.current?.id)
    fun ping(localNow: Long) = SyncClientMessage("ping", clientTime = localNow)
}

private val UUID = Regex("[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")

/** The group id in an invite link (`https://server/together/<id>`) or a pasted id; null if there is none. */
fun parseGroupId(input: String): String? {
    val s = input.trim()
    val after = s.substringAfter("/together/", "")
    return (UUID.find(after) ?: UUID.findAll(s).lastOrNull())?.value?.lowercase()
}
