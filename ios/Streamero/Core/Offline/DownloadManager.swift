import Foundation
import Network
import Observation

/// What to download from a series or season.
enum DownloadScope: Hashable, Sendable {
    case item
    case season
    /// Keep the next N unwatched episodes on the device.
    case next(Int)
}

extension Access {
    /// Same rule as the server: admins always, others with the download permission.
    static func canDownload(_ user: User?) -> Bool {
        guard let user else { return false }
        return user.isAdmin || user.permissions.download
    }

    /// Converted (MP4) downloads also need the transcoding permission.
    static func canDownloadTranscoded(_ user: User?) -> Bool {
        guard let user else { return false }
        return user.isAdmin || (user.permissions.download && user.permissions.videoTranscoding)
    }
}

/// Offline downloads (D-040): server jobs prepare the file, a background URLSession fetches it,
/// the offline library (OfflineLibrary, one JSON file per server) keeps what is on the device.
/// No entitlement is needed: background URLSessions survive Feather re-signing.
@MainActor
@Observable
final class DownloadManager {
    static let shared = DownloadManager()

    /// Server whose offline library is loaded.
    private(set) var serverId: String?
    private(set) var library = OfflineLibrary()
    /// Whether the network is reachable (NWPathMonitor).
    private(set) var online = true
    private(set) var syncing = false
    /// Fetch files only over Wi-Fi (applies to transfers started after the change).
    private(set) var wifiOnly: Bool

    /// The signed-in connection to the loaded server, when there is one.
    @ObservationIgnored private(set) var client: MediaClient?
    @ObservationIgnored private var session: URLSession?
    private let delegate = BackgroundDownloadDelegate()
    @ObservationIgnored private var backgroundCompletion: (() -> Void)?
    @ObservationIgnored private var pollTask: Task<Void, Never>?
    @ObservationIgnored private var monitor: NWPathMonitor?
    @ObservationIgnored private var activeTransfers: Set<String> = []
    @ObservationIgnored private var finalizing: Set<String> = []

    private static let wifiKey = "streamero.downloads.wifiOnly"

    private init() {
        wifiOnly = UserDefaults.standard.bool(forKey: Self.wifiKey)
    }

    // MARK: Lifecycle

    /// Reconnects to the background session (call at launch, before its events arrive).
    func start() {
        guard session == nil else { return }
        let config = URLSessionConfiguration.background(withIdentifier: OfflinePaths.sessionIdentifier)
        config.sessionSendsLaunchEvents = true
        config.isDiscretionary = false
        // Wi-Fi only is set per request, so the setting can change without a new session.
        config.allowsCellularAccess = true
        session = URLSession(configuration: config, delegate: delegate, delegateQueue: nil)
        monitor = Self.makeMonitor()
    }

    /// Path updates arrive on a private queue; the handler is built outside the main actor.
    nonisolated private static func makeMonitor() -> NWPathMonitor {
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { path in
            let up = path.status == .satisfied
            Task { @MainActor in DownloadManager.shared.networkChanged(up) }
        }
        monitor.start(queue: DispatchQueue(label: "net.scyde.streamero.network"))
        return monitor
    }

    /// Loads the offline library of a server; `client` is nil when nobody is signed in.
    func activate(server: SavedServer, client: MediaClient?) {
        start()
        if serverId != server.id {
            serverId = server.id
            library = OfflinePaths.load(serverId: server.id)
            activeTransfers = []
        }
        self.client = client
        Task {
            await reconcileTransfers()
            await sync()
        }
    }

    func appBecameActive() {
        guard serverId != nil else { return }
        Task {
            await reconcileTransfers()
            await sync()
        }
    }

    private func networkChanged(_ up: Bool) {
        let wasOnline = online
        online = up
        if up && !wasOnline { Task { await sync() } }
    }

    func setWifiOnly(_ value: Bool) {
        wifiOnly = value
        UserDefaults.standard.set(value, forKey: Self.wifiKey)
    }

    /// The system relaunched the app for finished transfers; called back once they are handled.
    func handleBackgroundEvents(identifier: String, completion: @escaping () -> Void) {
        guard identifier == OfflinePaths.sessionIdentifier else {
            completion()
            return
        }
        start()
        backgroundCompletion = completion
    }

