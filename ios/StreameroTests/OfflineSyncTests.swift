import XCTest
@testable import Streamero

/// Offline downloads (D-040): progress sync (newest wins), revocation, subscriptions, storage.
final class OfflineSyncTests: XCTestCase {
    private let t0 = Date(timeIntervalSince1970: 1_800_000_000)

    private func item(_ id: String, kind: String = "movie", series: String? = nil, duration: Int64 = 6_000_000) -> ItemSummary {
        ItemSummary(id: id, kind: kind, title: "Title \(id)", durationMs: duration, addedAt: "2026-11-11T10:00:00Z",
                    hasPoster: true, seriesId: series)
    }

    private func entry(_ id: String, playback: OfflinePlayback? = nil, state: OfflineState = .done,
                       series: String? = nil, subscription: String? = nil) -> OfflineEntry {
        var e = OfflineEntry(itemId: id, jobId: "job-\(id)", quality: .p1080, fileId: "f-\(id)", state: state,
                             item: item(id, kind: series == nil ? "movie" : "episode", series: series),
                             addedAt: t0, subscriptionSeriesId: subscription)
        e.playback = playback
        e.durationMs = 6_000_000
        return e
    }

    private func pb(_ pos: Int64, at date: Date, played: Bool = false, dirty: Bool) -> OfflinePlayback {
        OfflinePlayback(positionMs: pos, durationMs: 6_000_000, played: played, updatedAt: date, dirty: dirty)
    }

    private func state(_ id: String, _ pos: Int64, at date: Date, played: Bool = false) -> OfflineItemState {
        OfflineItemState(itemId: id, positionMs: pos, played: played, updatedAt: OfflineSync.formatDate(date))
    }

    // MARK: Request

    func testRequestListsEveryItemAndOnlyDirtyProgress() {
        var library = OfflineLibrary()
        library.entries = [entry("a", playback: pb(1000, at: t0, dirty: true)),
                           entry("b", playback: pb(2000, at: t0, dirty: false)),
                           entry("c")]
        let (request, sent) = OfflineSync.request(for: library)
        XCTAssertEqual(request.itemIds, ["a", "b", "c"])
        XCTAssertEqual(request.progress.map(\.itemId), ["a"])
        XCTAssertEqual(request.progress.first?.positionMs, 1000)
        XCTAssertEqual(request.progress.first.flatMap { OfflineSync.parseDate($0.updatedAt) }, t0)
        XCTAssertEqual(Set(sent.keys), ["a"])
    }

    // MARK: Newest wins

    func testNewerServerStateReplacesLocalState() {
        let entries = [entry("a", playback: pb(1000, at: t0, dirty: false))]
        let response = OfflineSyncResponse(revoked: [], items: [state("a", 5000, at: t0.addingTimeInterval(60), played: true)], applied: 0)
        let plan = OfflineSync.plan(entries: entries, sent: [:], response: response)
        XCTAssertEqual(plan.replace["a"]?.positionMs, 5000)
        XCTAssertEqual(plan.replace["a"]?.played, true)
        XCTAssertEqual(plan.replace["a"]?.dirty, false)
        XCTAssertTrue(plan.cleared.isEmpty)
    }

    func testNewerServerStateWinsOverDirtyLocalState() {
        let local = pb(1000, at: t0, dirty: true)
        let entries = [entry("a", playback: local)]
        let response = OfflineSyncResponse(revoked: [], items: [state("a", 9000, at: t0.addingTimeInterval(30))], applied: 0)
        let plan = OfflineSync.plan(entries: entries, sent: ["a": local], response: response)
        XCTAssertEqual(plan.replace["a"]?.positionMs, 9000)
        var library = OfflineLibrary(entries: entries)
        OfflineSync.applyPlayback(plan, to: &library)
        XCTAssertEqual(library.entries[0].playback?.positionMs, 9000)
        XCTAssertEqual(library.entries[0].playback?.dirty, false)
    }

