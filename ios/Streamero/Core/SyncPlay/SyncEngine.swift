import Foundation

// The pure part of watch together (D-036): clock offset, shared position and what the local
// player has to do for the latest group state. No AVFoundation and no I/O, so it is unit-tested;
// SyncSession owns the socket and PlayerModel executes the returned actions.

/// Estimates the offset between this device's clock and the server's from ping/pong pairs:
/// offset = serverTime - (t0 + t1) / 2, keeping the sample with the smallest round trip.
struct SyncClock: Sendable, Equatable {
    struct Sample: Sendable, Equatable {
        var offset: Int64
        var rtt: Int64
    }

    private(set) var samples: [Sample] = []
    var maxSamples = 8

    /// Adds a pong: `sent` and `received` are local ms, `serverTime` the server's ms.
    mutating func add(sent: Int64, serverTime: Int64, received: Int64) {
        let rtt = received - sent
        guard rtt >= 0, rtt < 30_000 else { return }
        samples.append(Sample(offset: serverTime - (sent + received) / 2, rtt: rtt))
        if samples.count > maxSamples { samples.removeFirst(samples.count - maxSamples) }
    }

    var hasSample: Bool { !samples.isEmpty }

    /// Server minus local clock in ms (0 until the first pong).
    var offset: Int64 {
        samples.min { $0.rtt < $1.rtt }?.offset ?? 0
    }

    func serverTime(local: Int64) -> Int64 { local + offset }
    func localTime(server: Int64) -> Int64 { server - offset }
}

enum SyncMath {
    /// The group's position at server time `now`.
    static func position(_ g: SyncGroup, at now: Int64) -> Int64 {
        var pos = g.positionMs
        if g.state == "playing" {
            pos += Int64((Double(max(0, now - g.anchorAt)) * g.speed).rounded())
        }
        if let d = g.currentEntry?.durationMs, d > 0 { pos = min(pos, d) }
        return max(0, pos)
    }
}

/// What the local player looks like right now.
struct LocalPlayback: Sendable, Equatable {
    /// Item loaded (or being loaded) in the player.
    var itemId: String?
    /// The item is ready to play.
    var loaded: Bool
    var positionMs: Int64
    /// The player intends to play (rate is not 0).
    var playing: Bool
    var rate: Double = 1
    /// Waiting for data while it intends to play.
    var stalled: Bool = false
    /// A seek or load is running.
    var seeking: Bool = false
    /// Reached the end of the item.
    var ended: Bool = false
}

/// What the player has to do.
enum SyncAction: Sendable, Equatable {
    /// Open another item at a position (the regular playback start).
    case open(itemId: String, startMs: Int64)
    case seek(Int64)
    /// Play at a rate (the group speed).
    case play(rate: Double)
    case pause
    /// Change the rate only while playing (drift correction, speed change).
    case setRate(Double)
    /// Pause at `positionMs` and start playing at `rate` at local time `localMs`.
    case startAt(localMs: Int64, positionMs: Int64, rate: Double)
    /// Send a report to the server.
    case send(SyncClientMessage)
}

struct SyncEngine: Sendable {
    /// Local position may differ this much from the shared one before a new state seeks.
    static let tolerance: Int64 = 1000
    /// A stall must last this long before it is reported as buffering.
    static let stallGrace: Int64 = 1500
    static let positionInterval: Int64 = 1000
    static let readyRetry: Int64 = 5000

    var clock = SyncClock()
    private(set) var group: SyncGroup?

    private var needsAlign = true
    private var openedEntry: String?
    private var scheduledAnchor: Int64?
    private var rateUntil: Int64?
    private var readySent: (version: Int64, at: Int64)?
    private var bufferingSentVersion: Int64?
    private var stallSince: Int64?
    private var lastPositionSent: Int64?
    private var endedSentEntry: String?

    init() {}

    var selfId: String? { group?.selfId }

    /// Takes a new group state (nil = not in a group). A new version asks for re-alignment.
    mutating func apply(_ g: SyncGroup?) {
        guard let g else {
            group = nil
            reset()
            return
        }
        if group?.id != g.id { reset() }
        let old = group
        // REST answers and socket states may cross; never go back to an older version of the same group.
        if let old, old.id == g.id, g.version < old.version { return }
        if old?.version != g.version || old?.id != g.id { needsAlign = true }
        if let old, old.speed != g.speed { rateUntil = nil }
        if old?.currentEntry?.id != g.currentEntry?.id { openedEntry = nil }
        group = g
    }

    /// Re-align after the player reloaded on its own (audio track change, new stream).
    mutating func realign() { needsAlign = true }

    private mutating func reset() {
        needsAlign = true
        openedEntry = nil
        scheduledAnchor = nil
        rateUntil = nil
        readySent = nil
        bufferingSentVersion = nil
        stallSince = nil
        lastPositionSent = nil
        endedSentEntry = nil
    }

    /// The shared position now, from local time `now`.
    func sharedPosition(now: Int64) -> Int64? {
        guard let group else { return nil }
        return SyncMath.position(group, at: clock.serverTime(local: now))
    }

