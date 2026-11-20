import Foundation
import Observation

/// The signed-in user for the current server. The token lives in the TokenStore (Keychain).
@MainActor
@Observable
final class SessionStore {
    enum State: Equatable {
        case unknown
        case signedOut
        case signedIn
        /// A saved sign-in exists but the server cannot be reached: the downloads still play.
        case offline
    }

    private(set) var state: State = .unknown
    private let tokens: TokenStore
    /// The server the state belongs to; switching servers starts from `.unknown`.
    private var serverID: String?
    /// Checks a saved token against the server (the account call); replaceable in tests.
    private let check: @Sendable (SavedServer, String) async throws -> Void

    /// Keychain items outlive the app, so a reinstall would sign in without asking.
    /// The first launch of an install therefore drops every saved session.
    init(tokens: TokenStore = KeychainTokenStore(), defaults: UserDefaults = .standard,
         check: @escaping @Sendable (SavedServer, String) async throws -> Void = { server, token in
             // The hand-written model: a new value in a server enum must not look like an outage.
             _ = try await MediaClient(baseURL: server.url, token: token).me()
         }) {
        self.tokens = tokens
        self.check = check
        let installed = "streamero.installed"
        if !defaults.bool(forKey: installed) {
            tokens.removeAll()
            defaults.set(true, forKey: installed)
        }
    }

    /// Adding a server always starts with the sign-in screen, even if it was used before.
    func forget(serverID: String) {
        tokens.setToken(nil, for: serverID)
        if self.serverID == nil || self.serverID == serverID { state = .signedOut }
    }

    func token(for server: SavedServer) -> String? { tokens.token(for: server.id) }

    /// Restores a saved session; a rejected token is forgotten.
    func restore(server: SavedServer) async {
        if serverID != server.id {
            serverID = server.id
            state = .unknown
        }
        guard let token = tokens.token(for: server.id) else {
            state = .signedOut
            return
        }
        let next: State
        do {
            try await check(server, token)
            next = .signedIn
        } catch is CancellationError {
            return
        } catch {
            next = Self.state(after: error)
        }
        // A late answer must not overwrite a newer state: the user switched servers, signed in
        // again (a new token) or out while the check ran.
        guard serverID == server.id, tokens.token(for: server.id) == token else { return }
        if next == .signedOut { tokens.setToken(nil, for: server.id) }
        state = next
    }

    /// What a failed session check means. Only an unreachable server (or one answering with a
    /// server error while it starts) is offline; a rejected token signs out; an answer the app
    /// cannot read still proves the session works, so the signed-in screens take over.
    nonisolated static func state(after error: Error) -> State {
        if let api = error as? APIError {
            if api.status == 401 { return .signedOut }
            if api.status == 0 || api.status >= 500 { return .offline }
            return .signedIn
        }
        if error is URLError { return .offline }
        if error is DecodingError { return .signedIn }
        return .offline
    }

    func signedIn(server: SavedServer, token: String) {
        serverID = server.id
        tokens.setToken(token, for: server.id)
        state = .signedIn
    }

    /// The server rejected `token` mid-session: forget it and ask to sign in again. A token
    /// that is no longer the saved one (an old client after switching servers) is ignored.
    func expire(server: SavedServer, token: String) {
        guard tokens.token(for: server.id) == token else { return }
        tokens.setToken(nil, for: server.id)
        if serverID == server.id { state = .signedOut }
    }

    /// Forgets the session at once and tells the server in the background, so signing out of
    /// an unreachable server does not wait for a network timeout.
    func signOut(server: SavedServer) async {
        let token = tokens.token(for: server.id)
        tokens.setToken(nil, for: server.id)
        state = .signedOut
        if let token {
            let url = server.url
            Task.detached(priority: .utility) { await AuthClient(baseURL: url, token: token).logout() }
        }
    }
}
