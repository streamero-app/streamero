import XCTest
@testable import Streamero

final class AdultTests: XCTestCase {
    private func user(access: Bool?, kid: Bool? = nil) -> User {
        let perms = UserPermissions(allLibraries: true, download: true, videoTranscoding: true, audioTranscoding: true, remux: true,
                                    liveTv: false, manageLiveTv: false, syncPlay: false, deleteMedia: false,
                                    manageCollections: false, editSubtitles: false, remoteControl: false, maxSessions: 0)
        let profile = kid.map { Profile(id: "p", name: "P", color: "#fff", hasPin: false, isKid: $0, maxRating: nil, hasAvatar: nil) }
        return User(id: "u", username: "u", isAdmin: false, disabled: false, totpEnabled: false, permissions: perms,
                    currentProfile: profile, adultAccess: access, createdAt: "")
    }

    private func status(pinSet: Bool, unlocked: Bool, blur: Bool = true) -> AdultStatus {
        AdultStatus(pinSet: pinSet, unlocked: unlocked, blur: blur, metadataConfigured: false, libraries: [])
    }

    func testEntryShowsOnlyWhenTheServerAllowsIt() {
        XCTAssertFalse(AdultGate.showsEntry(nil))
        XCTAssertFalse(AdultGate.showsEntry(user(access: nil)), "older servers send no flag")
        XCTAssertFalse(AdultGate.showsEntry(user(access: false)))
        XCTAssertTrue(AdultGate.showsEntry(user(access: true)))
        XCTAssertTrue(AdultGate.showsEntry(user(access: true, kid: false)))
        XCTAssertFalse(AdultGate.showsEntry(user(access: true, kid: true)), "never on a kids profile")
    }

    func testDecodesAdultAccessOnMe() throws {
        let json = """
        {"id":"u","username":"u","isAdmin":false,"disabled":false,"totpEnabled":false,"createdAt":"2026-01-01T00:00:00Z",
         "permissions":{"allLibraries":true,"download":false,"videoTranscoding":true,"audioTranscoding":true,"remux":true,
          "liveTv":false,"manageLiveTv":false,"syncPlay":false,"deleteMedia":false,"manageCollections":false,
          "editSubtitles":false,"remoteControl":false,"maxSessions":0},
         "adultAccess":true}
        """
        let me = try JSONDecoder().decode(User.self, from: Data(json.utf8))
        XCTAssertEqual(me.adultAccess, true)
    }

    func testLockedAndGoneErrors() {
        XCTAssertTrue(AdultGate.isLocked(APIError(status: 403, code: "adult_locked")))
        XCTAssertFalse(AdultGate.isLocked(APIError(status: 403, code: "invalid_pin")))
        XCTAssertFalse(AdultGate.isLocked(APIError(status: 404, code: "not_found")))
        XCTAssertFalse(AdultGate.isLocked(URLError(.timedOut)))
        XCTAssertTrue(AdultGate.isGone(APIError(status: 404, code: "not_found")))
        XCTAssertFalse(AdultGate.isGone(APIError(status: 500, code: "internal")))
    }

    func testPinRules() {
        XCTAssertTrue(AdultGate.validPin("1234"))
        XCTAssertTrue(AdultGate.validPin("123456789012"))
        XCTAssertFalse(AdultGate.validPin("123"))
        XCTAssertFalse(AdultGate.validPin("1234567890123"))
        XCTAssertFalse(AdultGate.validPin("12a4"))
        XCTAssertFalse(AdultGate.validPin("١٢٣٤"), "only ASCII digits")
        XCTAssertTrue(AdultGate.needsPin(status(pinSet: true, unlocked: false)))
        XCTAssertFalse(AdultGate.needsPin(status(pinSet: true, unlocked: true)))
        XCTAssertFalse(AdultGate.needsPin(status(pinSet: false, unlocked: true)))
    }

    func testBlurUntilRevealed() {
        XCTAssertTrue(AdultGate.blurred(blur: true, revealed: [], key: "scene/a"))
        XCTAssertFalse(AdultGate.blurred(blur: true, revealed: ["scene/a"], key: "scene/a"))
        XCTAssertTrue(AdultGate.blurred(blur: true, revealed: ["scene/a"], key: "scene/b"))
        XCTAssertFalse(AdultGate.blurred(blur: false, revealed: [], key: "scene/a"))
    }

