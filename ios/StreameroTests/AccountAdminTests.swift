import XCTest
@testable import Streamero

final class InviteLinkTests: XCTestCase {
    func testParsesAWebInviteLink() {
        let parsed = InviteLink.parse("https://media.example.net/invite/AbC_def-123456")
        XCTAssertEqual(parsed?.server?.absoluteString, "https://media.example.net")
        XCTAssertEqual(parsed?.token, "AbC_def-123456")
    }

    func testKeepsPortSchemeAndSubpath() {
        let parsed = InviteLink.parse("  http://192.168.1.10:8787/streamero/invite/tok12345678/  ")
        XCTAssertEqual(parsed?.server?.absoluteString, "http://192.168.1.10:8787/streamero")
        XCTAssertEqual(parsed?.token, "tok12345678")
    }

    func testAddsHTTPSWithoutScheme() {
        let parsed = InviteLink.parse("demo.streamero.net/invite/abcdefgh1234")
        XCTAssertEqual(parsed?.server?.absoluteString, "https://demo.streamero.net")
        XCTAssertEqual(parsed?.token, "abcdefgh1234")
    }

    func testIgnoresQueryAndFragment() {
        let parsed = InviteLink.parse("https://m.example/invite/abcdefgh1234?utm=x#top")
        XCTAssertEqual(parsed, InviteLink.Parsed(server: URL(string: "https://m.example"), token: "abcdefgh1234"))
    }

    func testABareTokenHasNoServer() {
        XCTAssertEqual(InviteLink.parse("Zx9_yW-8vU7tS6rQ"), InviteLink.Parsed(server: nil, token: "Zx9_yW-8vU7tS6rQ"))
    }

    func testRejectsOtherLinksAndJunk() {
        XCTAssertNil(InviteLink.parse(""))
        XCTAssertNil(InviteLink.parse("https://m.example/items/123"))
        XCTAssertNil(InviteLink.parse("https://m.example/invite/"))
        XCTAssertNil(InviteLink.parse("short"))
        XCTAssertNil(InviteLink.parse("has spaces in it"))
        XCTAssertNil(InviteLink.parse(String(repeating: "a", count: 129)))
    }

    func testParsesTheAppURL() {
        let url = URL(string: "streamero://invite?server=https%3A%2F%2Fm.example%3A8443&token=abcdefgh1234")!
        XCTAssertEqual(InviteLink.parse(url: url), InviteLink.Parsed(server: URL(string: "https://m.example:8443"), token: "abcdefgh1234"))
        XCTAssertEqual(InviteLink.parse("streamero://invite?token=abcdefgh1234"), InviteLink.Parsed(server: nil, token: "abcdefgh1234"))
    }

    func testTheAppURLIgnoresSingleSignOn() {
        XCTAssertNil(InviteLink.parse(url: URL(string: "streamero://oidc?code=abcdefgh1234")!))
        XCTAssertNil(InviteLink.parse(url: URL(string: "streamero://invite")!))
        XCTAssertNil(InviteLink.parse(url: URL(string: "https://invite?token=abcdefgh1234")!))
    }

    func testSignUpFormValidation() {
        var form = InviteSignUpForm(username: "  ", password: "longenough", confirm: "longenough")
        XCTAssertEqual(form.problem, .username)
        form.username = "ana"
        XCTAssertNil(form.problem)
        form.password = "short"
        form.confirm = "short"
        XCTAssertEqual(form.problem, .passwordLength)
        form.password = "longenough"
        form.confirm = "longenougH"
        XCTAssertEqual(form.problem, .mismatch)
        form.username = String(repeating: "x", count: 65)
        XCTAssertEqual(form.problem, .username)
    }
}

final class TwoFactorTests: XCTestCase {
    private let uri = "otpauth://totp/Streamero:ana?secret=JBSWY3DPEHPK3PXP&issuer=Streamero&algorithm=SHA1&digits=6&period=30"

