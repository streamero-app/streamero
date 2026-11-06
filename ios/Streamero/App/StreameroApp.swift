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
                case .signedIn(let user):
                    SignedInView(server: server, user: user)
                }
            }
            .task(id: server.id) { await session.restore(server: server) }
        } else {
            AddServerView()
        }
    }
}

/// Home after sign-in; libraries and playback arrive in P7.
struct SignedInView: View {
    let server: SavedServer
    let user: Components.Schemas.User
    @Environment(SessionStore.self) private var session

    var body: some View {
        VStack(spacing: 16) {
            Text("home.welcome \(user.username)").font(.title.bold())
            Text("server.connected \(server.version)").foregroundStyle(Theme.textMuted)
            Text(server.name).foregroundStyle(Theme.textMuted)
            Button("nav.logout", role: .destructive) {
                Task { await session.signOut(server: server) }
            }
            .buttonStyle(.bordered)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.background)
    }
}
