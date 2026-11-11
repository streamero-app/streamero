import XCTest
@testable import Streamero

/// Watch together (D-036): clock, shared position, the player decisions and corrections.
final class SyncEngineTests: XCTestCase {
    private let now: Int64 = 1_700_000_000_000

    private func member(_ id: String, ready: Bool, buffering: Bool = false, connected: Bool = true) -> SyncMember {
        SyncMember(id: id, name: id.capitalized, device: "Phone", ready: ready, buffering: buffering, connected: connected, lagging: false)
    }

    private func group(state: String, positionMs: Int64 = 0, anchorAt: Int64? = nil, speed: Double = 1,
                       version: Int64 = 1, meReady: Bool = true, others: [SyncMember] = []) -> SyncGroup {
        SyncGroup(id: "g1", name: "Movie night", state: state,
                  queue: [SyncQueueEntry(id: "e1", itemId: "item-a", title: "Alpha", durationMs: 6_000_000),
                          SyncQueueEntry(id: "e2", itemId: "item-b", title: "Beta", durationMs: nil)],
                  index: 0, positionMs: positionMs, anchorAt: anchorAt ?? now, speed: speed, version: version,
                  reason: nil, actor: nil, members: [member("me", ready: meReady)] + others,
                  serverTime: now, createdAt: "2026-11-11T10:00:00Z", selfId: "me")
    }

    private func local(at pos: Int64, playing: Bool, item: String = "item-a", rate: Double = 1, stalled: Bool = false) -> LocalPlayback {
        LocalPlayback(itemId: item, loaded: true, positionMs: pos, playing: playing, rate: playing ? rate : 0, stalled: stalled)
    }

    private func sent(_ actions: [SyncAction]) -> [SyncClientMessage] {
        var out: [SyncClientMessage] = []
        for action in actions {
            if case .send(let message) = action { out.append(message) }
        }
        return out
    }

    /// An engine whose clock says server time == local time.
    private func engine(_ g: SyncGroup) -> SyncEngine {
        var e = SyncEngine()
        e.clock.add(sent: now - 50, serverTime: now - 25, received: now)
        e.apply(g)
        return e
    }

    // MARK: Clock

    func testClockKeepsTheSampleWithTheSmallestRoundTrip() {
        var clock = SyncClock()
        XCTAssertFalse(clock.hasSample)
        XCTAssertEqual(clock.offset, 0)
        clock.add(sent: 1000, serverTime: 5000, received: 1400) // rtt 400, offset 3800
        clock.add(sent: 2000, serverTime: 4050, received: 2100) // rtt 100, offset 2000
        clock.add(sent: 3000, serverTime: 9000, received: 3300) // rtt 300, offset 5850
        XCTAssertTrue(clock.hasSample)
        XCTAssertEqual(clock.offset, 2000)
        XCTAssertEqual(clock.serverTime(local: 10_000), 12_000)
        XCTAssertEqual(clock.localTime(server: 12_000), 10_000)
    }

    func testClockIgnoresNegativeRoundTripsAndKeepsRecentSamples() {
        var clock = SyncClock()
        clock.add(sent: 2000, serverTime: 9999, received: 1000)
        XCTAssertFalse(clock.hasSample)
        for i in 0..<20 { clock.add(sent: Int64(i * 100), serverTime: 0, received: Int64(i * 100 + 10)) }
        XCTAssertEqual(clock.samples.count, clock.maxSamples)
    }

    // MARK: Shared position

    func testSharedPositionWhilePlayingAndPaused() {
        let playing = group(state: "playing", positionMs: 10_000, anchorAt: now, speed: 1.5)
        XCTAssertEqual(SyncMath.position(playing, at: now + 2000), 13_000)
        // An anchor in the future: start from positionMs exactly then.
        XCTAssertEqual(SyncMath.position(playing, at: now - 500), 10_000)
        let paused = group(state: "paused", positionMs: 10_000, anchorAt: now)
        XCTAssertEqual(SyncMath.position(paused, at: now + 60_000), 10_000)
        // Never past the end of the entry.
        XCTAssertEqual(SyncMath.position(playing, at: now + 10_000_000), 6_000_000)
    }

    // MARK: Decisions

    func testOpensTheGroupItemOnce() {
        var e = engine(group(state: "paused", positionMs: 42_000))
        let actions = e.reconcile(local(at: 0, playing: true, item: "other"), now: now)
        XCTAssertEqual(actions, [.open(itemId: "item-a", startMs: 42_000)])
        XCTAssertEqual(e.reconcile(local(at: 0, playing: true, item: "other"), now: now + 250), [])
    }

