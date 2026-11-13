import Foundation
import Observation

struct SavedServer: Codable, Identifiable, Equatable {
    var id: String
    var name: String
    var url: URL
    var version: String
}

/// Persists known servers and which one is in use. Credentials go to the Keychain (one token
/// per server), never here, so switching servers keeps each sign-in.
@MainActor
@Observable
final class ServerStore {
    private(set) var servers: [SavedServer] = []
    private(set) var currentID: String?
    var current: SavedServer? { servers.first { $0.id == currentID } ?? servers.first }
    private let key = "streamero.servers"
    private let currentKey = "streamero.currentServer"
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        if let data = defaults.data(forKey: key),
           let saved = try? JSONDecoder().decode([SavedServer].self, from: data) {
            servers = saved
        }
        currentID = defaults.string(forKey: currentKey)
    }

    /// Adds (or refreshes) a server and switches to it.
    func add(_ server: SavedServer) {
        if let i = servers.firstIndex(where: { $0.id == server.id }) {
            servers[i] = server
        } else {
            servers.append(server)
        }
        select(server)
        save()
    }

    func select(_ server: SavedServer) {
        guard servers.contains(where: { $0.id == server.id }) else { return }
        currentID = server.id
        defaults.set(server.id, forKey: currentKey)
    }

    func remove(_ server: SavedServer) {
        servers.removeAll { $0.id == server.id }
        if currentID == server.id {
            currentID = servers.first?.id
            defaults.set(currentID, forKey: currentKey)
        }
        save()
    }

    private func save() {
        if let data = try? JSONEncoder().encode(servers) { defaults.set(data, forKey: key) }
    }
}
