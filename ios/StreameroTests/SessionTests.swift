import SwiftUI
import XCTest
@testable import Streamero

final class SessionTests: XCTestCase {
    func testOnlyRejectedTokensExpireTheSession() {
        XCTAssertTrue(MediaClient.sessionExpired(APIError(status: 401, code: "unauthenticated")))
        XCTAssertTrue(MediaClient.sessionExpired(APIError(status: 401, code: "http_401")))
        XCTAssertFalse(MediaClient.sessionExpired(APIError(status: 401, code: "invalid_pin")), "wrong PIN keeps the session")
        XCTAssertFalse(MediaClient.sessionExpired(APIError(status: 401, code: "invalid_credentials")), "wrong password keeps the session")
        XCTAssertFalse(MediaClient.sessionExpired(APIError(status: 403, code: "forbidden")))
    }

    func testRestoreTreatsOnlyOutagesAsOffline() {
        XCTAssertEqual(SessionStore.state(after: URLError(.notConnectedToInternet)), .offline)
        XCTAssertEqual(SessionStore.state(after: URLError(.timedOut)), .offline)
        XCTAssertEqual(SessionStore.state(after: APIError(status: 503, code: "http_503")), .offline)
        XCTAssertEqual(SessionStore.state(after: APIError(status: 401, code: "unauthenticated")), .signedOut)
        let decoding = DecodingError.dataCorrupted(.init(codingPath: [], debugDescription: "new enum value"))
        XCTAssertEqual(SessionStore.state(after: decoding), .signedIn, "an unreadable answer is not an outage")
    }

    @MainActor
    func testExpireForgetsOnlyTheCurrentToken() {
        let defaults = UserDefaults(suiteName: "test-\(UUID().uuidString)")!
        let tokens = MemoryTokenStore()
        let session = SessionStore(tokens: tokens, defaults: defaults)
        let server = SavedServer(id: "s1", name: "Kino", url: URL(string: "https://example.invalid")!, version: "dev")
        session.signedIn(server: server, token: "new")
        session.expire(server: server, token: "old")
        XCTAssertEqual(tokens.token(for: "s1"), "new", "a stale client's 401 is ignored")
        XCTAssertEqual(session.state, .signedIn)
        session.expire(server: server, token: "new")
        XCTAssertNil(tokens.token(for: "s1"))
        XCTAssertEqual(session.state, .signedOut)
    }

    func testProfileGateMessages() {
        XCTAssertEqual(ProfileGateMessage.key(for: APIError(status: 401, code: "invalid_pin")), "profiles.wrongPin")
        XCTAssertEqual(ProfileGateMessage.key(for: APIError(status: 429, code: "profile_locked")), "profiles.locked")
        XCTAssertEqual(ProfileGateMessage.key(for: APIError(status: 401, code: "invalid_credentials")), "profiles.wrongPassword")
        XCTAssertEqual(ProfileGateMessage.key(for: URLError(.notConnectedToInternet)), "err_network")
        XCTAssertEqual(ProfileGateMessage.key(for: APIError(status: 400, code: "something_new")), "err_generic")
    }
}
