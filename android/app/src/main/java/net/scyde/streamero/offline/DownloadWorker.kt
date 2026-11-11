package net.scyde.streamero.offline

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.pm.ServiceInfo
import android.os.Build
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.work.CoroutineWorker
import androidx.work.ForegroundInfo
import androidx.work.WorkerParameters
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext
import net.scyde.streamero.R
import net.scyde.streamero.StreameroApp
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.ApiException
import net.scyde.streamero.api.Download
import net.scyde.streamero.api.DownloadRequest
import okhttp3.Request
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlin.coroutines.coroutineContext

/**
 * Brings one item onto the device: waits for the server's job (transcoding), downloads the
 * file with byte-range resume, then the subtitle sidecars and artwork, and finally deletes the
 * server job to free its cache. Runs in the foreground with a progress notification.
 */
class DownloadWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    private val app = context.applicationContext as StreameroApp
    private val manager get() = app.state.offline
    private val store get() = manager.store
    private val itemId = params.inputData.getString(ITEM).orEmpty()
    private val notificationId = 0x5000 + (itemId.hashCode() and 0xFFF)

    override suspend fun doWork(): Result {
        val start = store.entry(itemId) ?: return Result.success()
        if (start.done) return Result.success()
        val api = manager.api() ?: return Result.retry()
        runCatching { setForeground(foreground(start.item.title, null, preparing = true)) }
        return try {
            var job = ensureJob(api) ?: return Result.success()
            // Wait for the server to prepare the file.
            while (job.state != "ready") {
                if (job.state == "failed") return fail(job.error ?: "failed")
                store.updateEntry(itemId, persist = false) { it.copy(state = EntryState.fromJob(job.state), transcodeProgress = job.progress) }
                notify(job.item.title, job.progress, preparing = true)
                delay(POLL_MS)
                job = refresh(api, job) ?: return Result.success()
            }
            val dir = store.dirFor(itemId).apply { mkdirs() }
            val ext = (job.container ?: "mp4").filter { it.isLetterOrDigit() }.ifEmpty { "mp4" }
            val video = File(dir, "video.$ext")
            store.updateEntry(itemId) { it.copy(state = EntryState.DOWNLOADING, totalBytes = job.sizeBytes ?: it.totalBytes, container = ext, error = null) }
            job = fetchVideo(api, job, video)
            if (store.entry(itemId) == null) { dir.deleteRecursively(); return Result.success() }

            val subs = job.subtitles.mapNotNull { s ->
                val name = "sub_${s.index}.srt"
                if (fetch(api, api.resolve(s.url), File(dir, name))) OfflineSubtitle(s.index, s.language, s.forced, store.relative(itemId, name)) else null
            }
            val item = job.item
            val poster = if (item.hasPoster) image(api, item.id, if (item.kind == "episode") "still" else "poster", "poster.jpg") else null
            val backdrop = if (item.hasBackdrop) image(api, item.id, "backdrop", "backdrop.jpg") else null
            val seriesPoster = job.series?.takeIf { it.hasPoster }?.let { image(api, it.id, "poster", "series.jpg") }

            val done = store.updateEntry(itemId) {
                it.copy(state = EntryState.DONE, file = store.relative(itemId, video.name), subtitles = subs, poster = poster, backdrop = backdrop,
                    seriesPoster = seriesPoster, downloadedBytes = video.length(), totalBytes = video.length(), storedBytes = store.bytesOnDisk(itemId),
                    durationMs = job.durationMs ?: it.durationMs, overview = job.overview ?: it.overview, series = job.series ?: it.series, item = job.item, error = null)
            }
            if (done == null) { dir.deleteRecursively(); return Result.success() }
            // The file is here: the server's copy is no longer needed.
            runCatching { api.deleteDownload(job.id) }
            store.updateEntry(itemId) { it.copy(jobId = null) }
            notifyDone(item.title)
            Result.success()
        } catch (e: ApiException) {
            if (e.status == 403 || e.status == 400) fail(e.code) else retryLater(e)
        } catch (e: IOException) {
            retryLater(e)
        } catch (e: kotlinx.coroutines.CancellationException) {
            throw e // stopped (constraints lost, removed): the .part file is resumed next time
        } catch (e: Exception) {
            Log.w(OfflineManager.TAG, "download of $itemId failed", e)
            fail("generic")
        }
    }

    private suspend fun retryLater(e: Exception): Result {
        Log.w(OfflineManager.TAG, "download of $itemId paused: $e")
        store.updateEntry(itemId) { it.copy(error = null) }
        return if (runAttemptCount < 20) Result.retry() else fail("network")
    }

    private suspend fun fail(reason: String): Result {
        store.updateEntry(itemId) { it.copy(state = EntryState.FAILED, error = reason) }
        return Result.failure()
    }

    /** The server job of this entry, created again when it is gone (404) or never existed. */
    private suspend fun ensureJob(api: Api): Download? {
        val e = store.entry(itemId) ?: return null
        e.jobId?.let { id ->
            try { return api.download(id) } catch (x: ApiException) { if (x.status != 404) throw x }
        }
        val jobs = api.createDownloads(DownloadRequest(itemId, e.quality, fileId = e.fileId))
        val job = jobs.firstOrNull { it.itemId == itemId } ?: jobs.firstOrNull() ?: throw ApiException(404, "not_found", "no job")
        store.updateEntry(itemId) { it.copy(jobId = job.id) }
        return job
    }

    private suspend fun refresh(api: Api, job: Download): Download? = try {
        api.download(job.id)
    } catch (x: ApiException) {
        if (x.status == 404) { store.updateEntry(itemId) { it.copy(jobId = null) }; ensureJob(api) } else throw x
    }

    /** Downloads the video into `<name>.part` (resuming what is there) and renames it when complete. */
    private suspend fun fetchVideo(api: Api, first: Download, target: File): Download {
        var job = first
        val part = File(target.path + ".part")
        val client = api.http.newBuilder().readTimeout(60, TimeUnit.SECONDS).build()
        repeat(3) { attempt ->
            val url = job.url ?: throw IOException("job without url")
            val have = if (part.exists()) part.length() else 0L
            val req = Request.Builder().url(api.resolve(url)).apply { if (have > 0) header("Range", "bytes=$have-") }.build()
            val outcome = withContext(Dispatchers.IO) {
                client.newCall(req).execute().use { resp ->
                    when {
                        resp.code == 416 && have > 0 && (job.sizeBytes == null || have >= job.sizeBytes!!) -> true
                        // Signed links expire: fetch the job again for a fresh one.
                        resp.code == 401 || resp.code == 403 || resp.code == 410 -> false
                        !resp.isSuccessful -> throw IOException("HTTP ${resp.code}")
                        else -> {
                            val body = resp.body ?: throw IOException("empty body")
                            val append = resp.code == 206 && have > 0
                            val total = if (append) have + body.contentLength() else body.contentLength().takeIf { it >= 0 } ?: (job.sizeBytes ?: 0)
                            var done = if (append) have else 0L
                            var last = 0L
                            FileOutputStream(part, append).use { out ->
                                body.byteStream().use { input ->
                                    val buf = ByteArray(64 * 1024)
                                    while (true) {
                                        coroutineContext.ensureActive()
                                        val n = input.read(buf)
                                        if (n < 0) break
                                        out.write(buf, 0, n)
                                        done += n
                                        val now = System.currentTimeMillis()
                                        if (now - last > 750) {
                                            last = now
                                            store.updateEntry(itemId, persist = false) { it.copy(downloadedBytes = done, totalBytes = total) }
                                            notify(job.item.title, if (total > 0) done.toDouble() / total else null, preparing = false)
                                        }
                                    }
                                }
                            }
                            if (total > 0 && done < total) throw IOException("short read $done/$total")
                            true
                        }
                    }
                }
            }
            if (outcome) {
                if (target.exists()) target.delete()
                if (!part.renameTo(target)) throw IOException("rename failed")
                return job
            }
            if (attempt < 2) job = refresh(api, job) ?: throw IOException("job gone")
        }
        throw IOException("download link refused")
    }

    /** A small file (subtitle, image); false when it could not be fetched. */
    private suspend fun fetch(api: Api, url: String, to: File): Boolean = withContext(Dispatchers.IO) {
        runCatching {
            api.http.newCall(Request.Builder().url(url).build()).execute().use { resp ->
                if (!resp.isSuccessful) return@use false
                val tmp = File(to.path + ".tmp")
                resp.body!!.byteStream().use { input -> FileOutputStream(tmp).use { input.copyTo(it) } }
                tmp.renameTo(to)
            }
        }.getOrDefault(false)
    }

    private suspend fun image(api: Api, id: String, kind: String, name: String): String? =
        if (fetch(api, api.imageUrl(id, kind), File(store.dirFor(itemId), name))) store.relative(itemId, name) else null

    // ---- Notification ----

    private fun channel() {
        val nm = applicationContext.getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= 26 && nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(NotificationChannel(CHANNEL, applicationContext.getString(R.string.download_channel), NotificationManager.IMPORTANCE_LOW))
        }
    }

    private fun foreground(title: String, progress: Double?, preparing: Boolean): ForegroundInfo {
        channel()
        val text = applicationContext.getString(if (preparing) R.string.download_notification_preparing else R.string.download_notification_downloading, title)
        val n = NotificationCompat.Builder(applicationContext, CHANNEL)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setContentTitle(text)
            .setOnlyAlertOnce(true)
            .setOngoing(true)
            .setSilent(true)
            .setProgress(100, ((progress ?: 0.0) * 100).toInt(), progress == null)
            .build()
        return if (Build.VERSION.SDK_INT >= 29) ForegroundInfo(notificationId, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC) else ForegroundInfo(notificationId, n)
    }

    private suspend fun notify(title: String, progress: Double?, preparing: Boolean) {
        runCatching { setForeground(foreground(title, progress, preparing)) }
    }

    private fun notifyDone(title: String) {
        channel()
        val n = NotificationCompat.Builder(applicationContext, CHANNEL)
            .setSmallIcon(android.R.drawable.stat_sys_download_done)
            .setContentTitle(applicationContext.getString(R.string.download_notification_done, title))
            .setAutoCancel(true)
            .build()
        runCatching { applicationContext.getSystemService(NotificationManager::class.java).notify(notificationId + 0x1000, n) }
    }

    override suspend fun getForegroundInfo(): ForegroundInfo =
        foreground(store.entry(itemId)?.item?.title.orEmpty(), null, preparing = true)

    companion object {
        const val ITEM = "itemId"
        const val CHANNEL = "downloads"
        private const val POLL_MS = 3000L
    }
}
