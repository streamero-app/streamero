package net.scyde.streamero.music

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit

/**
 * Turning a list of tracks into playable items: the first one is resolved on its own so music
 * starts at once, the rest in parallel afterwards. Tracks the server cannot play are dropped
 * instead of becoming empty entries that stop the queue. No Android types, so it is unit-tested.
 */
object MusicQueue {
    /** A track whose stream could be resolved. */
    data class Resolved<T, R>(val track: T, val stream: R)

    /**
     * The first playable track at or after [start] (the one the listener picked, or the next one
     * that works), with its position; null when none of them can be played.
     */
    suspend fun <T, R : Any> first(tracks: List<T>, start: Int, resolve: suspend (T) -> R?): Pair<Int, Resolved<T, R>>? {
        for (i in start until tracks.size) {
            val r = attempt(tracks[i], resolve) ?: continue
            return i to Resolved(tracks[i], r)
        }
        return null
    }

    /** Resolves the other tracks a few at a time, keeping their order and dropping failures. */
    suspend fun <T, R : Any> rest(tracks: List<T>, skip: Int, parallel: Int = 4, resolve: suspend (T) -> R?): Pair<List<Resolved<T, R>>, List<Resolved<T, R>>> = coroutineScope {
        val gate = Semaphore(parallel)
        val all = tracks.mapIndexed { i, t -> async { if (i == skip) null else gate.withPermit { attempt(t, resolve)?.let { Resolved(t, it) } } } }.awaitAll()
        all.subList(0, skip).filterNotNull() to all.subList(skip + 1, all.size).filterNotNull()
    }

    private suspend fun <T, R : Any> attempt(t: T, resolve: suspend (T) -> R?): R? = try {
        resolve(t)
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        null
    }
}