    func testQRInputIsTheOtpauthURI() {
        XCTAssertEqual(TwoFactor.qrMessage(for: uri), Data(uri.utf8))
        XCTAssertNil(TwoFactor.qrMessage(for: "https://example.com"))
        XCTAssertNil(TwoFactor.qrMessage(for: "otpauth://hotp/x?secret=A"))
        XCTAssertNil(TwoFactor.qrMessage(for: ""))
    }

    func testDrawsASquareQRCode() throws {
        let image = try XCTUnwrap(TwoFactor.qrImage(for: uri, scale: 4))
        XCTAssertEqual(image.size.width, image.size.height)
        XCTAssertGreaterThan(image.size.width, 80)
        XCTAssertNil(TwoFactor.qrImage(for: "not a uri"))
    }

    func testReadsTheSecret() {
        XCTAssertEqual(TwoFactor.secret(in: uri), "JBSWY3DPEHPK3PXP")
        XCTAssertNil(TwoFactor.secret(in: "otpauth://totp/x?issuer=S"))
    }

    func testCodeValidation() {
        XCTAssertTrue(TwoFactor.isValidCode("123456"))
        XCTAssertTrue(TwoFactor.isValidCode("123 456"))
        XCTAssertEqual(TwoFactor.normalizedCode(" 123 456 "), "123456")
        XCTAssertFalse(TwoFactor.isValidCode("12345"))
        XCTAssertFalse(TwoFactor.isValidCode("123456789"))
        XCTAssertFalse(TwoFactor.isValidCode("12a456"))
        XCTAssertFalse(TwoFactor.isValidCode("١٢٣٤٥٦"), "only ASCII digits")
    }

    func testRecoveryCodesAsText() {
        XCTAssertEqual(TwoFactor.recoveryText(["a1", "b2"]), "a1\nb2")
    }
}

final class ProfileFormTests: XCTestCase {
    func testValidation() {
        var form = ProfileForm()
        XCTAssertEqual(form.problem, .name)
        form.name = "Mia"
        XCTAssertNil(form.problem)
        form.pin = "12"
        XCTAssertEqual(form.problem, .pin)
        form.pin = "1234"
        XCTAssertNil(form.problem)
        form.pin = "1234567"
        XCTAssertEqual(form.problem, .pin)
        form.pin = "12a4"
        XCTAssertEqual(form.problem, .pin)
        form.pin = ""
        form.name = String(repeating: "n", count: 41)
        XCTAssertEqual(form.problem, .name)
    }

    func testInputKeepsOrRemovesThePin() {
        var form = ProfileForm()
        form.name = " Mia "
        form.isKid = true
        form.maxRating = 12
        XCTAssertEqual(form.input, ProfileInput(name: "Mia", color: ProfileForm.colors[0], isKid: true, maxRating: 12, pin: nil, removePin: nil))
        form.pin = "4321"
        XCTAssertEqual(form.input.pin, "4321")
        form.removePin = true
        XCTAssertNil(form.input.pin)
        XCTAssertEqual(form.input.removePin, true)
    }

    func testEncodesOnlyWhatIsSet() throws {
        let json = String(decoding: try MediaClient.encode(ProfileForm(Profile(id: "p", name: "Leo", color: "#0090ff", hasPin: true, isKid: false)).input), as: UTF8.self)
        XCTAssertTrue(json.contains("\"isKid\":false"))
        XCTAssertFalse(json.contains("pin"), "an empty PIN keeps the stored one")
    }

    func testRespectsTheServerMaximum() {
        XCTAssertTrue(ProfileForm.canAdd(existing: 5))
        XCTAssertFalse(ProfileForm.canAdd(existing: 6))
    }

    func testKidsProfilesCannotOpenPersonalPages() {
        XCTAssertTrue(SettingsPage.profiles.isPersonal)
        XCTAssertTrue(SettingsPage.security.isPersonal)
        XCTAssertTrue(SettingsPage.devices.isPersonal)
        for page in [SettingsPage.notifications, .backups, .liveTvSources, .signIn] {
            XCTAssertTrue(page.isAdminOnly)
            XCTAssertFalse(page.isPersonal)
        }
    }
}

