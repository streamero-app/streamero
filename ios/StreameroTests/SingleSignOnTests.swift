import XCTest
@testable import Streamero

final class SingleSignOnTests: XCTestCase {
    func testChallengeIsBase64URLSHA256OfTheVerifier() {
        // Expected value computed independently: base64url(sha256(v)) without padding (Python hashlib).
        XCTAssertEqual(SingleSignOn.challenge(for: "dBjftJeZ4CVP-mJ0kXwOz2u9eiU0n3yhfPkXgPYgEDvmkoUOuIqI9Bu0WQe"),
                       "IB3LGqhE3YEn1yejQ2Ky3PGP-zyDyKSyUWSvnQFAKiA")
    }

    func testVerifiersAreRandomAndWellFormed() {
        let a = SingleSignOn.makeVerifier()
        let b = SingleSignOn.makeVerifier()
        XCTAssertNotEqual(a, b)
        XCTAssertEqual(a.count, 43)
        XCTAssertNil(a.rangeOfCharacter(from: CharacterSet(charactersIn: "+/=")))
        XCTAssertEqual(SingleSignOn.challenge(for: a).count, 43)
    }

    func testAuthorizeURLAsksForTheAppFlow() {
        let url = SingleSignOn.authorizeURL("https://m.example/api/v1/auth/oidc/kc/authorize", challenge: "abc")
        XCTAssertEqual(url?.absoluteString,
                       "https://m.example/api/v1/auth/oidc/kc/authorize?client=app&redirectUri=streamero%3A%2F%2Foidc&codeChallenge=abc")
    }

    func testParsesTheCallback() {
        XCTAssertEqual(SingleSignOn.parseCallback(URL(string: "streamero://oidc?code=c1")!), .code("c1"))
        XCTAssertEqual(SingleSignOn.parseCallback(URL(string: "streamero://oidc?error=no_account")!), .failed("no_account"))
        XCTAssertEqual(SingleSignOn.parseCallback(URL(string: "streamero://oidc")!), .failed("internal"))
        XCTAssertNil(SingleSignOn.parseCallback(URL(string: "https://oidc?code=c1")!))
        XCTAssertNil(SingleSignOn.parseCallback(URL(string: "streamero://other?code=c1")!))
    }

    func testSSOErrorCodesHaveMessages() {
        XCTAssertEqual(LoginMessage.key(for: "no_account"), "login.sso.error.noAccount")
        XCTAssertEqual(LoginMessage.key(for: "invalid_state"), "login.sso.error.state")
        XCTAssertEqual(LoginMessage.key(for: "password_login_disabled"), "login.error.passwordDisabled")
    }
}
