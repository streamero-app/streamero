import CoreGraphics
import XCTest
@testable import Streamero

final class PlaybackLogicTests: XCTestCase {
    /// A file with tracks 0 (PGS), 1 (English SRT), 2 (VobSub), 3 (Croatian SRT): the server lists
    /// only 1 and 3 as WebVTT, so positions in `subtitles` differ from file indexes.
    private func info(defaultPos: Int? = nil, burned: Int? = nil) -> PlaybackInfo {
        PlaybackInfo(fileId: "f", method: "direct", url: "/v", startMs: 0, durationMs: nil, audio: [], audioIndex: 0,
                     subtitles: [PlaybackSubtitle(index: 1, language: "eng", forced: false, external: false, url: "/s/1.vtt"),
                                 PlaybackSubtitle(index: 3, language: "hrv", forced: false, external: true, url: "/s/3.vtt")],
                     subtitleDefault: defaultPos,
                     bitmapSubtitles: [PlaybackBitmapSubtitle(index: 0, codec: "hdmv_pgs_subtitle", language: "eng"),
                                       PlaybackBitmapSubtitle(index: 2, codec: "dvd_subtitle", language: "hrv")],
                     burnedSubtitle: burned)
    }

    func testDefaultSubtitleIsMappedToTheFileIndex() {
        XCTAssertEqual(SubtitleTracks.current(in: info(defaultPos: 1)), 3)
        XCTAssertEqual(SubtitleTracks.current(in: info(defaultPos: 0)), 1)
        XCTAssertEqual(SubtitleTracks.current(in: info()), -1)
    }

    func testBurnedTrackWins() {
        XCTAssertEqual(SubtitleTracks.current(in: info(defaultPos: 0, burned: 2)), 2)
    }

    func testTextTrackLookupUsesFileIndex() {
        XCTAssertEqual(SubtitleTracks.text(3, in: info())?.language, "hrv")
        XCTAssertNil(SubtitleTracks.text(0, in: info()), "a bitmap track has no WebVTT")
    }

    func testReloadOnlyForBurnIn() {
        XCTAssertFalse(SubtitleTracks.needsReload(choosing: 1, in: info()))
        XCTAssertFalse(SubtitleTracks.needsReload(choosing: -1, in: info()))
        XCTAssertTrue(SubtitleTracks.needsReload(choosing: 0, in: info()), "bitmap needs burn-in")
        XCTAssertFalse(SubtitleTracks.needsReload(choosing: 2, in: info(burned: 2)), "already burned in")
        XCTAssertTrue(SubtitleTracks.needsReload(choosing: 1, in: info(burned: 2)), "leaving burn-in restarts")
        XCTAssertTrue(SubtitleTracks.needsReload(choosing: -1, in: info(burned: 2)))
    }

    func testPlaybackInfoDecodesBitmapSubtitles() throws {
        let json = """
        {"fileId":"f","method":"transcode","url":"/hls","startMs":5000,"audio":[],"subtitles":[],
         "bitmapSubtitles":[{"index":0,"codec":"hdmv_pgs_subtitle","language":"eng","forced":true}],"burnedSubtitle":0}
        """
        let pi = try JSONDecoder().decode(PlaybackInfo.self, from: Data(json.utf8))
        XCTAssertEqual(pi.bitmapSubtitles?.first?.codec, "hdmv_pgs_subtitle")
        XCTAssertEqual(pi.burnedSubtitle, 0)
        let old = try JSONDecoder().decode(PlaybackInfo.self, from: Data(#"{"fileId":"f","method":"direct","url":"/v","startMs":0,"audio":[],"subtitles":[]}"#.utf8))
        XCTAssertNil(old.bitmapSubtitles)
        XCTAssertNil(old.burnedSubtitle)
    }

    func testPlaybackRequestSendsSubtitleIndex() throws {
        let req = PlaybackRequest(device: MediaClient.device, fileId: nil, audioIndex: nil, startMs: nil, subtitleIndex: 2)
        let body = try JSONSerialization.jsonObject(with: MediaClient.encode(req)) as? [String: Any]
        XCTAssertEqual(body?["subtitleIndex"] as? Int, 2)
        XCTAssertNil(body?["burnSubtitle"])
        let none = PlaybackRequest(device: MediaClient.device, fileId: nil, audioIndex: nil, startMs: nil, subtitleIndex: -1)
        let off = try JSONSerialization.jsonObject(with: MediaClient.encode(none)) as? [String: Any]
        XCTAssertEqual(off?["subtitleIndex"] as? Int, -1, "-1 (none) is sent, nil (automatic) is not")
    }

    func testSubtitleClockStartComesFromTheURL() {
        XCTAssertEqual(WebVTT.clockStart(of: URL(string: "https://s/stream/sub/f/1.vtt?offsetMs=90000&sig=x")!), 90_000)
        XCTAssertEqual(WebVTT.clockStart(of: URL(string: "https://s/stream/sub/f/1.vtt?sig=x")!), 0)
    }

    func testProgressEveryTenSecondsOfPlayback() {
        var t = ProgressTrigger()
        var reports = 0
        for s in 0..<30 {
            if t.tick(playing: true, positionMs: Int64(s) * 1000) { reports += 1 }
        }
        XCTAssertEqual(reports, 3)
    }

    func testProgressOnPauseAndSeek() {
        var t = ProgressTrigger()
        XCTAssertFalse(t.tick(playing: true, positionMs: 1000))
        XCTAssertFalse(t.tick(playing: true, positionMs: 2000))
        XCTAssertTrue(t.tick(playing: false, positionMs: 2500), "paused")
        XCTAssertFalse(t.tick(playing: false, positionMs: 2500), "still paused")
        XCTAssertTrue(t.tick(playing: false, positionMs: 60_000), "seek while paused")
        XCTAssertFalse(t.tick(playing: true, positionMs: 60_000))
        XCTAssertTrue(t.tick(playing: true, positionMs: 10_000), "seek back while playing")
    }

    func testTapsOnTransportKeepControls() {
        let size = CGSize(width: 400, height: 800)
        XCTAssertEqual(ControlsTap.action(at: CGPoint(x: 200, y: 400), in: size), .keep, "centre play button")
        XCTAssertEqual(ControlsTap.action(at: CGPoint(x: 200, y: 760), in: size), .keep, "scrubber")
        XCTAssertEqual(ControlsTap.action(at: CGPoint(x: 60, y: 250), in: size), .toggle, "open picture")
    }
}