    fileprivate func backgroundEventsFinished() {
        let completion = backgroundCompletion
        backgroundCompletion = nil
        completion?()
    }

    // MARK: Files

    var folder: URL? { serverId.map { OfflinePaths.folder(serverId: $0) } }

    func localURL(_ name: String?) -> URL? {
        guard let name, !name.isEmpty, let folder else { return nil }
        return folder.appending(path: name)
    }

    func fileURL(for entry: OfflineEntry) -> URL? { localURL(entry.fileName) }

    func entry(_ itemId: String) -> OfflineEntry? { library.entry(itemId) }

    var totalBytes: Int64 { OfflineStorage.totalBytes(library.entries) }

    private func save() {
        guard let serverId else { return }
        OfflinePaths.save(library, serverId: serverId)
    }

    /// Changes a server's library: the loaded one in memory, any other on disk (background
    /// transfers of a server that is not shown right now).
    private func mutate(serverId: String, _ change: (inout OfflineLibrary) -> Void) {
        if serverId == self.serverId {
            change(&library)
            save()
        } else {
            var other = OfflinePaths.load(serverId: serverId)
            change(&other)
            OfflinePaths.save(other, serverId: serverId)
        }
    }

    // MARK: Requests

    /// Asks the server for a movie, an episode, a season or a series (next N episodes).
    func download(itemId: String, quality: DownloadQuality, fileId: String? = nil, audioIndex: Int? = nil,
                  nextEpisodes: Int? = nil, skip: [String] = [], subscription: String? = nil) async throws {
        guard let client, let serverId else { throw APIError(status: 0, code: "offline") }
        let body = DownloadRequestBody(itemId: itemId, quality: quality.rawValue, fileId: fileId,
                                       audioIndex: quality.isTranscoded ? audioIndex : nil,
                                       nextEpisodes: nextEpisodes, skipItemIds: skip.isEmpty ? nil : Array(skip.prefix(500)))
        let jobs = try await client.createDownloads(body)
        guard self.serverId == serverId else { return }
        for job in jobs { upsert(job, quality: quality, subscription: subscription) }
        save()
        ensurePolling()
    }

    /// Downloads a season, leaving out the episodes already here.
    func downloadSeason(_ seasonId: String, seriesId: String?, quality: DownloadQuality) async throws {
        let skip = seriesId.map { OfflineSync.localEpisodes(of: $0, in: library) } ?? []
        try await download(itemId: seasonId, quality: quality, skip: skip)
    }

    /// Keeps the next `count` episodes of a series on the device, from now on at every sync.
    func subscribe(seriesId: String, title: String, count: Int, quality: DownloadQuality) async throws {
        let sub = OfflineSubscription(seriesId: seriesId, title: title, count: max(1, min(50, count)), quality: quality, addedAt: Date())
        library.subscriptions.removeAll { $0.seriesId == seriesId }
        library.subscriptions.append(sub)
        save()
        try await run(sub)
    }

    /// Stops a subscription; episodes already downloaded stay.
    func unsubscribe(_ seriesId: String) {
        library.subscriptions.removeAll { $0.seriesId == seriesId }
        for i in library.entries.indices where library.entries[i].subscriptionSeriesId == seriesId {
            library.entries[i].subscriptionSeriesId = nil
        }
        save()
    }

    func subscription(_ seriesId: String?) -> OfflineSubscription? {
        guard let seriesId else { return nil }
        return library.subscriptions.first { $0.seriesId == seriesId }
    }

    private func run(_ sub: OfflineSubscription) async throws {
        try await download(itemId: sub.seriesId, quality: sub.quality, nextEpisodes: sub.count,
                           skip: OfflineSync.localEpisodes(of: sub.seriesId, in: library), subscription: sub.seriesId)
    }

    /// Adds or refreshes the entry of a server job.
    private func upsert(_ job: DownloadJob, quality: DownloadQuality, subscription: String?) {
        if let i = library.index(of: job.itemId) {
            if library.entries[i].state == .done { return }
            library.entries[i].jobId = job.id
            library.entries[i].quality = DownloadQuality(rawValue: job.quality) ?? quality
            if library.entries[i].subscriptionSeriesId == nil { library.entries[i].subscriptionSeriesId = subscription }
            apply(job, at: i)
        } else {
            let entry = OfflineEntry(itemId: job.itemId, jobId: job.id, quality: DownloadQuality(rawValue: job.quality) ?? quality,
                                     fileId: job.fileId, state: .waiting, item: job.item, overview: job.overview,
                                     series: job.series, addedAt: Date(), subscriptionSeriesId: subscription)
            library.entries.append(entry)
            apply(job, at: library.entries.count - 1)
        }
    }

