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
    func testDeviceInfoNamesTheClient() {
        XCTAssertEqual(AuthClient.device.client?.hasPrefix("Streamero iOS"), true)
    }
}
