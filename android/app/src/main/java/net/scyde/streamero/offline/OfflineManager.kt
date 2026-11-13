package net.scyde.streamero.offline

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.util.Log
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.workDataOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.ApiException
import net.scyde.streamero.api.Download
import net.scyde.streamero.api.DownloadRequest
import net.scyde.streamero.api.ProgressReport
import java.io.File
import java.util.concurrent.TimeUnit

/**
 * Offline downloads on this device: asks the server for download jobs, hands them to
 * [DownloadWorker] (WorkManager, so they survive the app closing), keeps the [OfflineStore]
 * and syncs offline progress back (on start, resume, regained connectivity and after playback).
 */
class OfflineManager(private val context: Context, private val apiProvider: suspend () -> Api?) {
    private val root = File(context.getExternalFilesDir(null) ?: context.filesDir, "offline")
    private var account: String? = OfflineAccounts.current(root)
    /**
     * The library of the signed-in account (see [OfflineAccounts]). Until the first sign-in after
     * the update, a library from before downloads were per account is shown as it is.
     */
    var store by mutableStateOf(
        if (account == null && OfflineAccounts.hasLegacy(root)) OfflineStore(root) else OfflineStore(OfflineAccounts.dir(root, account))
    )
        private set
    /** Whether the server confirmed in this run that the session belongs to [account]; nothing syncs before. */
    @Volatile private var confirmed = false
    private val binding = Mutex()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val syncing = Mutex()
    private val work get() = WorkManager.getInstance(context)

    private val _online = MutableStateFlow(true)
    /** Whether the device has a validated network (not whether the server answers). */
    val online: StateFlow<Boolean> = _online

    suspend fun api(): Api? = apiProvider()?.takeIf { it.token != null }