    /// Moves an entry along with its server job.
    private func apply(_ job: DownloadJob, at i: Int) {
        guard library.entries.indices.contains(i), library.entries[i].state != .done else { return }
        library.entries[i].item = job.item
        library.entries[i].overview = job.overview ?? library.entries[i].overview
        library.entries[i].series = job.series ?? library.entries[i].series
        library.entries[i].fileId = job.fileId
        library.entries[i].durationMs = job.durationMs ?? job.item.durationMs
        library.entries[i].container = job.container ?? library.entries[i].container
        switch job.state {
        case "queued":
            library.entries[i].state = .waiting
            library.entries[i].error = nil
        case "transcoding":
            library.entries[i].state = .transcoding
            library.entries[i].transcodeProgress = job.progress
            library.entries[i].error = nil
        case "failed":
            library.entries[i].state = .failed
            library.entries[i].error = job.error
        case "ready":
            startTransfer(job, at: i)
        default:
            break
        }
    }

    private func startTransfer(_ job: DownloadJob, at i: Int) {
        let itemId = job.itemId
        if activeTransfers.contains(itemId) && library.entries[i].state == .downloading { return }
        guard let serverId, let session, let client, let path = job.url, let url = client.absolute(path) else {
            library.entries[i].state = .failed
            return
        }
        let container = job.container ?? job.fileName.map { ($0 as NSString).pathExtension } ?? "mp4"
        var request = URLRequest(url: url)
        request.allowsCellularAccess = !wifiOnly
        let task = session.downloadTask(with: request)
        task.taskDescription = OfflinePaths.taskDescription(serverId: serverId, itemId: itemId, container: container)
        if let size = job.sizeBytes, size > 0 { task.countOfBytesClientExpectsToReceive = size }
        library.entries[i].state = .downloading
        library.entries[i].container = container
        library.entries[i].transcodeProgress = 1
        library.entries[i].bytesWritten = 0
        library.entries[i].bytesExpected = job.sizeBytes ?? 0
        library.entries[i].error = nil
        activeTransfers.insert(itemId)
        task.resume()
    }

    // MARK: Server jobs