    /// Whether a rate correction is running at local time `now`.
    func correcting(now: Int64) -> Bool { rateUntil.map { now < $0 } ?? false }

    /// What the player does now for the latest state; `now` is local Unix ms.
    mutating func reconcile(_ local: LocalPlayback, now: Int64) -> [SyncAction] {
        guard let g = group else { return [] }
        guard let entry = g.currentEntry else {
            return local.playing ? [.pause] : []
        }
        let serverNow = clock.serverTime(local: now)
        let target = SyncMath.position(g, at: serverNow)
        if local.itemId != entry.itemId {
            guard openedEntry != entry.id else { return [] }
            openedEntry = entry.id
            needsAlign = true
            return [.open(itemId: entry.itemId, startMs: target)]
        }
        guard local.loaded, !local.seeking else { return [] }

        var actions: [SyncAction] = []
        var seeked = false
        let off = abs(local.positionMs - target)
        let me = g.selfMember

        if local.ended && g.state != "idle" && endedSentEntry != entry.id {
            endedSentEntry = entry.id
            needsAlign = false
            return [.pause, .send(.next(entryId: entry.id))]
        }
        // Asked for the next entry: wait for the group to move on instead of starting over.
        if local.ended && endedSentEntry == entry.id && g.state != "idle" { return [] }

        if !local.stalled { stallSince = nil }

        switch g.state {
        case "playing" where g.anchorAt > serverNow:
            // A synchronised start: everyone begins at positionMs at anchorAt.
            if scheduledAnchor != g.anchorAt {
                scheduledAnchor = g.anchorAt
                actions.append(.startAt(localMs: clock.localTime(server: g.anchorAt), positionMs: g.positionMs, rate: g.speed))
                seeked = true
            }
            needsAlign = false
        case "playing":
            let startPending = scheduledAnchor == g.anchorAt && serverNow < g.anchorAt + 500
            if !local.playing {
                if !startPending {
                    if off > Self.tolerance {
                        actions.append(.seek(target))
                        seeked = true
                    }
                    actions.append(.play(rate: g.speed))
                }
            } else {
                if needsAlign && off > Self.tolerance {
                    actions.append(.seek(target))
                    seeked = true
                }
                if let until = rateUntil, now >= until {
                    rateUntil = nil
                    actions.append(.setRate(g.speed))
                } else if rateUntil == nil && abs(local.rate - g.speed) > 0.001 {
                    actions.append(.setRate(g.speed))
                }
            }
            needsAlign = false
            // Buffering: only a ready player that stalls for a while.
            if local.playing && local.stalled && me?.ready == true {
                let since = stallSince ?? now
                stallSince = since
                if now - since >= Self.stallGrace && bufferingSentVersion != g.version {
                    bufferingSentVersion = g.version
                    actions.append(.send(.buffering(entryId: entry.id, positionMs: local.positionMs, at: serverNow)))
                }
            }
            // Drift reports about once a second.
            if !seeked && local.playing && !local.stalled && me?.ready == true && clock.hasSample && serverNow >= g.anchorAt
                && (lastPositionSent.map { now - $0 >= Self.positionInterval } ?? true) {
                lastPositionSent = now
                actions.append(.send(.position(entryId: entry.id, positionMs: local.positionMs, at: serverNow)))
            }
        default:
            // paused, waiting, idle: stand still at the group position.
            if local.playing { actions.append(.pause) }
            if needsAlign && g.state != "idle" && abs(local.positionMs - g.positionMs) > Self.tolerance {
                actions.append(.seek(g.positionMs))
                seeked = true
            }
            needsAlign = false
            scheduledAnchor = nil
            rateUntil = nil
        }

        // Ready once the entry is loaded at the shared position (after any seek above has finished).
        if g.state != "idle", let me, !me.ready, !seeked, !local.stalled {
            let due: Bool
            if let sent = readySent {
                due = sent.version != g.version || now - sent.at >= Self.readyRetry
            } else {
                due = true
            }
            if due {
                readySent = (g.version, now)
                actions.append(.send(.ready(entryId: entry.id, positionMs: local.positionMs, at: serverNow)))
            }
        }
        return actions
    }

    /// Drift correction from the server; `serverTime` is when it was sent, `now` local ms.
    mutating func correction(_ c: SyncCorrection, serverTime: Int64, now: Int64) -> [SyncAction] {
        guard let g = group, g.state == "playing" else { return [] }
        switch c.mode {
        case "rate":
            guard let rate = c.rate, rate > 0, let duration = c.durationMs, duration > 0 else { return [] }
            rateUntil = now + duration
            return [.setRate(rate)]
        case "seek":
            guard let pos = c.positionMs else { return [] }
            rateUntil = nil
            let elapsed = max(0, clock.serverTime(local: now) - serverTime)
            return [.seek(pos + Int64((Double(elapsed) * g.speed).rounded())), .setRate(g.speed)]
        default:
            return []
        }
    }
}