    /** Watches connectivity: a regained connection syncs and resumes waiting downloads. */
    fun start() {
        val cm = context.getSystemService(ConnectivityManager::class.java) ?: return
        _online.value = cm.activeNetwork?.let { cm.getNetworkCapabilities(it) }?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) == true
        runCatching {
            cm.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) {
                    val was = _online.value
                    _online.value = true
                    if (!was) syncSoon()
                }
                override fun onLost(network: Network) { _online.value = false }
            })
        }
    }

    /**
     * Switches to the downloads of [key] (an account on a server, from [OfflineAccounts.key]), or
     * hides them all when signed out. Transfers of another account stop; the first account to
     * sign in after the update takes over the downloads made before.
     */
    suspend fun bind(key: String?) = binding.withLock {
        confirmed = key != null
        if (key == account && store.root == OfflineAccounts.dir(root, key)) { if (key != null) resumePending(); return@withLock }
        work.cancelAllWorkByTag(TAG)
        val dir = OfflineAccounts.dir(root, key)
        store = withContext(Dispatchers.IO) {
            if (key != null) OfflineAccounts.migrateLegacy(root, dir)
            OfflineAccounts.setCurrent(root, key)
            OfflineStore(dir)
        }
        account = key
        if (key != null) resumePending()
    }

    // ---- Downloads ----

    /**
     * Asks the server for jobs (a film, an episode, a season, or a series' next N episodes),
     * stores an entry for each item the device does not have yet and queues the transfers.
     * Errors (no permission, parental limit) are thrown to the caller to show.
     */
    suspend fun request(api: Api, r: DownloadRequest): List<Download> {
        val jobs = api.createDownloads(r)
        add(jobs)
        return jobs
    }

    private suspend fun add(jobs: List<Download>) {
        val now = System.currentTimeMillis()
        val queued = mutableListOf<String>()
        store.update { d ->
            var entries = d.entries
            for (j in jobs) {
                val old = entries.firstOrNull { it.itemId == j.itemId }
                if (old != null && old.state != EntryState.FAILED) continue
                val e = OfflineEntry(
                    itemId = j.itemId, quality = j.quality, jobId = j.id, fileId = j.fileId.ifBlank { null },
                    state = EntryState.fromJob(j.state).let { if (it == EntryState.FAILED) EntryState.QUEUED else it },
                    transcodeProgress = j.progress, totalBytes = j.sizeBytes ?: 0, container = j.container,
                    durationMs = j.durationMs ?: j.item.durationMs, item = j.item, overview = j.overview, series = j.series,
                    addedAt = old?.addedAt ?: now, playback = old?.playback ?: LocalPlayback(j.item.positionMs, j.item.played),
                )
                entries = if (old != null) entries.map { if (it.itemId == j.itemId) e else it } else entries + e
                queued += j.itemId
            }
            d.copy(entries = entries)
        }
        queued.forEach { enqueue(it) }
    }

    /** Queues (or keeps) the transfer of one item; `replace` restarts it with fresh constraints. */
    fun enqueue(itemId: String, replace: Boolean = false) {
        val wifi = store.data.value.wifiOnly
        val req = OneTimeWorkRequestBuilder<DownloadWorker>()
            .setInputData(workDataOf(DownloadWorker.ITEM to itemId))
            .setConstraints(Constraints.Builder().setRequiredNetworkType(if (wifi) NetworkType.UNMETERED else NetworkType.CONNECTED).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
            .addTag(TAG)
            .build()
        work.enqueueUniqueWork(workName(itemId), if (replace) ExistingWorkPolicy.REPLACE else ExistingWorkPolicy.KEEP, req)
    }

    /** Starts again after a failure. */
    suspend fun retry(itemId: String) {
        store.updateEntry(itemId) { it.copy(state = EntryState.QUEUED, error = null, jobId = null) }
        enqueue(itemId, replace = true)
    }

    /** Removes downloads from the device (and their unfinished server jobs). */
    suspend fun delete(itemIds: Collection<String>) {
        val gone = itemIds.mapNotNull { store.entry(it) }
        itemIds.forEach { work.cancelUniqueWork(workName(it)) }
        store.remove(itemIds)
        val api = api() ?: return
        gone.filter { !it.done && it.jobId != null }.forEach { e -> runCatching { api.deleteDownload(e.jobId!!) } }
    }

    suspend fun deleteAll() {
        work.cancelAllWorkByTag(TAG)
        delete(store.data.value.entries.map { it.itemId })
    }

    suspend fun setWifiOnly(on: Boolean) {
        store.update { it.copy(wifiOnly = on) }
        store.data.value.entries.filter { !it.done && it.state != EntryState.FAILED }.forEach { enqueue(it.itemId, replace = true) }
    }

    /** Queues every unfinished transfer again (KEEP leaves running ones alone). */
    fun resumePending() {
        store.data.value.entries.filter { !it.done && it.state != EntryState.FAILED }.forEach { enqueue(it.itemId) }
    }

    // ---- Automatic episodes ----

    suspend fun subscribe(api: Api, seriesId: String, title: String, count: Int, quality: String) {
        // Ask once right away so permission errors reach the screen.
        val plan = AutoDownloads.plan(store.data.value.entries, AutoSubscription(seriesId, title, count, quality))
        request(api, DownloadRequest(seriesId, quality, nextEpisodes = count, skipItemIds = plan.skip.ifEmpty { null }))
        store.update { d -> d.copy(subscriptions = d.subscriptions.filter { it.seriesId != seriesId } + AutoSubscription(seriesId, title, count, quality, System.currentTimeMillis())) }
    }

    suspend fun unsubscribe(seriesId: String) {
        store.update { d -> d.copy(subscriptions = d.subscriptions.filter { it.seriesId != seriesId }) }
    }

    // ---- Playback ----

    /** Records where offline playback is (dirty until the next sync confirms it). */
    suspend fun recordPlayback(itemId: String, positionMs: Long, durationMs: Long?, persist: Boolean = true) {
        val now = System.currentTimeMillis()
        store.updateEntry(itemId, persist) { e -> e.copy(playback = OfflineSync.record(e, positionMs, durationMs, now)) }
    }

    /** Tells the server too, when it can be reached (the local record is what counts offline). */
    suspend fun reportOnline(itemId: String, positionMs: Long, durationMs: Long?) {
        if (!online.value) return
        val api = api() ?: return
        val played = store.entry(itemId)?.playback?.played == true
        runCatching { api.progress(itemId, ProgressReport(positionMs, durationMs, played.takeIf { it })) }
    }

    /** Offline playback ended: save the position, report it and sync. */
    fun finishPlayback(itemId: String, positionMs: Long, durationMs: Long?) {
        scope.launch {
            recordPlayback(itemId, positionMs, durationMs)
            reportOnline(itemId, positionMs, durationMs)
            sync()
        }
    }

    // ---- Sync ----

    fun syncSoon() { scope.launch { sync() } }

    /**
     * Sends offline progress and the list of kept items, applies the server's answer (newest wins,
     * revoked items deleted), then keeps automatic series topped up. Returns false when the server
     * could not be reached. Runs one at a time.
     */
    suspend fun sync(): Boolean {
        if (!syncing.tryLock()) return true
        try {
            if (!confirmed) return false
            val api = api() ?: return false
            val before = store.data.value.entries
            if (before.isNotEmpty()) {
                val req = OfflineSync.request(before)
                val resp = try { api.syncOffline(req) } catch (e: ApiException) {
                    Log.w(TAG, "offline sync refused: ${e.code}"); return false
                } catch (e: Exception) { return false }
                var revoked = emptySet<String>()
                store.update { d ->
                    val plan = OfflineSync.plan(d.entries, req.progress, resp)
                    revoked = plan.revoked
                    OfflineSync.apply(d, plan)
                }
                if (revoked.isNotEmpty()) delete(revoked)
            }
            for (sub in store.data.value.subscriptions) {
                val plan = AutoDownloads.plan(store.data.value.entries, sub)
                delete(plan.delete)
                runCatching { request(api, DownloadRequest(sub.seriesId, sub.quality, nextEpisodes = sub.count, skipItemIds = plan.skip.ifEmpty { null })) }
                    .onFailure { Log.w(TAG, "automatic downloads of ${sub.seriesId}: $it") }
            }
            resumePending()
            return true
        } finally {
            syncing.unlock()
        }
    }

    companion object {
        const val TAG = "offline"
        fun workName(itemId: String) = "offline-$itemId"
    }
}
