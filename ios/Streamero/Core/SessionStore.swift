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
    }

    private(set) var state: State = .unknown
    private let tokens: TokenStore

    init(tokens: TokenStore = KeychainTokenStore()) {
        self.tokens = tokens
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
            // Offline or server starting: keep the token, show sign-in with the error later.
            state = .signedOut
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
