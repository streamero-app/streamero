import SwiftUI
import UIKit

/// UIKit hooks SwiftUI does not offer: reconnecting to the background download session at
/// launch and the system's "your transfers finished" relaunch.
@MainActor
final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        DownloadManager.shared.start()
        return true
    }

    func application(_ application: UIApplication, handleEventsForBackgroundURLSession identifier: String,
                     completionHandler: @escaping () -> Void) {
        DownloadManager.shared.handleBackgroundEvents(identifier: identifier, completion: completionHandler)
    }
}

@main
struct StreameroApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
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
    @Environment(\.scenePhase) private var phase

    var body: some View {
        Group {
            if let server = store.current {
                Group {
                    switch session.state {
                    case .unknown:
                        ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity).background(Theme.background)
                    case .signedOut:
                        LoginView(server: server)
                    case .offline:
                        OfflineRootView(server: server) { await session.restore(server: server) }
                    case .signedIn:
                        if let token = session.token(for: server) {
                            MainView(server: server, client: MediaClient(baseURL: server.url, token: token))
                                // A new server or sign-in gets fresh state (music, player, profile).
                                .id("\(server.id)|\(token)")
                        } else {
                            // The saved sign-in disappeared (for example the Keychain was reset): sign in again.
                            LoginView(server: server)
                        }
                    }
                }
                .task(id: server.id) { await session.restore(server: server) }
                .task(id: "\(server.id)|\(stateKey)") { activateDownloads(server) }
            } else {
                AddServerView()
            }
        }
        .onChange(of: phase) { _, now in
            if now == .active { DownloadManager.shared.appBecameActive() }
        }
    }

    private var stateKey: String {
        switch session.state {
        case .unknown: "unknown"
        case .signedOut: "signedOut"
        case .offline: "offline"
        case .signedIn: "signedIn"
        }
    }

    /// Opens the server's offline library; with a saved sign-in it also syncs.
    private func activateDownloads(_ server: SavedServer) {
        let token = session.token(for: server)
        DownloadManager.shared.activate(server: server, client: token.map { MediaClient(baseURL: server.url, token: $0) })
    }
}
