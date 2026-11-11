import Foundation
import Observation

/// Something the player reacts to.
enum SyncEvent: Sendable {
    /// A new group state arrived.
    case state
    /// Drift correction; `serverTime` is when the server sent it.
    case correction(SyncCorrection, serverTime: Int64)
    /// This device is no longer in the group (left, removed, closed).
    case ended(reason: String?)
}

/// Watch-together membership for the signed-in session: REST calls for groups, the socket
/// (`/api/v1/syncplay/socket`) with clock pings, reconnects with backoff, and the sync engine.
@MainActor
@Observable
final class SyncSession {
    let client: MediaClient
    private(set) var group: SyncGroup?
    private(set) var connected = false

    @ObservationIgnored private var engine = SyncEngine()
    @ObservationIgnored private var runner: Task<Void, Never>?
    @ObservationIgnored private var socket: URLSessionWebSocketTask?
    /// The attached player; nil while no player follows the group.
    @ObservationIgnored var listener: (@MainActor (SyncEvent) -> Void)?

    init(client: MediaClient) {
        self.client = client
    }

    static func nowMs() -> Int64 { Int64((Date().timeIntervalSince1970 * 1000).rounded()) }

    // MARK: Groups

    func groups() async throws -> [SyncGroup] { try await client.syncGroups() }

    @discardableResult
    func create(itemIds: [String], startMs: Int64) async throws -> SyncGroup {
        let g = try await client.createSyncGroup(CreateSyncGroupRequest(name: nil, itemIds: itemIds, index: 0, startMs: max(0, startMs)))
        apply(g)
        return g
    }

    @discardableResult
    func join(_ id: String) async throws -> SyncGroup {
        let g = try await client.joinSyncGroup(id)
        apply(g)
        return g
    }

    /// Picks up the group this session is in (after a restart or reconnect).
    func refresh() async {
        do {
            let g = try await client.mySyncGroup()
            if g == nil, group != nil {
                apply(nil)
                listener?(.ended(reason: "closed"))
            } else if g != nil {
                apply(g)
            }
        } catch {}
    }

    func leave() async {
        try? await client.leaveSyncGroup()
        apply(nil)
    }

    /// The shared position now (nil outside a group).
    var sharedPosition: Int64? { engine.sharedPosition(now: Self.nowMs()) }

    var serverURL: URL { client.baseURL }

    // MARK: Commands

    func play() { send(.play) }
    func pause() { send(.pause) }
    func seek(_ ms: Int64) { send(.seek(ms)) }

    func next() {
        guard let entry = group?.currentEntry else { return }
        send(.next(entryId: entry.id))
    }

    func previous() {
        guard let entry = group?.currentEntry else { return }
        send(.previous(entryId: entry.id))
    }

    var hasNext: Bool {
        guard let g = group else { return false }
        return g.index >= 0 && g.index + 1 < g.queue.count
    }

    // MARK: Engine

    func reconcile(_ local: LocalPlayback) -> [SyncAction] {
        engine.reconcile(local, now: Self.nowMs())
    }

    func correction(_ c: SyncCorrection, serverTime: Int64) -> [SyncAction] {
        engine.correction(c, serverTime: serverTime, now: Self.nowMs())
    }

    func realign() { engine.realign() }

    func correcting() -> Bool { engine.correcting(now: Self.nowMs()) }

    private func apply(_ g: SyncGroup?) {
        engine.apply(g)
        group = engine.group
        if group != nil {
            if runner == nil { startSocket() }
        } else {
            stopSocket()
        }
    }

    // MARK: Socket

    func send(_ message: SyncClientMessage) {
        guard let socket, let data = try? JSONEncoder().encode(message) else { return }
        let text = String(decoding: data, as: UTF8.self)
        Task { await SyncSession.sendText(text, on: socket) }
    }

    private var socketURL: URL? {
        let url = ServerURL.apiBase(client.baseURL).appending(path: "syncplay/socket")
        guard var comps = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
        comps.scheme = comps.scheme?.lowercased() == "http" ? "ws" : "wss"
        return comps.url
    }

    private func startSocket() {
        runner?.cancel()
        runner = Task { [weak self] in
            var attempt = 0
            while !Task.isCancelled {
                guard let self, self.group != nil, let url = self.socketURL else { return }
                var req = URLRequest(url: url)
                req.setValue("Bearer \(self.client.token)", forHTTPHeaderField: "Authorization")
                let task = URLSession.shared.webSocketTask(with: req)
                self.socket = task
                task.resume()
                let pinger = Task { [weak self] in
                    while !Task.isCancelled {
                        self?.send(.ping(clientTime: SyncSession.nowMs()))
                        try? await Task.sleep(for: .seconds(5))
                    }
                }
                while !Task.isCancelled {
                    guard let text = await SyncSession.receiveText(task) else { break }
                    if !self.connected { self.connected = true }
                    attempt = 0
                    self.handle(text)
                }
                pinger.cancel()
                task.cancel(with: .goingAway, reason: nil)
                if self.socket === task { self.socket = nil }
                self.connected = false
                if Task.isCancelled || self.group == nil { return }
                attempt += 1
                let delay = min(30.0, pow(2.0, Double(min(attempt, 5))))
                try? await Task.sleep(for: .seconds(delay))
            }
        }
    }

    private func stopSocket() {
        runner?.cancel()
        runner = nil
        socket?.cancel(with: .goingAway, reason: nil)
        socket = nil
        connected = false
    }

    private func handle(_ text: String) {
        guard let msg = try? JSONDecoder().decode(SyncServerMessage.self, from: Data(text.utf8)) else { return }
        switch msg.type {
        case "hello":
            send(.ping(clientTime: Self.nowMs()))
            // After a reconnect the server only sends a state when the membership survived
            // (a restarted server forgets its groups); ask explicitly.
            Task { await self.refresh() }
        case "pong":
            if let sent = msg.clientTime {
                engine.clock.add(sent: sent, serverTime: msg.serverTime, received: Self.nowMs())
            }
        case "state":
            if let g = msg.group {
                engine.apply(g)
                group = engine.group
                listener?(.state)
            } else {
                apply(nil)
                listener?(.ended(reason: msg.reason))
            }
        case "correction":
            if let c = msg.correction { listener?(.correction(c, serverTime: msg.serverTime)) }
        default:
            break
        }
    }

    /// Reads one text message; nil when the socket closed or failed.
    nonisolated private static func receiveText(_ task: URLSessionWebSocketTask) async -> String? {
        do {
            switch try await task.receive() {
            case .string(let s): return s
            case .data(let d): return String(decoding: d, as: UTF8.self)
            @unknown default: return ""
            }
        } catch {
            return nil
        }
    }

    nonisolated private static func sendText(_ text: String, on task: URLSessionWebSocketTask) async {
        try? await task.send(.string(text))
    }
}
