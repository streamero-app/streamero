package net.scyde.streamero.api

import java.io.IOException
import java.time.Instant
import java.time.OffsetDateTime
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow

/** What the Quick Connect sign-in screen shows. */
sealed interface QuickConnectState {
    /** Show [code]; it can be approved until [expiresAt]. */
    data class Code(val code: String, val expiresAt: Instant) : QuickConnectState
    data class Approved(val auth: AuthResult) : QuickConnectState
    /** Gave up: the server is unreachable or refused to start. */
    data class Failed(val error: Throwable) : QuickConnectState
    /** Several codes expired unused; the user starts again by hand. */
    data object Expired : QuickConnectState
}

/** The two calls the flow needs, so it can be tested without a server. */
interface QuickConnectApi {
    suspend fun start(): QuickConnectStart
    suspend fun poll(secret: String): AuthResult?
}

fun Api.quickConnect(device: DeviceInfo) = object : QuickConnectApi {
    override suspend fun start() = quickConnectStart(device)
    override suspend fun poll(secret: String) = quickConnectPoll(secret)
}

/**
 * Starts a Quick Connect sign-in and polls until the code is approved. An expired code is
 * replaced by a new one (up to [maxCodes]); short network failures are retried.
 */
fun quickConnectFlow(
    api: QuickConnectApi,
    now: () -> Instant = Instant::now,
    intervalMs: Long = 3_000,
    maxCodes: Int = 6,
    maxFailures: Int = 10,
): Flow<QuickConnectState> = flow {
    repeat(maxCodes) {
        val start = try { api.start() } catch (e: ApiException) { emit(QuickConnectState.Failed(e)); return@flow } catch (e: IOException) { emit(QuickConnectState.Failed(e)); return@flow }
        val expires = runCatching { OffsetDateTime.parse(start.expiresAt).toInstant() }.getOrElse { now().plusSeconds(300) }
        emit(QuickConnectState.Code(start.code, expires))
        var failures = 0
        while (now().isBefore(expires)) {
            delay(intervalMs)
            val auth = try {
                api.poll(start.secret)
            } catch (e: ApiException) {
                if (e.status == 404) break // expired or unknown: get a new code
                if (++failures >= maxFailures) { emit(QuickConnectState.Failed(e)); return@flow }
                null
            } catch (e: IOException) {
                if (++failures >= maxFailures) { emit(QuickConnectState.Failed(e)); return@flow }
                null
            }
            if (auth != null) { emit(QuickConnectState.Approved(auth)); return@flow }
        }
    }
    emit(QuickConnectState.Expired)
}