    func testNothingHappensUntilTheItemIsLoaded() {
        var e = engine(group(state: "playing", meReady: false))
        let loading = LocalPlayback(itemId: "item-a", loaded: false, positionMs: 0, playing: false)
        XCTAssertEqual(e.reconcile(loading, now: now), [])
    }

    func testPausedGroupPausesSeeksThenReportsReady() {
        var e = engine(group(state: "paused", positionMs: 60_000, meReady: false))
        XCTAssertEqual(e.reconcile(local(at: 0, playing: true), now: now), [.pause, .seek(60_000)])
        let next = e.reconcile(local(at: 60_000, playing: false), now: now + 300)
        XCTAssertEqual(next, [.send(.ready(entryId: "e1", positionMs: 60_000, at: now + 300))])
        // Not sent again for the same state…
        XCTAssertEqual(e.reconcile(local(at: 60_000, playing: false), now: now + 600), [])
        // …but again after a while if the server did not take it.
        XCTAssertEqual(sent(e.reconcile(local(at: 60_000, playing: false), now: now + 6000)).map(\.type), ["ready"])
    }

    func testReadyPlayerAtThePositionSendsNothing() {
        var e = engine(group(state: "paused", positionMs: 60_000))
        XCTAssertEqual(e.reconcile(local(at: 60_400, playing: false), now: now), [])
    }

    func testScheduledStartUsesTheClockOffset() {
        var e = SyncEngine()
        e.clock.add(sent: now - 100, serverTime: now + 1950, received: now) // offset 2000
        e.apply(group(state: "playing", positionMs: 5000, anchorAt: now + 2000 + 600))
        let actions = e.reconcile(local(at: 5000, playing: false), now: now)
        XCTAssertEqual(actions, [.startAt(localMs: now + 600, positionMs: 5000, rate: 1)])
        // Scheduled once; while the start is pending nothing else happens.
        XCTAssertEqual(e.reconcile(local(at: 5000, playing: false), now: now + 200), [])
    }

    func testPlayingGroupCatchesUpAStoppedPlayer() {
        var e = engine(group(state: "playing", positionMs: 0, anchorAt: now - 10_000))
        let actions = e.reconcile(local(at: 0, playing: false), now: now)
        XCTAssertEqual(actions, [.seek(10_000), .play(rate: 1)])
    }

    func testNewStateSeeksOnlyWhenFarOff() {
        var e = engine(group(state: "playing", positionMs: 0, anchorAt: now - 10_000))
        XCTAssertFalse(e.reconcile(local(at: 9_500, playing: true), now: now).contains(.seek(10_000)))
        e.apply(group(state: "playing", positionMs: 30_000, anchorAt: now, version: 2))
        XCTAssertTrue(e.reconcile(local(at: 10_000, playing: true), now: now).contains(.seek(30_000)))
    }

    func testFollowsTheGroupSpeed() {
        var e = engine(group(state: "playing", positionMs: 0, anchorAt: now - 1000, speed: 1.25))
        let actions = e.reconcile(local(at: 1250, playing: true, rate: 1), now: now)
        XCTAssertTrue(actions.contains(.setRate(1.25)))
    }

    func testPositionReportsAboutOncePerSecond() {
        var e = engine(group(state: "playing", positionMs: 0, anchorAt: now - 5000))
        let first = sent(e.reconcile(local(at: 5000, playing: true), now: now))
        XCTAssertEqual(first, [.position(entryId: "e1", positionMs: 5000, at: now)])
        XCTAssertEqual(first.first?.playing, true)
        XCTAssertTrue(sent(e.reconcile(local(at: 5500, playing: true), now: now + 500)).isEmpty)
        XCTAssertEqual(sent(e.reconcile(local(at: 6000, playing: true), now: now + 1000)).map(\.type), ["position"])
    }

    func testNoPositionReportsWithoutAClockSample() {
        var e = SyncEngine()
        e.apply(group(state: "playing", positionMs: 0, anchorAt: now - 5000))
        XCTAssertTrue(sent(e.reconcile(local(at: 5000, playing: true), now: now)).isEmpty)
    }

    func testBufferingIsReportedAfterAStallLasts() {
        var e = engine(group(state: "playing", positionMs: 0, anchorAt: now - 5000))
        XCTAssertTrue(sent(e.reconcile(local(at: 5000, playing: true, stalled: true), now: now)).isEmpty)
        let later = sent(e.reconcile(local(at: 5000, playing: true, stalled: true), now: now + 1600))
        XCTAssertEqual(later, [.buffering(entryId: "e1", positionMs: 5000, at: now + 1600)])
        XCTAssertTrue(sent(e.reconcile(local(at: 5000, playing: true, stalled: true), now: now + 2000)).isEmpty)
    }

