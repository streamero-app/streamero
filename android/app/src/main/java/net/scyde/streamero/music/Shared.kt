package net.scyde.streamero.music

import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * One instance made on first use and kept while [valid] says it still works. Callers that ask
 * while it is being made wait for that same one, so two quick taps never connect twice; a
 * failed attempt is simply tried again by the next caller. No Android types, so it is unit-tested.
 */
class Shared<T : Any>(private val valid: (T) -> Boolean = { true }, private val create: suspend () -> T) {
    private val lock = Mutex()
    @Volatile private var value: T? = null

    /** The instance when there is a working one, without making it. */
    val current: T? get() = value?.takeIf(valid)

    suspend fun get(): T {
        current?.let { return it }
        return lock.withLock { current ?: create().also { value = it } }
    }

    /** Forgets [v] (it disconnected), so the next [get] makes a new one. */
    fun forget(v: T) { if (value === v) value = null }
}
