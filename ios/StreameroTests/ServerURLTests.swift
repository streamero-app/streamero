import XCTest
@testable import Streamero

final class ServerURLTests: XCTestCase {
    func testAddsHTTPSWhenSchemeMissing() {
        XCTAssertEqual(ServerURL.normalize("demo.streamero.net")?.absoluteString, "https://demo.streamero.net")
    }

    func testKeepsExplicitHTTPAndPort() {
        XCTAssertEqual(ServerURL.normalize(" http://192.168.1.10:8787/ ")?.absoluteString, "http://192.168.1.10:8787")
    }

    func testStripsAPISuffix() {
        XCTAssertEqual(ServerURL.normalize("https://x.net/api/v1")?.absoluteString, "https://x.net")
    }

    func testRejectsEmpty() {
        XCTAssertNil(ServerURL.normalize("   "))
    }

    func testAPIBase() {
        XCTAssertEqual(ServerURL.apiBase(URL(string: "https://x.net")!).absoluteString, "https://x.net/api/v1")
    }
}
