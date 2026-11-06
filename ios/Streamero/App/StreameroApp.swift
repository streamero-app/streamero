import SwiftUI

@main
struct StreameroApp: App {
    @State private var serverStore = ServerStore()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(serverStore)
                .preferredColorScheme(.dark)
                .tint(Theme.accent)
        }
    }
}

struct RootView: View {
    @Environment(ServerStore.self) private var store

    var body: some View {
        if let server = store.current {
            ConnectedPlaceholderView(server: server)
        } else {
            AddServerView()
        }
    }
}

/// Shown after a server is added; replaced by login in P1.
struct ConnectedPlaceholderView: View {
    let server: SavedServer
    @Environment(ServerStore.self) private var store

    var body: some View {
        VStack(spacing: 16) {
            Text(server.name).font(.title.bold())
            Text("server.connected \(server.version)")
                .foregroundStyle(Theme.textMuted)
            Button("server.remove", role: .destructive) { store.remove(server) }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.background)
    }
}