    @MainActor
    func testSessionFollowsStatusAndContentErrors() {
        let session = AdultSession()
        XCTAssertTrue(session.blur, "blurred until the status says otherwise")
        session.apply(status(pinSet: true, unlocked: false, blur: false))
        XCTAssertTrue(session.locked)
        XCTAssertFalse(session.isBlurred("scene/a"))
        session.apply(status(pinSet: true, unlocked: true))
        XCTAssertFalse(session.locked)
        XCTAssertFalse(session.note(APIError(status: 500, code: "internal")))
        XCTAssertFalse(session.locked)
        XCTAssertTrue(session.note(APIError(status: 403, code: "adult_locked")))
        XCTAssertTrue(session.locked)
        session.reveal("scene/a")
        XCTAssertFalse(session.isBlurred("scene/a"))
        session.reset()
        XCTAssertFalse(session.locked)
        XCTAssertNil(session.status)
        XCTAssertTrue(session.isBlurred("scene/a"), "reset forgets revealed pictures")
    }

    func testSceneQuery() {
        let c = MediaClient(baseURL: URL(string: "https://demo.example")!, token: "t")
        var f = AdultSceneFilter(studioId: "s1")
        f.kind = "movie"
        f.q = "  beach "
        f.status = "inProgress"
        f.sort = "title"
        XCTAssertEqual(c.url(path: "adult/scenes", query: f.query(offset: 60)).absoluteString,
                       "https://demo.example/api/v1/adult/scenes?kind=movie&limit=60&offset=60&q=beach&sort=title&status=inProgress&studioId=s1")
        let plain = AdultSceneFilter().query(offset: 0)
        XCTAssertNil(plain["kind"] ?? nil)
        XCTAssertNil(plain["status"] ?? nil, "all is the server default and is not sent")
        XCTAssertNil(plain["q"] ?? nil)
        XCTAssertEqual(plain["sort"] ?? nil, "date")
        XCTAssertNil(AdultGate.nameQuery(q: " ", offset: 0, limit: 100)["q"] ?? nil)
    }

    func testImageURL() {
        let c = MediaClient(baseURL: URL(string: "https://demo.example")!, token: "t")
        XCTAssertEqual(c.adultImageURL(owner: "performer", id: "p1", type: "image")?.absoluteString,
                       "https://demo.example/api/v1/adult/images/performer/p1/image")
    }

    func testDecodesSceneDetail() throws {
        let json = """
        {"id":"s","libraryId":"l","kind":"scene","title":"T","date":"2025-04-01","year":2025,
         "studio":{"id":"st","name":"Studio"},"performers":[{"id":"p","name":"P"}],"tags":["a"],
         "durationMs":1800000,"height":1080,"hasPoster":true,"hasBackdrop":false,"positionMs":60000,"played":false,
         "addedAt":"2026-01-01T00:00:00Z","description":"D","files":[{"id":"f","name":"t.mp4","size":100,"container":"mp4"}],
         "providerIds":{"tpdb":"x"}}
        """
        let d = try JSONDecoder().decode(AdultSceneDetail.self, from: Data(json.utf8))
        XCTAssertEqual(d.studio?.name, "Studio")
        XCTAssertEqual(d.performers.first?.id, "p")
        XCTAssertEqual(d.files.first?.container, "mp4")
        XCTAssertEqual(d.providerIds["tpdb"], "x")
        XCTAssertEqual(AdultGate.progress(positionMs: d.positionMs, durationMs: d.durationMs, played: d.played) ?? 0, 1.0 / 30, accuracy: 0.0001)
        XCTAssertNil(AdultGate.progress(positionMs: 10, durationMs: 100, played: true))
        let page = try JSONDecoder().decode(AdultScenePage.self, from: Data(#"{"items":[],"total":0}"#.utf8))
        XCTAssertEqual(page.total, 0)
    }

    func testPreferencesOmitUnsetFieldsButSendAnEmptyPin() throws {
        let blur = try XCTUnwrap(JSONSerialization.jsonObject(with: MediaClient.encode(AdultPreferencesInput(blur: false))) as? [String: Any])
        XCTAssertEqual(blur["blur"] as? Bool, false)
        XCTAssertNil(blur["pin"])
        let remove = try XCTUnwrap(JSONSerialization.jsonObject(with: MediaClient.encode(AdultPreferencesInput(pin: "", currentPin: "1234"))) as? [String: Any])
        XCTAssertEqual(remove["pin"] as? String, "", "an empty PIN removes it")
        XCTAssertEqual(remove["currentPin"] as? String, "1234")
    }

    func testAdultLibrariesAreNotBrowsed() {
        let scan = ScanStatus(state: "idle")
        XCTAssertTrue(Library(id: "a", name: "A", kind: "adult", paths: [], itemCount: 0, scan: scan).isAdult)
        XCTAssertFalse(Library(id: "m", name: "M", kind: "movies", paths: [], itemCount: 0, scan: scan).isAdult)
    }
}
