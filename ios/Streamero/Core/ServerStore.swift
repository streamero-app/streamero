import Foundation
import Observation

struct SavedServer: Codable, Identifiable, Equatable {
    var id: String
    var name: String
    var url: URL
    var version: String
}

/// Persists known servers. Credentials (P1) go to the Keychain, never here.
@MainActor
@Observable
final class ServerStore {
    private(set) var servers: [SavedServer] = []
    var current: SavedServer? { servers.first }
    private let key = "streamero.servers"
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        if let data = defaults.data(forKey: key),
           let saved = try? JSONDecoder().decode([SavedServer].self, from: data) {
            servers = saved
        }
    }

    func add(_ server: SavedServer) {
        servers.removeAll { $0.id == server.id }
        servers.insert(server, at: 0)
        save()
    }

    func remove(_ server: SavedServer) {
        servers.removeAll { $0.id == server.id }
        save()
    }

    private func save() {
        if let data = try? JSONEncoder().encode(servers) { defaults.set(data, forKey: key) }
    }
}