    func testEndOfItemAsksForTheNextEntryOnce() {
        var e = engine(group(state: "playing", positionMs: 0, anchorAt: now - 5000))
        var end = local(at: 6_000_000, playing: false)
        end.ended = true
        XCTAssertEqual(e.reconcile(end, now: now), [.pause, .send(.next(entryId: "e1"))])
        XCTAssertFalse(sent(e.reconcile(end, now: now + 300)).contains { $0.type == "next" })
    }

    func testOlderVersionsAreIgnored() {
        var e = engine(group(state: "playing", version: 5))
        e.apply(group(state: "paused", version: 4))
        XCTAssertEqual(e.group?.state, "playing")
        e.apply(nil)
        XCTAssertNil(e.group)
    }

    // MARK: Corrections

    func testRateCorrectionEndsAtTheGroupSpeed() {
        var e = engine(group(state: "playing", positionMs: 0, anchorAt: now - 5000))
        _ = e.reconcile(local(at: 5000, playing: true), now: now)
        let c = SyncCorrection(mode: "rate", rate: 1.05, durationMs: 2000, positionMs: nil, driftMs: -100)
        XCTAssertEqual(e.correction(c, serverTime: now, now: now), [.setRate(1.05)])
        XCTAssertTrue(e.correcting(now: now + 500))
        XCTAssertFalse(e.reconcile(local(at: 5500, playing: true, rate: 1.05), now: now + 500).contains(.setRate(1)))
        XCTAssertTrue(e.reconcile(local(at: 7100, playing: true, rate: 1.05), now: now + 2100).contains(.setRate(1)))
    }

    func testSeekCorrectionAddsTheTimeSinceItWasSent() {
        var e = engine(group(state: "playing", positionMs: 0, anchorAt: now - 50_000))
        let c = SyncCorrection(mode: "seek", rate: nil, durationMs: nil, positionMs: 50_000, driftMs: 3000)
        XCTAssertEqual(e.correction(c, serverTime: now - 200, now: now), [.seek(50_200), .setRate(1)])
    }

    func testCorrectionsAreIgnoredWhenNotPlaying() {
        var e = engine(group(state: "paused"))
        let c = SyncCorrection(mode: "rate", rate: 1.05, durationMs: 2000, positionMs: nil, driftMs: -100)
        XCTAssertEqual(e.correction(c, serverTime: now, now: now), [])
    }

    // MARK: Wire format and invites

    func testDecodesStateWithNullQueue() throws {
        let json = """
        {"type":"state","serverTime":5,"group":{"id":"g","name":"","state":"idle","queue":null,"index":-1,
        "positionMs":0,"anchorAt":0,"speed":1,"version":1,"members":[],"serverTime":5,"createdAt":"2026-11-11T10:00:00Z"}}
        """
        let msg = try JSONDecoder().decode(SyncServerMessage.self, from: Data(json.utf8))
        XCTAssertEqual(msg.group?.queue, [])
        XCTAssertNil(msg.group?.currentEntry)
        XCTAssertNil(msg.group?.selfId)
    }

    func testClientMessagesLeaveOutUnsetFields() throws {
        let data = try JSONEncoder().encode(SyncClientMessage.ready(entryId: "e1", positionMs: 1200, at: 99))
        let body = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(body["type"] as? String, "ready")
        XCTAssertEqual(body["positionMs"] as? Int, 1200)
        XCTAssertEqual(body["at"] as? Int, 99)
        XCTAssertNil(body["speed"])
        XCTAssertNil(body["playing"])
    }

    func testWaitingForNamesOtherMembers() {
        let g = group(state: "waiting", others: [member("ben", ready: false), member("ana", ready: true, buffering: true),
                                                 member("off", ready: false, connected: false)])
        XCTAssertEqual(g.waitingFor.map(\.id), ["ben", "ana"])
    }

    func testParsesInviteLinksAndIds() {
        let id = "3F2504E0-4F89-11D3-9A0C-0305E82C3301"
        XCTAssertEqual(SyncInvite.groupId(from: "https://demo.example.net/together/\(id)"), id.lowercased())
        XCTAssertEqual(SyncInvite.groupId(from: "  \(id.lowercased())\n"), id.lowercased())
        XCTAssertEqual(SyncInvite.groupId(from: "http://192.168.1.5:8787/together/\(id)?x=1"), id.lowercased())
        XCTAssertNil(SyncInvite.groupId(from: "https://demo.example.net/together/nope"))
        XCTAssertNil(SyncInvite.groupId(from: ""))
        XCTAssertEqual(SyncInvite.link(server: URL(string: "https://demo.example.net")!, groupId: "abc").absoluteString,
                       "https://demo.example.net/together/abc")
    }
}
