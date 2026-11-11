import Foundation
import Observation

/// The signed-in user for the current server. The token lives in the TokenStore (Keychain).
@MainActor
@Observable
final class SessionStore {
    enum State: Equatable {
        case unknown
        case signedOut
        case signedIn(Components.Schemas.User)
        /// A saved sign-in exists but the server cannot be reached: the downloads still play.
        case offline
    }

    private(set) var state: State = .unknown
    private let tokens: TokenStore

    /// Keychain items outlive the app, so a reinstall would sign in without asking.
    /// The first launch of an install therefore drops every saved session.
    init(tokens: TokenStore = KeychainTokenStore(), defaults: UserDefaults = .standard) {
        self.tokens = tokens
        let installed = "streamero.installed"
        if !defaults.bool(forKey: installed) {
            tokens.removeAll()
            defaults.set(true, forKey: installed)
        }
    }

    /// Adding a server always starts with the sign-in screen, even if it was used before.
    func forget(serverID: String) {
        tokens.setToken(nil, for: serverID)
        state = .signedOut
    }

    func token(for server: SavedServer) -> String? { tokens.token(for: server.id) }

    /// Restores a saved session; a rejected token is forgotten.
    func restore(server: SavedServer) async {
        guard let token = tokens.token(for: server.id) else {
            state = .signedOut
            return
        }
        do {
            state = .signedIn(try await AuthClient(baseURL: server.url, token: token).me())
        } catch let error as APIError where error.status == 401 {
            tokens.setToken(nil, for: server.id)
            state = .signedOut
        } catch {
            // Offline or server starting: keep the token and open the offline library.
            state = .offline
        }
    }

    func signedIn(server: SavedServer, token: String, user: Components.Schemas.User) {
        tokens.setToken(token, for: server.id)
        state = .signedIn(user)
    }

    func signOut(server: SavedServer) async {
        if let token = tokens.token(for: server.id) {
            await AuthClient(baseURL: server.url, token: token).logout()
        }
        tokens.setToken(nil, for: server.id)
        state = .signedOut
    }
}
