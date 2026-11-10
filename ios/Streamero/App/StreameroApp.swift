import SwiftUI

@main
struct StreameroApp: App {
    @State private var serverStore = ServerStore()
    @State private var sessionStore = SessionStore()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(serverStore)
                .environment(sessionStore)
                .preferredColorScheme(.dark)
                .tint(Theme.accent)
        }
    }
}

struct RootView: View {
    @Environment(ServerStore.self) private var store
    @Environment(SessionStore.self) private var session

    var body: some View {
        if let server = store.current {
            Group {
                switch session.state {
                case .unknown:
                    ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity).background(Theme.background)
                case .signedOut:
                    LoginView(server: server)
                case .signedIn:
                    if let token = session.token(for: server) {
                        MainView(server: server, client: MediaClient(baseURL: server.url, token: token))
                    } else {
                        // The saved sign-in disappeared (for example the Keychain was reset): sign in again.
                        LoginView(server: server)
                    }
                }
            }
            .task(id: server.id) { await session.restore(server: server) }
        } else {
            AddServerView()
        }
    }
}