    func testOlderServerStateKeepsLocalState() {
        let entries = [entry("a", playback: pb(4000, at: t0, dirty: false))]
        let response = OfflineSyncResponse(revoked: [], items: [state("a", 100, at: t0.addingTimeInterval(-600))], applied: 0)
        let plan = OfflineSync.plan(entries: entries, sent: [:], response: response)
        XCTAssertTrue(plan.replace.isEmpty)
        XCTAssertTrue(plan.cleared.isEmpty)
    }

    func testAcceptedReportClearsDirty() {
        let local = pb(3000, at: t0, dirty: true)
        let entries = [entry("a", playback: local)]
        // The server saved our report: same time, same position.
        let response = OfflineSyncResponse(revoked: [], items: [state("a", 3000, at: t0)], applied: 1)
        let plan = OfflineSync.plan(entries: entries, sent: ["a": local], response: response)
        XCTAssertEqual(plan.cleared, ["a"])
        XCTAssertTrue(plan.replace.isEmpty)
        var library = OfflineLibrary(entries: entries)
        OfflineSync.applyPlayback(plan, to: &library)
        XCTAssertEqual(library.entries[0].playback, pb(3000, at: t0, dirty: false))
    }

    func testFutureDeviceTimeClampedByServerStillClears() {
        let local = pb(3000, at: t0, dirty: true)
        let entries = [entry("a", playback: local)]
        // The device clock ran ahead; the server stored "now" (earlier) with our position.
        let response = OfflineSyncResponse(revoked: [], items: [state("a", 3000, at: t0.addingTimeInterval(-120))], applied: 1)
        let plan = OfflineSync.plan(entries: entries, sent: ["a": local], response: response)
        XCTAssertEqual(plan.cleared, ["a"])
    }

    func testRejectedReportStaysDirty() {
        let local = pb(3000, at: t0, dirty: true)
        let entries = [entry("a", playback: local)]
        let response = OfflineSyncResponse(revoked: [], items: [state("a", 100, at: t0.addingTimeInterval(-120))], applied: 0)
        let plan = OfflineSync.plan(entries: entries, sent: ["a": local], response: response)
        XCTAssertTrue(plan.cleared.isEmpty)
        XCTAssertTrue(plan.replace.isEmpty)
    }

    func testProgressRecordedDuringTheRequestStaysDirty() {
        let reported = pb(3000, at: t0, dirty: true)
        let newer = pb(8000, at: t0.addingTimeInterval(20), dirty: true)
        let entries = [entry("a", playback: newer)]
        let response = OfflineSyncResponse(revoked: [], items: [state("a", 3000, at: t0)], applied: 1)
        let plan = OfflineSync.plan(entries: entries, sent: ["a": reported], response: response)
        XCTAssertTrue(plan.cleared.isEmpty)
        XCTAssertTrue(plan.replace.isEmpty)
    }

    func testServerStateFillsAnEntryWithoutLocalPlayback() {
        let entries = [entry("a")]
        let response = OfflineSyncResponse(revoked: [], items: [state("a", 1234, at: t0)], applied: 0)
        let plan = OfflineSync.plan(entries: entries, sent: [:], response: response)
        XCTAssertEqual(plan.replace["a"]?.positionMs, 1234)
    }

    // MARK: Revocation and subscriptions

    func testRevokedItemsAreDeletedAndNotUpdated() {
        let entries = [entry("a"), entry("b")]
        let response = OfflineSyncResponse(revoked: ["b", "unknown"], items: [state("b", 10, at: t0)], applied: 0)
        let plan = OfflineSync.plan(entries: entries, sent: [:], response: response)
        XCTAssertEqual(plan.delete, ["b"])
        XCTAssertNil(plan.replace["b"])
    }

