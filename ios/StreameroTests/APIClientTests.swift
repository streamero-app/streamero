import XCTest
@testable import Streamero

final class RequestEncodingTests: XCTestCase {
    private func json<B: Encodable>(_ body: B) throws -> [String: Any] {
        let data = try MediaClient.encode(body)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    func testRequiredFalseFieldsAreSent() throws {
        let enh = Enhancements(qualityTags: false, randomButton: false, pauseScreen: false, arrLinks: false,
                               discoverInSearch: false, removeFromContinue: false, shortcutsHelp: false, autoPip: false)
        let body = try json(enh)
        XCTAssertEqual(body["autoPip"] as? Bool, false)
        XCTAssertEqual(body["qualityTags"] as? Bool, false)
    }

    func testRequiredFalseAndZeroInEncodingSettings() throws {
        let s = EncodingSettings(hardwareAcceleration: "none", preset: "veryfast", quality: 22, audioBitrateKbps: 192, segmentSeconds: 6, maxSessions: 0, toneMapping: false)
        let body = try json(s)
        XCTAssertEqual(body["maxSessions"] as? Int, 0)
        XCTAssertEqual(body["toneMapping"] as? Bool, false)
        XCTAssertNil(body["vaapiDevice"], "nil optionals are omitted")
    }

    func testSubtitleOfferKeepsAllRequiredFields() throws {
        let offer = SubtitleOffer(provider: "p", language: "hr", forced: false, hearingImpaired: false, score: 0,
                                  release: nil, uploader: nil, subtitle: "s", originalFormat: false)
        let body = try json(offer)
        XCTAssertEqual(body["forced"] as? Bool, false)
        XCTAssertEqual(body["hearingImpaired"] as? Bool, false)
        XCTAssertEqual(body["originalFormat"] as? Bool, false)
        XCTAssertEqual(body["score"] as? Double, 0)
        XCTAssertNil(body["uploader"])
    }

    func testUsersCanRequestFalseIsSent() throws {
        let input = RequestSettingsInput(usersCanRequest: false)
        let body = try json(input)
        XCTAssertEqual(body["usersCanRequest"] as? Bool, false)
        XCTAssertNil(body["seerrUrl"])
    }

    func testPreferencesKeepsRequiredFields() throws {
        let p = Preferences(subtitleMode: "default", autoplayNext: false)
        let body = try json(p)
        XCTAssertEqual(body["autoplayNext"] as? Bool, false)
        XCTAssertEqual(body["subtitleMode"] as? String, "default")
        XCTAssertNil(body["skipIntro"])
    }

    func testCreateUserSendsIsAdminFalse() throws {
        let body = try json(CreateUser(username: "u", password: "12345678", isAdmin: false))
        XCTAssertEqual(body["isAdmin"] as? Bool, false)
    }

    func testPositionsAreIntegerMilliseconds() throws {
        let report = ProgressReport(positionMs: 1234, durationMs: 5000)
        let data = try MediaClient.encode(report)
        let text = String(decoding: data, as: UTF8.self)
        XCTAssertTrue(text.contains("\"positionMs\":1234"), text)
        XCTAssertFalse(text.contains("."), "no fractional milliseconds: \(text)")
    }

    func testPlaybackRequestOmitsNilOptionals() throws {
        let req = PlaybackRequest(device: MediaClient.device, fileId: nil, audioIndex: nil, startMs: 90_500)
        let body = try json(req)
        XCTAssertNil(body["fileId"])
        XCTAssertNil(body["audioIndex"])
        XCTAssertEqual(body["startMs"] as? Int, 90_500)
        XCTAssertNotNil(body["device"])
    }

    func testDeviceProfileAppliesQualityLimits() {
        var prefs = Preferences(subtitleMode: "default", autoplayNext: true)
        prefs.maxHeight = 720
        prefs.maxBitrateKbps = 4000
        let d = MediaClient.device(preferences: prefs)
        XCTAssertEqual(d.maxHeight, 720)
        XCTAssertEqual(d.maxBitrate, 4_000_000)
        XCTAssertTrue(d.hls)
    }
}

final class AccessRuleTests: XCTestCase {
    private func user(admin: Bool, editSubs: Bool = false, kid: Bool? = nil) -> User {
        let perms = UserPermissions(allLibraries: true, download: false, videoTranscoding: true, audioTranscoding: true, remux: true,
                                    liveTv: false, manageLiveTv: false, syncPlay: false, deleteMedia: false,
                                    manageCollections: false, editSubtitles: editSubs, remoteControl: false, maxSessions: 0)
        let profile = kid.map { Profile(id: "p", name: "P", color: "#fff", hasPin: false, isKid: $0, maxRating: nil, hasAvatar: nil) }
        return User(id: "u", username: "u", isAdmin: admin, disabled: false, totpEnabled: false, permissions: perms,
                    currentProfile: profile, displayName: nil, email: nil, language: nil, hasAvatar: nil, createdAt: "", lastLoginAt: nil)
    }

    func testAdminGating() {
        XCTAssertFalse(Access.isAdmin(nil))
        XCTAssertFalse(Access.isAdmin(user(admin: false)))
        XCTAssertTrue(Access.isAdmin(user(admin: true)))
        XCTAssertTrue(Access.isAdmin(user(admin: true, kid: false)))
        XCTAssertFalse(Access.isAdmin(user(admin: true, kid: true)), "admin on a kids profile sees no administration")
    }

    func testSubtitlePermissionRule() {
        XCTAssertFalse(Access.canFindSubtitles(nil))
        XCTAssertFalse(Access.canFindSubtitles(user(admin: false)))
        XCTAssertTrue(Access.canFindSubtitles(user(admin: true)))
        XCTAssertTrue(Access.canFindSubtitles(user(admin: false, editSubs: true)))
        XCTAssertFalse(Access.canFindSubtitles(user(admin: true, kid: true)))
        XCTAssertFalse(Access.canFindSubtitles(user(admin: false, editSubs: true, kid: true)))
    }

    func testAdminPagesAreExactlyTheAdminOnes() {
        XCTAssertFalse(SettingsPage.account.isAdminOnly)
        XCTAssertFalse(SettingsPage.home.isAdminOnly)
        XCTAssertTrue(SettingsPage.dashboard.isAdminOnly)
        XCTAssertTrue(SettingsPage.branding.isAdminOnly)
        XCTAssertEqual(SettingsPage.userPages.count + SettingsPage.adminPages.count, SettingsPage.allCases.count)
    }
}

final class WebVTTTests: XCTestCase {
    func testParsesCuesAndStripsTags() {
        let text = "WEBVTT\n\n1\n00:00:01.500 --> 00:00:03.000 line:90%\n<i>Hello</i> there\n\n00:04.000 --> 00:05.250\nSecond\nline\n"
        let cues = WebVTT.parse(text)
        XCTAssertEqual(cues.count, 2)
        XCTAssertEqual(cues[0], SubtitleCue(startMs: 1500, endMs: 3000, text: "Hello there"))
        XCTAssertEqual(cues[1].startMs, 4000)
        XCTAssertEqual(cues[1].endMs, 5250)
        XCTAssertEqual(cues[1].text, "Second\nline")
    }

    func testFindsTheActiveCue() {
        let cues = [SubtitleCue(startMs: 0, endMs: 1000, text: "a"), SubtitleCue(startMs: 2000, endMs: 3000, text: "b")]
        XCTAssertEqual(WebVTT.cue(in: cues, at: 2500)?.text, "b")
        XCTAssertNil(WebVTT.cue(in: cues, at: 1500))
    }
}
