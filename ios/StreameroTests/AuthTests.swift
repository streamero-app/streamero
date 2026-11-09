import SwiftUI
import XCTest
@testable import Streamero

final class AuthTests: XCTestCase {
    func testMemoryTokenStoreRoundTrip() {
        let store = MemoryTokenStore()
        XCTAssertNil(store.token(for: "a"))
        store.setToken("t1", for: "a")
        XCTAssertEqual(store.token(for: "a"), "t1")
        store.setToken(nil, for: "a")
        XCTAssertNil(store.token(for: "a"))
    }

    func testKeychainTokenStoreRoundTrip() {
        let store = KeychainTokenStore()
        let server = "test-\(UUID().uuidString)"
        store.setToken("secret-1", for: server)
        XCTAssertEqual(store.token(for: server), "secret-1")
        store.setToken("secret-2", for: server) // replaces, no duplicate item error
        XCTAssertEqual(store.token(for: server), "secret-2")
        store.setToken(nil, for: server)
        XCTAssertNil(store.token(for: server))
    }

    func testLoginMessagesCoverServerCodes() {
        XCTAssertEqual(LoginMessage.key(for: "invalid_credentials"), "login.error.credentials")
        XCTAssertEqual(LoginMessage.key(for: "account_locked"), "login.error.locked")
        XCTAssertEqual(LoginMessage.key(for: "invalid_totp"), "login.error.totp")
        XCTAssertEqual(LoginMessage.key(for: "something_new"), "login.error.generic")
    }

    @MainActor
    func testSessionWithoutTokenIsSignedOut() async {
        let session = SessionStore(tokens: MemoryTokenStore())
        let server = SavedServer(id: "s1", name: "Kino", url: URL(string: "https://example.invalid")!, version: "dev")
        await session.restore(server: server)
        XCTAssertEqual(session.state, .signedOut)
    }

    @MainActor
    func testFirstLaunchOfAnInstallForgetsOldSessions() {
        let defaults = UserDefaults(suiteName: "test-\(UUID().uuidString)")!
        let tokens = MemoryTokenStore()
        tokens.setToken("left-from-old-install", for: "s1")
        _ = SessionStore(tokens: tokens, defaults: defaults)
        XCTAssertNil(tokens.token(for: "s1"))
        tokens.setToken("fresh", for: "s1")
        _ = SessionStore(tokens: tokens, defaults: defaults)
        XCTAssertEqual(tokens.token(for: "s1"), "fresh", "later launches keep the session")
    }

    @MainActor
    func testAddingAServerAsksToSignIn() {
        let defaults = UserDefaults(suiteName: "test-\(UUID().uuidString)")!
        let tokens = MemoryTokenStore()
        let session = SessionStore(tokens: tokens, defaults: defaults)
        tokens.setToken("old", for: "s1")
        session.forget(serverID: "s1")
        XCTAssertNil(tokens.token(for: "s1"))
        XCTAssertEqual(session.state, .signedOut)
    }

    @MainActor
    func testDeviceInfoNamesTheClient() {
        XCTAssertEqual(AuthClient.device.client?.hasPrefix("Streamero iOS"), true)
    }
}

final class MediaClientTests: XCTestCase {
    func testDeviceProfileAsksForHLSAndHEVC() {
        let d = MediaClient.device
        XCTAssertTrue(d.hls)
        XCTAssertTrue(d.videoCodecs.contains("hevc"))
        XCTAssertTrue(d.containers.contains("mp4"))
    }

    func testAbsoluteURLsKeepTheSignature() {
        let c = MediaClient(baseURL: URL(string: "https://demo.example.com")!, token: "t")
        XCTAssertEqual(c.absolute("/stream/file/x/video.mp4?exp=1&sig=a%2Bb")?.absoluteString,
                       "https://demo.example.com/stream/file/x/video.mp4?exp=1&sig=a%2Bb")
        XCTAssertEqual(c.imageURL(item: "abc", type: "poster")?.absoluteString,
                       "https://demo.example.com/api/v1/items/abc/images/poster")
    }
}
