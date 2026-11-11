import XCTest
@testable import Streamero

final class LiveTVTests: XCTestCase {
    private func program(_ id: String, _ start: String, _ end: String, status: String? = nil) -> LiveTvProgram {
        LiveTvProgram(id: id, channelId: "c", title: id, start: start, end: end, categories: [], isNew: false, recordingStatus: status)
    }

    private func at(_ s: String) -> Date { LiveTV.date(s)! }

    func testParsesTimestampsWithAndWithoutFraction() {
        XCTAssertNotNil(LiveTV.date("2026-11-11T20:00:00Z"))
        XCTAssertNotNil(LiveTV.date("2026-11-11T20:00:00.123+01:00"))
        XCTAssertEqual(LiveTV.date("2026-11-11T20:00:00+01:00"), LiveTV.date("2026-11-11T19:00:00Z"))
        XCTAssertNil(LiveTV.date("not a date"))
    }

    func testProgressIsClampedAndProportional() {
        let p = program("a", "2026-11-11T20:00:00Z", "2026-11-11T21:00:00Z")
        XCTAssertEqual(LiveTV.progress(p, now: at("2026-11-11T20:15:00Z")), 0.25, accuracy: 0.0001)
        XCTAssertEqual(LiveTV.progress(p, now: at("2026-11-11T19:00:00Z")), 0)
        XCTAssertEqual(LiveTV.progress(p, now: at("2026-11-11T22:00:00Z")), 1)
        let broken = program("b", "2026-11-11T21:00:00Z", "2026-11-11T20:00:00Z")
        XCTAssertEqual(LiveTV.progress(broken, now: at("2026-11-11T20:30:00Z")), 0)
    }

    func testNowNextSkipsEndedAndSortsByStart() {
        let ended = program("ended", "2026-11-11T19:00:00Z", "2026-11-11T20:00:00Z")
        let later = program("later", "2026-11-11T21:00:00Z", "2026-11-11T22:00:00Z")
        let current = program("now", "2026-11-11T20:00:00Z", "2026-11-11T21:00:00Z")
        let r = LiveTV.nowNext([later, ended, current], now: at("2026-11-11T20:30:00Z"))
        XCTAssertEqual(r.now?.id, "now")
        XCTAssertEqual(r.next?.id, "later")
    }

    func testNowNextInAGap() {
        let later = program("later", "2026-11-11T21:00:00Z", "2026-11-11T22:00:00Z")
        let r = LiveTV.nowNext([later], now: at("2026-11-11T20:30:00Z"))
        XCTAssertNil(r.now)
        XCTAssertEqual(r.next?.id, "later")
        XCTAssertTrue(LiveTV.nowNext([], now: Date()).now == nil)
    }

    func testCanScheduleOnlyUnrecordedFutureOrRunning() {
        let now = at("2026-11-11T20:30:00Z")
        XCTAssertTrue(LiveTV.canSchedule(program("a", "2026-11-11T20:00:00Z", "2026-11-11T21:00:00Z"), now: now))
        XCTAssertFalse(LiveTV.canSchedule(program("b", "2026-11-11T19:00:00Z", "2026-11-11T20:00:00Z"), now: now))
        XCTAssertFalse(LiveTV.canSchedule(program("c", "2026-11-11T21:00:00Z", "2026-11-11T22:00:00Z", status: "scheduled"), now: now))
        XCTAssertTrue(LiveTV.canSchedule(program("d", "2026-11-11T21:00:00Z", "2026-11-11T22:00:00Z", status: "cancelled"), now: now))
    }

    func testFavoritesFilter() {
        let a = LiveTvChannel(id: "a", number: "1", name: "A", tunerId: "t", favorite: true)
        let b = LiveTvChannel(id: "b", number: "2", name: "B", tunerId: "t", favorite: false)
        XCTAssertEqual(LiveTV.filter([a, b], favoritesOnly: true).map(\.id), ["a"])
        XCTAssertEqual(LiveTV.filter([a, b], favoritesOnly: false).map(\.id), ["a", "b"])
    }

    private func user(admin: Bool, liveTv: Bool, manage: Bool, kid: Bool = false) -> User {
        let perms = UserPermissions(allLibraries: true, download: true, videoTranscoding: true, audioTranscoding: true, remux: true,
                                    liveTv: liveTv, manageLiveTv: manage, syncPlay: true, deleteMedia: false,
                                    manageCollections: false, editSubtitles: false, remoteControl: false, maxSessions: 0)
        let profile = kid ? Profile(id: "p", name: "Kid", color: "#000000", hasPin: false, isKid: true) : nil
        return User(id: "u", username: "u", isAdmin: admin, disabled: false, totpEnabled: false, permissions: perms,
                    currentProfile: profile, createdAt: "2026-01-01T00:00:00Z")
    }

    func testPermissions() {
        XCTAssertFalse(LiveTV.canWatch(nil))
        XCTAssertTrue(LiveTV.canWatch(user(admin: false, liveTv: true, manage: false)))
        XCTAssertFalse(LiveTV.canWatch(user(admin: false, liveTv: false, manage: false)))
        XCTAssertTrue(LiveTV.canWatch(user(admin: true, liveTv: false, manage: false)))
        XCTAssertFalse(LiveTV.canRecord(user(admin: false, liveTv: true, manage: false)))
        XCTAssertTrue(LiveTV.canRecord(user(admin: false, liveTv: true, manage: true)))
        XCTAssertTrue(LiveTV.canRecord(user(admin: true, liveTv: false, manage: false)))
        XCTAssertFalse(LiveTV.canRecord(user(admin: true, liveTv: true, manage: true, kid: true)))
    }

    func testRequestBodies() throws {
        let rec = try XCTUnwrap(JSONSerialization.jsonObject(with: MediaClient.encode(LiveTvRecordingCreate(programId: "p", channelId: nil))) as? [String: Any])
        XCTAssertEqual(rec["programId"] as? String, "p")
        XCTAssertNil(rec["channelId"], "nil optionals are omitted")
        let rule = try XCTUnwrap(JSONSerialization.jsonObject(with: MediaClient.encode(LiveTvSeriesRuleInput(programId: "p", channelId: "c", newOnly: false))) as? [String: Any])
        XCTAssertEqual(rule["newOnly"] as? Bool, false, "required false is sent")
        XCTAssertEqual(rule["channelId"] as? String, "c")
        let play = try XCTUnwrap(JSONSerialization.jsonObject(with: MediaClient.encode(LiveTvPlayRequest(device: MediaClient.device))) as? [String: Any])
        XCTAssertNotNil(play["device"])
    }

    func testFavoriteQuery() {
        let c = MediaClient(baseURL: URL(string: "https://tv.example")!, token: "t")
        XCTAssertEqual(c.url(path: "livetv/channels", query: ["favorites": "true"]).absoluteString,
                       "https://tv.example/api/v1/livetv/channels?favorites=true")
    }

    func testDecodesChannelWithNowNext() throws {
        let json = """
        {"id":"c1","number":"5.1","name":"News","tunerId":"t","favorite":false,"category":"news",
         "now":{"id":"p1","channelId":"c1","title":"Evening","start":"2026-11-11T20:00:00Z","end":"2026-11-11T21:00:00Z","categories":["News"],"isNew":true}}
        """
        let ch = try JSONDecoder().decode(LiveTvChannel.self, from: Data(json.utf8))
        XCTAssertEqual(ch.now?.title, "Evening")
        XCTAssertNil(ch.next)
        XCTAssertEqual(ch.category, "news")
    }
}