    func testWatchedSubscriptionEpisodesMakeRoom() {
        var library = OfflineLibrary()
        library.subscriptions = [OfflineSubscription(seriesId: "s1", title: "Harbour", count: 3, quality: .p720, addedAt: t0)]
        library.entries = [
            entry("e1", playback: pb(0, at: t0, played: true, dirty: false), series: "s1", subscription: "s1"),
            entry("e2", playback: pb(100, at: t0, dirty: false), series: "s1", subscription: "s1"),
            // Downloaded by hand: kept even when watched.
            entry("e3", playback: pb(0, at: t0, played: true, dirty: false), series: "s1"),
            // Still on its way.
            entry("e4", playback: pb(0, at: t0, played: true, dirty: false), state: .downloading, series: "s1", subscription: "s1"),
        ]
        XCTAssertEqual(OfflineSync.watchedSubscriptionEpisodes(library), ["e1"])
        XCTAssertEqual(OfflineSync.localEpisodes(of: "s1", in: library), ["e1", "e2", "e3", "e4"])
        library.subscriptions = []
        XCTAssertEqual(OfflineSync.watchedSubscriptionEpisodes(library), [])
    }

    func testPlaybackPastNinetyPercentIsPlayed() {
        let now = Date(timeIntervalSince1970: 1_800_000_000.123_456)
        let early = OfflineSync.playback(positionMs: 1000, durationMs: 10_000, now: now)
        XCTAssertFalse(early.played)
        XCTAssertTrue(early.dirty)
        XCTAssertTrue(OfflineSync.playback(positionMs: 9000, durationMs: 10_000, now: now).played)
        XCTAssertFalse(OfflineSync.playback(positionMs: 9000, durationMs: nil, now: now).played)
        // Whole milliseconds survive the ISO 8601 round trip.
        let roundTrip = OfflineSync.parseDate(OfflineSync.formatDate(early.updatedAt))
        XCTAssertEqual(roundTrip?.timeIntervalSince1970 ?? 0, early.updatedAt.timeIntervalSince1970, accuracy: 0.000_01)
        XCTAssertEqual(early.updatedAt.timeIntervalSince1970, 1_800_000_000.123, accuracy: 0.000_01)
    }

    // MARK: Dates

    func testParsesServerTimestamps() {
        XCTAssertEqual(OfflineSync.parseDate("2026-11-11T10:00:00Z")?.timeIntervalSince1970, 1_794_391_200)
        XCTAssertEqual(OfflineSync.formatDate(Date(timeIntervalSince1970: 1_794_391_200.25)), "2026-11-11T10:00:00.250Z")
        let base = OfflineSync.parseDate("2026-11-11T10:00:00Z")!
        let nano = OfflineSync.parseDate("2026-11-11T10:00:00.123456789Z")!
        XCTAssertEqual(nano.timeIntervalSince(base), 0.123456789, accuracy: 0.000_001)
        let offset = OfflineSync.parseDate("2026-11-11T12:00:00+02:00")!
        XCTAssertEqual(offset, base)
        XCTAssertNil(OfflineSync.parseDate("yesterday"))
    }

    // MARK: Storage

    func testStorageAccounting() {
        var movie = entry("m")
        movie.videoBytes = 1_000_000
        movie.extraBytes = 50_000
        var ep1 = entry("e1", series: "s1")
        ep1.videoBytes = 300_000
        var ep2 = entry("e2", state: .downloading, series: "s1")
        ep2.bytesWritten = 120_000
        ep2.bytesExpected = 400_000
        XCTAssertEqual(OfflineStorage.bytes(movie), 1_050_000)
        XCTAssertEqual(OfflineStorage.bytes(ep2), 120_000)
        XCTAssertEqual(OfflineStorage.totalBytes([movie, ep1, ep2]), 1_470_000)
        XCTAssertEqual(OfflineStorage.bytesByTitle([movie, ep1, ep2]), ["m": 1_050_000, "s1": 420_000])
        XCTAssertEqual(ep2.stageProgress, 0.3, accuracy: 0.0001)
        XCTAssertFalse(OfflineStorage.format(1_470_000).isEmpty)
        XCTAssertFalse(OfflineStorage.format(-5).isEmpty)
    }