    private func ensurePolling() {
        guard pollTask == nil else { return }
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let self else { return }
                let more = await self.pollOnce()
                if !more { break }
                try? await Task.sleep(for: .seconds(4))
            }
            self?.pollTask = nil
        }
    }

    /// Checks the jobs the server is still preparing; true while any remain.
    private func pollOnce() async -> Bool {
        guard let client, let serverId, online else { return false }
        let pending = library.entries.filter { $0.state == .waiting || $0.state == .transcoding }
        if pending.isEmpty { return false }
        for entry in pending {
            do {
                guard let jobId = entry.jobId else {
                    try await recreate(entry)
                    continue
                }
                let job = try await client.download(jobId)
                guard self.serverId == serverId, let i = library.index(of: entry.itemId) else { continue }
                apply(job, at: i)
            } catch let error as APIError where error.status == 404 {
                // The job is gone (cleaned up on the server): ask again.
                try? await recreate(entry)
            } catch let error as APIError where error.status == 403 {
                if let i = library.index(of: entry.itemId) {
                    library.entries[i].state = .failed
                    library.entries[i].error = error.code
                }
            } catch {
                // Network trouble: try again on the next round.
            }
        }
        if self.serverId == serverId { save() }
        return library.entries.contains { $0.state == .waiting || $0.state == .transcoding }
    }

    private func recreate(_ entry: OfflineEntry) async throws {
        guard let client, let serverId else { return }
        let jobs = try await client.createDownloads(DownloadRequestBody(itemId: entry.itemId, quality: entry.quality.rawValue,
                                                                        fileId: entry.fileId))
        guard self.serverId == serverId, let job = jobs.first(where: { $0.itemId == entry.itemId }),
              let i = library.index(of: entry.itemId) else { return }
        library.entries[i].jobId = job.id
        apply(job, at: i)
    }

    /// After a relaunch: transfers the system still runs stay, lost ones start again.
    private func reconcileTransfers() async {
        guard let session, let serverId else { return }
        let tasks = await session.allTasks
        guard self.serverId == serverId else { return }
        var live: Set<String> = []
        for task in tasks {
            guard let info = OfflinePaths.parseTaskDescription(task.taskDescription) else {
                task.cancel()
                continue
            }
            guard info.serverId == serverId else { continue }
            if library.entry(info.itemId)?.state == .downloading {
                live.insert(info.itemId)
            } else if library.entry(info.itemId) == nil {
                task.cancel()
            }
        }
        activeTransfers = live
        var changed = false
        for i in library.entries.indices where library.entries[i].state == .downloading && !live.contains(library.entries[i].itemId) {
            // The transfer was lost (app removed from memory): fetch a fresh link and start again.
            library.entries[i].state = .waiting
            changed = true
        }
        if changed { save() }
        ensurePolling()
    }

    // MARK: Transfer events (from the background session delegate)

    fileprivate func transferProgress(serverId: String, itemId: String, written: Int64, expected: Int64) {
        guard serverId == self.serverId, let i = library.index(of: itemId), library.entries[i].state == .downloading else { return }
        let old = library.entries[i].bytesWritten
        let total = expected > 0 ? expected : library.entries[i].bytesExpected
        // Redraw about every half percent, not for every packet.
        let step = max(Int64(512 * 1024), total / 200)
        if written - old >= step || written == total {
            library.entries[i].bytesWritten = written
            if expected > 0 { library.entries[i].bytesExpected = expected }
        }
    }

    fileprivate func transferFinished(serverId: String, itemId: String, fileName: String, bytes: Int64) {
        if serverId == self.serverId { activeTransfers.remove(itemId) }
        var known = true
        mutate(serverId: serverId) { lib in
            guard let i = lib.index(of: itemId) else {
                known = false
                return
            }
            lib.entries[i].state = .done
            lib.entries[i].fileName = fileName
            lib.entries[i].videoBytes = bytes
            lib.entries[i].bytesWritten = bytes
            lib.entries[i].bytesExpected = bytes
            lib.entries[i].error = nil
        }
        if !known {
            // Deleted while it was on its way.
            OfflinePaths.remove(fileName, serverId: serverId)
            return
        }
        if serverId == self.serverId {
            Task { await finalize(itemId) }
        }
    }

    fileprivate func transferFailed(serverId: String, itemId: String, message: String) {
        if serverId == self.serverId { activeTransfers.remove(itemId) }
        mutate(serverId: serverId) { lib in
            guard let i = lib.index(of: itemId), lib.entries[i].state == .downloading else { return }
            lib.entries[i].state = .failed
            lib.entries[i].error = message
        }
    }

    /// Saves subtitles and artwork next to a finished file, then frees the server's copy.
    private func finalize(_ itemId: String) async {
        guard let client, let serverId, !finalizing.contains(itemId),
              let entry = library.entry(itemId), entry.state == .done else { return }
        finalizing.insert(itemId)
        defer { finalizing.remove(itemId) }
        let folder = OfflinePaths.folder(serverId: serverId)

        if !entry.extrasSaved {
            var subtitles: [OfflineSubtitle] = []
            var extra: Int64 = 0
            var complete = true
            if let jobId = entry.jobId {
                do {
                    let job = try await client.download(jobId)
                    for sub in job.subtitles {
                        guard let url = client.absolute(sub.vttUrl), let data = try? await client.signedData(url) else { continue }
                        let name = OfflinePaths.subtitleName(itemId: itemId, index: sub.index)
                        if (try? data.write(to: folder.appending(path: name), options: .atomic)) != nil {
                            subtitles.append(OfflineSubtitle(index: sub.index, language: sub.language, forced: sub.forced, file: name))
                            extra += Int64(data.count)
                        }
                    }
                } catch let error as APIError where error.status == 404 {
                    // No job any more: the video stays, without subtitles.
                } catch {
                    complete = false
                }
            }
            guard complete else { return }

            let item = entry.item
            var poster: String?
            var backdrop: String?
            var seriesPoster: String?
            if item.hasPoster {
                let type = item.kind == "episode" ? "still" : "poster"
                if let saved = await saveArtwork(client: client, folder: folder, id: item.id, type: type, name: OfflinePaths.artworkName(itemId: itemId, type: type)) {
                    poster = saved.name
                    extra += saved.bytes
                }
            }
            if item.hasBackdrop == true,
               let saved = await saveArtwork(client: client, folder: folder, id: item.id, type: "backdrop", name: OfflinePaths.artworkName(itemId: itemId, type: "backdrop")) {
                backdrop = saved.name
                extra += saved.bytes
            }
            if let series = entry.series, series.hasPoster {
                let name = OfflinePaths.artworkName(itemId: "series-" + series.id, type: "poster")
                if FileManager.default.fileExists(atPath: folder.appending(path: name).path) {
                    seriesPoster = name
                } else if let saved = await saveArtwork(client: client, folder: folder, id: series.id, type: "poster", name: name) {
                    seriesPoster = saved.name
                }
            }

            guard self.serverId == serverId, let i = library.index(of: itemId) else { return }
            library.entries[i].subtitles = subtitles.sorted { $0.index < $1.index }
            library.entries[i].posterFile = poster
            library.entries[i].backdropFile = backdrop
            library.entries[i].seriesPosterFile = seriesPoster
            library.entries[i].extraBytes = extra
            library.entries[i].extrasSaved = true
            save()
        }

        guard let jobId = library.entry(itemId)?.jobId else { return }
        do {
            try await client.deleteDownload(jobId)
        } catch let error as APIError where error.status == 404 {
            // Already gone.
        } catch {
            return
        }
        guard self.serverId == serverId, let i = library.index(of: itemId) else { return }
        library.entries[i].jobId = nil
        save()
    }

    private func saveArtwork(client: MediaClient, folder: URL, id: String, type: String, name: String) async -> (name: String, bytes: Int64)? {
        guard let url = client.imageURL(item: id, type: type),
              let data = try? await client.authorizedData(url), !data.isEmpty,
              (try? data.write(to: folder.appending(path: name), options: .atomic)) != nil else { return nil }
        return (name, Int64(data.count))
    }

    // MARK: Delete and retry

    func delete(_ itemId: String) {
        guard let serverId, let i = library.index(of: itemId) else { return }
        let entry = library.entries.remove(at: i)
        activeTransfers.remove(itemId)
        cancelTransfer(serverId: serverId, itemId: itemId)
        removeFiles(of: entry, serverId: serverId)
        save()
        if let jobId = entry.jobId, let client {
            Task { try? await client.deleteDownload(jobId) }
        }
    }

    /// Removes every download and subscription of this server.
    func deleteAll() {
        library.subscriptions = []
        for id in library.entries.map(\.itemId) { delete(id) }
        save()
    }

    func retry(_ itemId: String) {
        guard let i = library.index(of: itemId) else { return }
        library.entries[i].state = .waiting
        library.entries[i].error = nil
        save()
        ensurePolling()
    }

    private func cancelTransfer(serverId: String, itemId: String) {
        guard let session else { return }
        Task {
            for task in await session.allTasks {
                if let info = OfflinePaths.parseTaskDescription(task.taskDescription), info.serverId == serverId, info.itemId == itemId {
                    task.cancel()
                }
            }
        }
    }

    private func removeFiles(of entry: OfflineEntry, serverId: String) {
        OfflinePaths.remove(entry.fileName, serverId: serverId)
        OfflinePaths.remove(OfflinePaths.videoName(itemId: entry.itemId, container: entry.container), serverId: serverId)
        for sub in entry.subtitles { OfflinePaths.remove(sub.file, serverId: serverId) }
        OfflinePaths.remove(entry.posterFile, serverId: serverId)
        OfflinePaths.remove(entry.backdropFile, serverId: serverId)
        // The series poster is shared by the series' episodes.
        if let poster = entry.seriesPosterFile, !library.entries.contains(where: { $0.seriesPosterFile == poster }) {
            OfflinePaths.remove(poster, serverId: serverId)
        }
    }

    // MARK: Playback and sync

    /// Records where offline playback is (dirty until the server confirms it).
    func recordProgress(itemId: String, positionMs: Int64, durationMs: Int64?) {
        guard let i = library.index(of: itemId) else { return }
        let duration = durationMs ?? library.entries[i].durationMs ?? library.entries[i].item.durationMs
        library.entries[i].playback = OfflineSync.playback(positionMs: positionMs, durationMs: duration, now: Date())
        save()
    }

    /// Sends offline progress, drops what may no longer be kept, runs subscriptions and
    /// finishes downloads that still need their extras.
    func sync() async {
        guard let client, let serverId, !syncing else { return }
        syncing = true
        defer { syncing = false }

        if !library.entries.isEmpty {
            let (request, sent) = OfflineSync.request(for: library)
            do {
                let response = try await client.syncDownloads(request)
                guard self.serverId == serverId else { return }
                let plan = OfflineSync.plan(entries: library.entries, sent: sent, response: response)
                OfflineSync.applyPlayback(plan, to: &library)
                save()
                for id in plan.delete { delete(id) }
            } catch {
                return
            }
        }

        for id in OfflineSync.watchedSubscriptionEpisodes(library) { delete(id) }
        for sub in library.subscriptions {
            try? await run(sub)
            guard self.serverId == serverId else { return }
        }
        for entry in library.entries where entry.state == .done && (!entry.extrasSaved || entry.jobId != nil) {
            await finalize(entry.itemId)
        }
        ensurePolling()
    }
}

