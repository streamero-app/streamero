import SwiftUI
import UIKit
import AVFoundation
import XCTest
@testable import Streamero

final class SignInDecodingTests: XCTestCase {
    func testAnUnknownLanguageDoesNotFailASignIn() throws {
        let body = #"{"token":"abc","sessionId":"s","user":{"id":"u","username":"ana","language":"de","isAdmin":false,"newField":1}}"#
        XCTAssertEqual(try AuthClient.sessionToken(status: 200, data: Data(body.utf8)), "abc")
    }

    func testErrorsKeepTheServerCode() {
        XCTAssertThrowsError(try AuthClient.sessionToken(status: 401, data: Data(#"{"code":"totp_required"}"#.utf8))) { error in
            XCTAssertEqual(error as? APIError, APIError(status: 401, code: "totp_required"), "a body without a message still gives its code")
        }
        XCTAssertThrowsError(try AuthClient.sessionToken(status: 502, data: Data("<html>Bad gateway</html>".utf8))) { error in
            XCTAssertEqual((error as? APIError)?.code, "http_502")
        }
        XCTAssertThrowsError(try AuthClient.sessionToken(status: 200, data: Data(#"{"sessionId":"s"}"#.utf8))) { error in
            XCTAssertEqual((error as? APIError)?.code, "no_token")
        }
    }

    func testQuickConnectWaitsOn202() throws {
        XCTAssertEqual(try AuthClient.quickConnectState(status: 202, data: Data()), .pending)
        XCTAssertEqual(try AuthClient.quickConnectState(status: 200, data: Data(#"{"token":"t"}"#.utf8)), .signedIn(token: "t"))
    }
}

final class SessionRaceTests: XCTestCase {
    @MainActor
    func testALateCheckDoesNotOverwriteTheNextServer() async {
        let defaults = UserDefaults(suiteName: "test-\(UUID().uuidString)")!
        let tokens = MemoryTokenStore()
        let session = SessionStore(tokens: tokens, defaults: defaults) { server, _ in
            if server.id == "a" {
                try await Task.sleep(for: .milliseconds(300))
                throw APIError(status: 401, code: "unauthenticated")
            }
        }
        tokens.setToken("ta", for: "a")
        tokens.setToken("tb", for: "b")
        let a = SavedServer(id: "a", name: "A", url: URL(string: "https://a.invalid")!, version: "1")
        let b = SavedServer(id: "b", name: "B", url: URL(string: "https://b.invalid")!, version: "1")
        let first = Task { await session.restore(server: a) }
        try? await Task.sleep(for: .milliseconds(50))
        await session.restore(server: b)
        await first.value
        XCTAssertEqual(session.state, .signedIn, "server A's late answer must not sign out server B")
        XCTAssertEqual(tokens.token(for: "a"), "ta", "nor forget A's token while B is shown")
    }

    @MainActor
    func testSignOutDoesNotWaitForTheServer() async {
        let defaults = UserDefaults(suiteName: "test-\(UUID().uuidString)")!
        let tokens = MemoryTokenStore()
        let session = SessionStore(tokens: tokens, defaults: defaults) { _, _ in }
        let server = SavedServer(id: "s", name: "S", url: URL(string: "https://unreachable.invalid")!, version: "1")
        session.signedIn(server: server, token: "t")
        await session.signOut(server: server)
        XCTAssertNil(tokens.token(for: "s"))
        XCTAssertEqual(session.state, .signedOut)
    }
}

final class OriginTests: XCTestCase {
    private let client = MediaClient(baseURL: URL(string: "https://media.example.com")!, token: "t")

    func testArtworkStaysOnTheServer() {
        XCTAssertEqual(client.ownURL("/api/v1/items/a/images/poster")?.absoluteString, "https://media.example.com/api/v1/items/a/images/poster")
        XCTAssertNil(client.ownURL("https://tracker.example.net/logo.png"), "the session token never goes to another host")
        XCTAssertNil(client.ownURL("http://media.example.com/x.png"), "nor over plain http to the same name")
        XCTAssertNotNil(client.ownURL("https://media.example.com:443/x.png"))
    }

    func testPlainHTTPNeedsConsentOnlyOffTheLocalNetwork() {
        for local in ["http://192.168.1.10:8787", "http://10.0.0.2", "http://172.20.1.1", "http://nas.local:8787",
                      "http://nas", "http://localhost:8787", "http://100.101.102.103", "http://[fd00::1]:8787", "http://[fe80::1]"] {
            XCTAssertFalse(ServerURL.isPlainHTTPOverInternet(URL(string: local)!), local)
        }
        for remote in ["http://media.example.com", "http://8.8.8.8", "http://172.32.0.1", "http://[2001:db8::1]"] {
            XCTAssertTrue(ServerURL.isPlainHTTPOverInternet(URL(string: remote)!), remote)
        }
        XCTAssertFalse(ServerURL.isPlainHTTPOverInternet(URL(string: "https://media.example.com")!))
    }
}

final class PlayerHardeningTests: XCTestCase {
    func testFirstFailureFallsBackOnceThenShowsTheError() {
        var r = PlaybackRecovery()
        XCTAssertFalse(r.safe)
        XCTAssertEqual(r.onError(), .fallback)
        XCTAssertTrue(r.safe)
        XCTAssertEqual(r.onError(), .fail)
        r.retry()
        XCTAssertEqual(r.onError(), .fail, "Retry keeps the safe profile")
    }

    func testSafeProfileIsPlainH264AndKeepsQualityLimits() {
        let p = MediaClient.safeDevice(preferences: nil)
        XCTAssertEqual(p.videoCodecs, ["h264"])
        XCTAssertEqual(p.audioCodecs, ["aac"])
        XCTAssertTrue(p.hls)
    }

    func testMalformedSubtitleTimestampsDoNotCrash() {
        XCTAssertNil(WebVTT.parseTimestamp("00:."))
        XCTAssertNil(WebVTT.parseTimestamp("00:01:x"))
        XCTAssertEqual(WebVTT.parse("WEBVTT\n\n00:. --> 00:02.000\nHi\n\n00:01.000 --> 00:02.000\nOk").map(\.text), ["Ok"])
    }

    func testInterruptionNotificationsAreRead() {
        let began: [AnyHashable: Any] = [AVAudioSessionInterruptionTypeKey: AVAudioSession.InterruptionType.began.rawValue]
        XCTAssertEqual(Interruptions.parse(began)?.began, true)
        let ended: [AnyHashable: Any] = [AVAudioSessionInterruptionTypeKey: AVAudioSession.InterruptionType.ended.rawValue,
                                         AVAudioSessionInterruptionOptionKey: AVAudioSession.InterruptionOptions.shouldResume.rawValue]
        XCTAssertEqual(Interruptions.parse(ended)?.shouldResume, true)
        XCTAssertNil(Interruptions.parse(nil))
    }
}

final class PresentationTests: XCTestCase {
    func testImageBucketsMatchTheViewSize() {
        XCTAssertEqual(ImageCache.bucket(for: 300), 512)
        XCTAssertEqual(ImageCache.bucket(for: 512), 512)
        XCTAssertEqual(ImageCache.bucket(for: 9000), 4096)
        XCTAssertEqual(ImageCache.bucket(for: 0), 1024)
        XCTAssertEqual(ImageCache.bucket(for: .infinity), 1024)
    }

    func testDecodingDownsamples() throws {
        let image = UIGraphicsImageRenderer(size: CGSize(width: 1200, height: 600), format: {
            let f = UIGraphicsImageRendererFormat()
            f.scale = 1
            return f
        }()).image { ctx in
            UIColor.red.setFill()
            ctx.fill(CGRect(x: 0, y: 0, width: 1200, height: 600))
        }
        let data = try XCTUnwrap(image.pngData())
        let small = try XCTUnwrap(ImageCache.decode(data, maxPixels: 256))
        XCTAssertLessThanOrEqual(max(small.size.width * small.scale, small.size.height * small.scale), 256)
    }

    func testSecretsOnThePasteboardAreLocalAndExpire() {
        let now = Date(timeIntervalSince1970: 1000)
        let options = SecretPasteboard.options(sensitive: true, now: now)
        XCTAssertEqual(options[.localOnly] as? Bool, true)
        XCTAssertEqual(options[.expirationDate] as? Date, now.addingTimeInterval(SecretPasteboard.lifetime))
        XCTAssertTrue(SecretPasteboard.options(sensitive: false).isEmpty)
    }

    func testServerDatesAreFormattedNotSliced() {
        let en = Locale(identifier: "en_US")
        XCTAssertNotNil(ServerDate.parse("2024-05-01"))
        XCTAssertTrue(ServerDate.short("2024-05-01", time: false, locale: en).contains("2024"))
        XCTAssertFalse(ServerDate.short("2024-05-01T12:30:00Z", locale: en).contains("T"))
        XCTAssertEqual(ServerDate.short("not a date", time: false), "not a date")
    }

    @MainActor
    func testEveryHomeSectionHasItsOwnTitle() {
        XCTAssertEqual(HomeSectionsPage.all, ["continue", "nextUp", "latest", "favorites", "collections", "top", "genres", "libraries"])
        XCTAssertEqual(HomeSectionsPage.title("top"), "home.top")
        XCTAssertEqual(HomeSectionsPage.title("genres"), "home.genres")
    }

    @MainActor
    func testEveryTaskHasItsOwnTitle() {
        let keys = ["scan_all", "refresh_metadata", "cleanup_images", "analyze_media", "detect_segments", "backup", "refresh_guide"]
        let titles = keys.map { TasksPage.title($0) }
        XCTAssertEqual(Set(titles.map { "\($0)" }).count, keys.count)
    }

    func testProfileColoursHaveSpokenNames() {
        XCTAssertEqual(ProfileForm.colorName("#E5484D"), "colour.red")
        XCTAssertEqual(ProfileForm.colors.map(ProfileForm.colorName).count, Set(ProfileForm.colors.map(ProfileForm.colorName)).count)
        XCTAssertEqual(ProfileForm.colorName("#123456"), "colour")
    }

    func testSeasonEpisodeConvention() {
        XCTAssertEqual(seasonEpisode(season: 1, episode: 2), "S01E02")
        XCTAssertEqual(seasonEpisode(season: nil, episode: 12), "S00E12")
    }
}