    // MARK: Persistence

    func testLibraryRoundTrip() throws {
        var library = OfflineLibrary()
        var e = entry("a", playback: pb(1500, at: t0, dirty: true), series: "s1", subscription: "s1")
        e.subtitles = [OfflineSubtitle(index: 0, language: "hr", forced: false, file: "a.sub0.vtt")]
        e.posterFile = "a.still.img"
        e.series = item("s1", kind: "series")
        e.overview = "Overview"
        library.entries = [e, entry("b", state: .transcoding)]
        library.subscriptions = [OfflineSubscription(seriesId: "s1", title: "Harbour", count: 5, quality: .source, addedAt: t0)]
        let decoded = try OfflineLibrary.decode(try library.encoded())
        XCTAssertEqual(decoded, library)
        XCTAssertEqual(decoded.entry("a")?.seriesId, "s1")
        XCTAssertEqual(decoded.entry("a")?.resumeMs, 1500)
    }

    func testDownloadModelsDecodeTheServerShape() throws {
        let json = """
        {"id":"j1","itemId":"i1","fileId":"f1","quality":"1080p","state":"ready","progress":1,"sizeBytes":1234,
         "url":"/stream/dl/j1/video.mp4?exp=1&sig=x","fileName":"Movie.mp4","container":"mp4","durationMs":6000,
         "subtitles":[{"index":0,"language":"en","forced":false,"url":"/s.srt","vttUrl":"/s.vtt"}],
         "item":{"id":"i1","kind":"movie","title":"Movie","addedAt":"2026-11-11T10:00:00Z","hasPoster":true},
         "createdAt":"2026-11-11T10:00:00Z"}
        """
        let job = try JSONDecoder().decode(DownloadJob.self, from: Data(json.utf8))
        XCTAssertEqual(DownloadQuality(rawValue: job.quality), .p1080)
        XCTAssertEqual(job.subtitles.first?.vttUrl, "/s.vtt")
        let body = try JSONSerialization.jsonObject(with: MediaClient.encode(
            DownloadRequestBody(itemId: "s1", quality: DownloadQuality.source.rawValue, nextEpisodes: 3, skipItemIds: ["e1"]))) as? [String: Any]
        XCTAssertEqual(body?["quality"] as? String, "source")
        XCTAssertEqual(body?["nextEpisodes"] as? Int, 3)
        XCTAssertNil(body?["fileId"])
    }

    func testTaskDescriptionRoundTrip() {
        let d = OfflinePaths.taskDescription(serverId: "srv", itemId: "item", container: "mkv")
        let parsed = OfflinePaths.parseTaskDescription(d)
        XCTAssertEqual(parsed?.serverId, "srv")
        XCTAssertEqual(parsed?.itemId, "item")
        XCTAssertEqual(parsed?.container, "mkv")
        XCTAssertNil(OfflinePaths.parseTaskDescription("garbage"))
        XCTAssertEqual(OfflinePaths.videoName(itemId: "a/../b", container: "MP4"), "a____b.mp4")
    }

    func testDownloadPermissions() {
        let perms = UserPermissions(allLibraries: true, download: true, videoTranscoding: false, audioTranscoding: false, remux: false,
                                    liveTv: false, manageLiveTv: false, syncPlay: false, deleteMedia: false, manageCollections: false,
                                    editSubtitles: false, remoteControl: false, maxSessions: 0)
        var user = User(id: "u", username: "u", isAdmin: false, disabled: false, totpEnabled: false, permissions: perms, createdAt: "")
        XCTAssertTrue(Access.canDownload(user))
        XCTAssertFalse(Access.canDownloadTranscoded(user))
        user.isAdmin = true
        XCTAssertTrue(Access.canDownloadTranscoded(user))
        XCTAssertFalse(Access.canDownload(nil))
    }
}
