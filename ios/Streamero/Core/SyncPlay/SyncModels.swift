import Foundation

// Watch together (SyncPlay, D-036): models mirroring the `syncplay` schemas in api/openapi.yaml.
// Times are the server's clock in Unix milliseconds.

struct SyncQueueEntry: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var itemId: String
    var title: String
    var durationMs: Int64?
}

struct SyncMember: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var name: String
    var device: String
    var ready: Bool
    var buffering: Bool
    var connected: Bool
    var lagging: Bool
}

struct SyncGroup: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var name: String
    /// idle, waiting, paused or playing.
    var state: String
    var queue: [SyncQueueEntry]
    /// Current queue entry (-1 = nothing).
    var index: Int
    var positionMs: Int64
    var anchorAt: Int64
    var speed: Double
    var version: Int64
    var reason: String?
    var actor: String?
    var members: [SyncMember]
    var serverTime: Int64
    var createdAt: String
    var selfId: String?

    var currentEntry: SyncQueueEntry? {
        queue.indices.contains(index) ? queue[index] : nil
    }

    var selfMember: SyncMember? {
        guard let selfId else { return nil }
        return members.first { $0.id == selfId }
    }

    /// The group wants to play (playing, or waiting for members to get ready).
    var wantsPlay: Bool { state == "playing" || state == "waiting" }

    /// Connected members the group is waiting for (loading or buffering), without this device.
    var waitingFor: [SyncMember] {
        guard state == "waiting" else { return [] }
        return members.filter { $0.connected && !$0.lagging && (!$0.ready || $0.buffering) && $0.id != selfId }
    }
}

extension SyncGroup {
    /// Tolerant decoding: the server may send `null` for an empty queue or member list.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        state = try c.decode(String.self, forKey: .state)
        queue = try c.decodeIfPresent([SyncQueueEntry].self, forKey: .queue) ?? []
        index = try c.decodeIfPresent(Int.self, forKey: .index) ?? -1
        positionMs = try c.decodeIfPresent(Int64.self, forKey: .positionMs) ?? 0
        anchorAt = try c.decodeIfPresent(Int64.self, forKey: .anchorAt) ?? 0
        speed = try c.decodeIfPresent(Double.self, forKey: .speed) ?? 1
        version = try c.decodeIfPresent(Int64.self, forKey: .version) ?? 0
        reason = try c.decodeIfPresent(String.self, forKey: .reason)
        actor = try c.decodeIfPresent(String.self, forKey: .actor)
        members = try c.decodeIfPresent([SyncMember].self, forKey: .members) ?? []
        serverTime = try c.decodeIfPresent(Int64.self, forKey: .serverTime) ?? 0
        createdAt = try c.decodeIfPresent(String.self, forKey: .createdAt) ?? ""
        selfId = try c.decodeIfPresent(String.self, forKey: .selfId)
    }
}

struct SyncCorrection: Codable, Sendable, Equatable {
    /// rate or seek.
    var mode: String
    var rate: Double?
    var durationMs: Int64?
    var positionMs: Int64?
    var driftMs: Int64
}

struct CreateSyncGroupRequest: Codable, Sendable, Equatable {
    var name: String?
    var itemIds: [String]?
    var index: Int?
    var startMs: Int64?
}

/// A socket message to the server; nil fields are left out.
struct SyncClientMessage: Codable, Sendable, Equatable {
    var type: String
    var clientTime: Int64?
    var positionMs: Int64?
    var speed: Double?
    var itemIds: [String]?
    var index: Int?
    var startMs: Int64?
    var entryId: String?
    var at: Int64?
    var playing: Bool?

    init(type: String, clientTime: Int64? = nil, positionMs: Int64? = nil, speed: Double? = nil,
         itemIds: [String]? = nil, index: Int? = nil, startMs: Int64? = nil, entryId: String? = nil,
         at: Int64? = nil, playing: Bool? = nil) {
        self.type = type
        self.clientTime = clientTime
        self.positionMs = positionMs
        self.speed = speed
        self.itemIds = itemIds
        self.index = index
        self.startMs = startMs
        self.entryId = entryId
        self.at = at
        self.playing = playing
    }

    static func ping(clientTime: Int64) -> SyncClientMessage { SyncClientMessage(type: "ping", clientTime: clientTime) }
    static let play = SyncClientMessage(type: "play")
    static let pause = SyncClientMessage(type: "pause")
    static func seek(_ positionMs: Int64) -> SyncClientMessage { SyncClientMessage(type: "seek", positionMs: max(0, positionMs)) }
    static func speed(_ speed: Double) -> SyncClientMessage { SyncClientMessage(type: "speed", speed: speed) }
    static func next(entryId: String) -> SyncClientMessage { SyncClientMessage(type: "next", entryId: entryId) }
    static func previous(entryId: String) -> SyncClientMessage { SyncClientMessage(type: "previous", entryId: entryId) }
    static func setQueue(itemIds: [String], index: Int, startMs: Int64) -> SyncClientMessage {
        SyncClientMessage(type: "setQueue", itemIds: itemIds, index: index, startMs: startMs)
    }
    static func ready(entryId: String, positionMs: Int64, at: Int64) -> SyncClientMessage {
        SyncClientMessage(type: "ready", positionMs: max(0, positionMs), entryId: entryId, at: at)
    }
    static func buffering(entryId: String, positionMs: Int64, at: Int64) -> SyncClientMessage {
        SyncClientMessage(type: "buffering", positionMs: max(0, positionMs), entryId: entryId, at: at)
    }
    static func position(entryId: String, positionMs: Int64, at: Int64) -> SyncClientMessage {
        SyncClientMessage(type: "position", positionMs: max(0, positionMs), entryId: entryId, at: at, playing: true)
    }
}

/// A socket message from the server.
struct SyncServerMessage: Codable, Sendable, Equatable {
    /// hello, pong, state, correction or error.
    var type: String
    var serverTime: Int64
    var clientTime: Int64?
    var group: SyncGroup?
    var reason: String?
    var correction: SyncCorrection?
    var code: String?
    var message: String?
}

/// Invite links: `https://<server>/together/<groupId>` or the bare id.
enum SyncInvite {
    /// The group id inside a pasted link or id (lowercased), or nil.
    static func groupId(from text: String) -> String? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        let parts = trimmed.split(whereSeparator: { "/?#&=".contains($0) })
        // Prefer the segment after "together", else the last UUID in the text.
        if let i = parts.firstIndex(of: "together"), parts.indices.contains(i + 1),
           let id = UUID(uuidString: String(parts[i + 1])) {
            return id.uuidString.lowercased()
        }
        for part in parts.reversed() {
            if let id = UUID(uuidString: String(part)) { return id.uuidString.lowercased() }
        }
        return nil
    }

    /// The link to share for a group on a server.
    static func link(server: URL, groupId: String) -> URL {
        server.appending(path: "together").appending(path: groupId)
    }
}