final class AdminFormTests: XCTestCase {
    func testNotificationTargetValidationAndWriteOnlyToken() {
        var target = NotificationTarget.new()
        XCTAssertFalse(target.isValid)
        target.name = "Discord"
        target.url = "ftp://x"
        XCTAssertFalse(target.isValid)
        target.url = " https://discord.com/api/webhooks/1 "
        XCTAssertTrue(target.isValid)
        target.kind = "ntfy"
        target.token = ""
        XCTAssertNil(target.forSaving.token, "an empty token keeps the stored one")
        target.token = "tk"
        XCTAssertEqual(target.forSaving.token, "tk")
        XCTAssertEqual(target.forSaving.url, "https://discord.com/api/webhooks/1")
        target.kind = "discord"
        XCTAssertNil(target.forSaving.token)
    }

    func testTunerForm() {
        var form = TunerForm()
        form.name = "IPTV"
        XCTAssertFalse(form.isValid)
        form.url = "https://example.com/list.m3u"
        XCTAssertTrue(form.isValid)
        form.count = "99"
        XCTAssertFalse(form.isValid)
        form.count = ""
        form.fromFile = true
        XCTAssertFalse(form.isValid)
        form.filePath = "/config/a.m3u"
        XCTAssertEqual(form.input, LiveTvTunerInput(kind: "m3u", name: "IPTV", url: nil, filePath: "/config/a.m3u", tunerCount: nil, userAgent: nil, enabled: true))
        form.kind = "hdhomerun"
        form.url = ""
        XCTAssertFalse(form.isValid, "HDHomeRun needs an address")
        form.url = "http://10.0.0.5"
        XCTAssertEqual(form.input.url, "http://10.0.0.5")
        XCTAssertNil(form.input.filePath)
    }

    func testGuideForm() {
        var form = GuideForm()
        form.name = "EPG"
        form.url = "https://e.example/guide.xml.gz"
        XCTAssertTrue(form.isValid)
        XCTAssertNil(form.input.timezone)
        form.refreshHours = 0
        XCTAssertFalse(form.isValid)
    }

    func testOidcForm() {
        XCTAssertTrue(OidcForm.isValidID("keycloak-1"))
        XCTAssertFalse(OidcForm.isValidID("-bad"))
        XCTAssertFalse(OidcForm.isValidID("Upper"))
        var form = OidcForm()
        form.apply(.authelia)
        form.clientId = "streamero"
        XCTAssertTrue(form.isValid)
        XCTAssertEqual(form.input.scopes, ["openid", "profile", "email", "groups"])
        XCTAssertNil(form.input.clientSecret, "an empty secret keeps the stored one")
    }

    func testRestoreNeedsTheExactName() {
        let b = Backup(name: "streamero-2026.tar.gz", sizeBytes: 1, createdAt: "", kind: "manual", serverVersion: "", schemaVersion: 1,
                       includesImages: false, scanRunning: false, restorable: true)
        XCTAssertFalse(b.confirms("streamero-2026"))
        XCTAssertFalse(b.confirms(" streamero-2026.tar.gz"))
        XCTAssertTrue(b.confirms("streamero-2026.tar.gz"))
    }

    func testLdapPlainTextWarning() {
        let s = LdapSettings(enabled: true, url: "ldap://dir:389", startTls: false, skipVerify: false, caCert: "", bindDn: "",
                             bindPasswordSet: true, baseDn: "", userFilter: "", usernameAttribute: "", emailAttribute: "",
                             nameAttribute: "", groupAttribute: "", groupFilter: "", adminGroup: "", allowedGroup: "")
        var input = LdapSettingsInput(s)
        XCTAssertTrue(input.isPlainText)
        XCTAssertNil(input.bindPassword, "the stored bind password is kept")
        input.startTls = true
        XCTAssertFalse(input.isPlainText)
    }
}