/// Receives the background session's events on its own queue. The finished file must be moved
/// before this method returns (the system deletes it afterwards), so that happens here; the rest
/// goes to the main actor.
final class BackgroundDownloadDelegate: NSObject, URLSessionDownloadDelegate, @unchecked Sendable {
    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {
        guard let info = OfflinePaths.parseTaskDescription(downloadTask.taskDescription) else { return }
        let serverId = info.serverId
        let itemId = info.itemId
        let status = (downloadTask.response as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            Task { @MainActor in
                DownloadManager.shared.transferFailed(serverId: serverId, itemId: itemId, message: "http_\(status)")
            }
            return
        }
        let name = OfflinePaths.videoName(itemId: itemId, container: info.container)
        let destination = OfflinePaths.folder(serverId: serverId).appending(path: name)
        do {
            if FileManager.default.fileExists(atPath: destination.path) {
                try FileManager.default.removeItem(at: destination)
            }
            try FileManager.default.moveItem(at: location, to: destination)
            let attributes = try? FileManager.default.attributesOfItem(atPath: destination.path)
            let bytes = (attributes?[.size] as? NSNumber)?.int64Value ?? 0
            Task { @MainActor in
                DownloadManager.shared.transferFinished(serverId: serverId, itemId: itemId, fileName: name, bytes: bytes)
            }
        } catch {
            let message = error.localizedDescription
            Task { @MainActor in
                DownloadManager.shared.transferFailed(serverId: serverId, itemId: itemId, message: message)
            }
        }
    }

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didWriteData bytesWritten: Int64,
                    totalBytesWritten: Int64, totalBytesExpectedToWrite: Int64) {
        guard let info = OfflinePaths.parseTaskDescription(downloadTask.taskDescription) else { return }
        let serverId = info.serverId
        let itemId = info.itemId
        Task { @MainActor in
            DownloadManager.shared.transferProgress(serverId: serverId, itemId: itemId, written: totalBytesWritten,
                                                    expected: totalBytesExpectedToWrite)
        }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: (any Error)?) {
        guard let error else { return }
        let nsError = error as NSError
        // Cancelled by the app (deleted) or by the system (app removed from memory): the
        // relaunch reconciles those.
        if nsError.domain == NSURLErrorDomain && nsError.code == NSURLErrorCancelled { return }
        guard let info = OfflinePaths.parseTaskDescription(task.taskDescription) else { return }
        let serverId = info.serverId
        let itemId = info.itemId
        let message = nsError.localizedDescription
        Task { @MainActor in
            DownloadManager.shared.transferFailed(serverId: serverId, itemId: itemId, message: message)
        }
    }

    func urlSessionDidFinishEvents(forBackgroundURLSession session: URLSession) {
        Task { @MainActor in DownloadManager.shared.backgroundEventsFinished() }
    }
}
