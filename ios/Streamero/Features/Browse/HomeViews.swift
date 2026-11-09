import SwiftUI

extension View {
    /// Registers all navigation destinations of the signed-in tabs.
    func withAppRoutes(client: MediaClient) -> some View {
        navigationDestination(for: Route.self) { route in
            RouteDestination(route: route, client: client)
        }
    }
}

struct RouteDestination: View {
    let route: Route
    let client: MediaClient

    var body: some View {
        switch route {
        case .item(let id): ItemView(itemID: id, client: client)
        case .library(let library):
            if library.kind == "music" {
                MusicLibraryView(library: library, client: client)
            } else {
                LibraryView(route: library, client: client)
            }
        case .favorites: FavoritesView(client: client)
        case .collections: CollectionsView(client: client)
        case .collection(let id): CollectionView(id: id, client: client)
        case .playlist(let id): PlaylistView(id: id, client: client)
        case .settings(let page): SettingsPageView(page: page, client: client)
        }
    }
}

/// Signed-in root: profile picker, then the five tabs with the music bar above them.
struct MainView: View {
    let server: SavedServer
    let client: MediaClient
    @State private var app: AppState
    @State private var generation = 0
    @State private var loadFailed = false
    @Environment(SessionStore.self) private var session
    @Environment(\.locale) private var systemLocale

    init(server: SavedServer, client: MediaClient) {
        self.server = server
        self.client = client
        _app = State(initialValue: AppState(server: server, client: client))
    }

    var body: some View {
        Group {
            if app.me != nil {
                tabs
            } else if loadFailed {
                VStack(spacing: 16) {
                    Text("err_network").foregroundStyle(Theme.textMuted)
                    Button { Task { await bootstrap() } } label: { Text("connect") }
                        .buttonStyle(PrimaryButtonStyle())
                    Button("sign_out") { Task { await signOut() } }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Theme.background)
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity).background(Theme.background)
            }
        }
        .environment(app)
        .environment(\.locale, app.locale ?? systemLocale)
        .task { await bootstrap() }
    }

    private func bootstrap() async {
        loadFailed = false
        do {
            let me = try await client.me()
            app.me = me
            Loc.apply(language: me.language)
            app.enhancements = (try? await client.enhancements()) ?? Enhancements()
            app.profiles = (try? await client.profiles()) ?? []
            // Like a streaming service: ask who is watching when the app starts.
            if !app.profiles.isEmpty && me.currentProfile == nil { app.showProfileGate = true }
        } catch let error as APIError where error.status == 401 {
            await signOut()
        } catch {
            loadFailed = true
        }
    }

    private func signOut() async {
        app.music.stop()
        await session.signOut(server: server)
    }

    @ViewBuilder private var tabs: some View {
        @Bindable var app = app
        TabView {
            tab { HomeTab(client: client) }
                .tabItem { Label("nav_home", systemImage: "house.fill") }
            tab { SearchTab(client: client) }
                .tabItem { Label("nav_search", systemImage: "magnifyingglass") }
            tab { DiscoverTab(client: client) }
                .tabItem { Label("nav_discover", systemImage: "safari") }
            tab { LibrariesTab(client: client) }
                .tabItem { Label("nav_libraries", systemImage: "rectangle.stack") }
            tab { MeTab(client: client, signOut: { Task { await signOut() } }) }
                .tabItem { Label("nav_me", systemImage: "person.crop.circle") }
        }
        .id(generation)
        .tint(.white)
        .fullScreenCover(isPresented: $app.showProfileGate) {
            ProfileGateView(client: client) {
                app.showProfileGate = false
                generation += 1
                app.reloadTick += 1
            }
            .environment(app)
            .environment(\.locale, app.locale ?? systemLocale)
        }
        .fullScreenCover(item: $app.playing, onDismiss: { app.reloadTick += 1 }) { target in
            PlayerView(target: target, client: client, music: app.music)
                .environment(app)
                .environment(\.locale, app.locale ?? systemLocale)
        }
    }

    private func tab<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        NavigationStack {
            content().withAppRoutes(client: client)
        }
        .safeAreaInset(edge: .bottom) { MiniPlayerBar(client: client) }
    }
}

/// "Who's watching?"
struct ProfileGateView: View {
    let client: MediaClient
    let done: () -> Void
    @Environment(AppState.self) private var app
    @State private var profiles: [Profile] = []
    @State private var pinFor: Profile?
    @State private var pin = ""
    @State private var askPassword = false
    @State private var password = ""
    @State private var failed = false

    private let columns = [GridItem(.adaptive(minimum: 120), spacing: 24)]

    var body: some View {
        let me = app.me
        VStack(spacing: 36) {
            Spacer()
            Text("profiles.whoIsWatching").font(Theme.display(26)).textCase(.uppercase).foregroundStyle(Theme.text)
            LazyVGrid(columns: columns, spacing: 28) {
                tile(name: me?.displayName ?? me?.username ?? "", color: "#8A5A3A",
                     url: me?.hasAvatar == true ? me.flatMap { client.avatarURL(owner: $0.id) } : nil, locked: false) { chooseMain() }
                ForEach(profiles) { p in
                    tile(name: p.name, color: p.color, url: p.hasAvatar == true ? client.avatarURL(owner: p.id) : nil, locked: p.hasPin) {
                        if p.hasPin { pinFor = p } else { Task { await apply { try await client.selectProfile(p.id, pin: nil) } } }
                    }
                }
            }
            .padding(.horizontal, 32)
            Spacer()
            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.background.ignoresSafeArea())
        .task { profiles = (try? await client.profiles()) ?? [] }
        .alert(Text("profiles.pin"), isPresented: Binding(get: { pinFor != nil }, set: { if !$0 { pinFor = nil } })) {
            SecureField("profiles.pin", text: $pin).keyboardType(.numberPad)
            Button("profiles.open") {
                guard let p = pinFor else { return }
                let code = pin
                pin = ""
                Task { await apply { try await client.selectProfile(p.id, pin: code) } }
            }
            Button("player.back", role: .cancel) { pin = "" }
        }
        .alert(Text("password"), isPresented: $askPassword) {
            SecureField("password", text: $password)
            Button("profiles.open") {
                let value = password
                password = ""
                Task { await apply { try await client.selectMainProfile(password: value) } }
            }
            Button("player.back", role: .cancel) { password = "" }
        }
        .alert(Text("profiles.wrongPin"), isPresented: $failed) { Button("OK") {} }
    }

    /// Leaving a kids profile needs the account password; any other profile just switches.
    private func chooseMain() {
        guard let current = app.me?.currentProfile else { done(); return }
        if current.isKid {
            askPassword = true
        } else {
            Task { await apply { try await client.selectMainProfile() } }
        }
    }

    private func apply(_ action: @MainActor () async throws -> Void) async {
        do {
            try await action()
            await app.refreshMe()
            done()
        } catch {
            failed = true
        }
    }

    private func tile(name: String, color: String, url: URL?, locked: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            VStack(spacing: 10) {
                AvatarView(name: name, color: color, url: url, token: client.token, size: 104)
                    .overlay(alignment: .bottomTrailing) {
                        if locked { Image(systemName: "lock.fill").font(.caption).foregroundStyle(.white.opacity(0.85)).padding(8) }
                    }
                Text(verbatim: name).font(.subheadline).foregroundStyle(Theme.textMuted).lineLimit(1)
            }
        }
        .buttonStyle(CardPressStyle())
    }
}

/// Home: spotlight stage, then the shelves in the order the profile chose.
struct HomeTab: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var data: HomeData?
    @State private var error: Error?
    @State private var pick: ItemSummary?
    @State private var randomItem: String?

    private struct HomeData {
        var latest: [ItemSummary]
        var cont: [ItemSummary]
        var nextUp: [ItemSummary]
        var favorites: [ItemSummary]
        var collections: [MediaCollection]
        var libraries: [Library]
        var sections: [String]?
    }

    private var pool: [ItemSummary] {
        Array((data?.latest ?? []).filter { $0.kind == "movie" || $0.kind == "series" }.prefix(10))
    }

    private var featured: ItemSummary? {
        pick ?? pool.first { $0.hasBackdrop == true } ?? pool.first
    }

    private var isEmpty: Bool {
        guard let d = data else { return false }
        return d.latest.isEmpty && d.cont.isEmpty && d.nextUp.isEmpty && d.favorites.isEmpty && d.collections.isEmpty
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 30) {
                if let featured {
                    HeroView(item: featured, client: client, onPlay: { app.play(PlayTarget(id: featured.id)) },
                             onRandom: app.enhancements.randomButton ? { Task { await surprise() } } : nil)
                    stagePicker
                } else {
                    Color.clear.frame(height: 100)
                }
                if data == nil && error == nil { LoadingView() }
                ErrorLine(error: error).padding(.horizontal, 20)
                if isEmpty { emptyState }
                if let data {
                    ForEach(data.sections ?? Self.defaultOrder, id: \.self) { section in
                        shelf(section, data)
                    }
                }
            }
            .padding(.bottom, 24)
        }
        .ignoresSafeArea(edges: .top)
        .background(AmbientBackground(colors: featured?.colors))
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                HStack(spacing: 8) {
                    Image("AppMark").resizable().frame(width: 26, height: 26).clipShape(RoundedRectangle(cornerRadius: 7, style: .continuous))
                    Text(verbatim: app.server.name).font(.headline)
                }
            }
        }
        .toolbarBackground(.hidden, for: .navigationBar)
        .navigationDestination(item: $randomItem) { id in ItemView(itemID: id, client: client) }
        .task(id: app.reloadTick) { await load() }
        .refreshable { await load() }
    }

    private static let defaultOrder = ["continue", "nextUp", "latest", "favorites", "collections", "libraries"]

    private var emptyState: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("home_empty_title").font(Theme.display(34)).textCase(.uppercase).foregroundStyle(Theme.text)
            Text(app.isAdmin ? "home_empty_admin" : "home_empty_user").foregroundStyle(Theme.textMuted)
        }
        .padding(.horizontal, 20)
    }

    private var stagePicker: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 10) {
                ForEach(pool) { c in
                    Button { pick = c } label: {
                        AuthImage(url: client.imageURL(item: c.id, type: c.hasPoster ? "poster" : "backdrop"), token: client.token, title: c.title)
                            .frame(width: 78, height: 117)
                            .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
                            .overlay(RoundedRectangle(cornerRadius: 6, style: .continuous).strokeBorder(Theme.text, lineWidth: c.id == featured?.id ? 2 : 0))
                    }
                    .buttonStyle(CardPressStyle())
                }
            }
            .padding(.horizontal, 20)
        }
    }

    @ViewBuilder private func shelf(_ section: String, _ d: HomeData) -> some View {
        switch section {
        case "continue":
            if !d.cont.isEmpty { playShelf("continue_watching", d.cont) }
        case "nextUp":
            if !d.nextUp.isEmpty { playShelf("next_up", d.nextUp) }
        case "latest":
            if !d.latest.isEmpty { posterShelf("recently_added", d.latest) }
        case "favorites":
            if !d.favorites.isEmpty { posterShelf("my_list", d.favorites) }
        case "collections":
            if !d.collections.isEmpty {
                Shelf(title: "collections") {
                    ForEach(d.collections) { c in
                        NavigationLink(value: Route.collection(c.id)) { CollectionTile(collection: c, client: client).frame(width: 130) }
                            .buttonStyle(CardPressStyle())
                    }
                }
            }
        case "libraries":
            if !d.libraries.isEmpty {
                Shelf(title: "libraries") {
                    ForEach(d.libraries) { lib in
                        NavigationLink(value: Route.library(LibraryRoute(id: lib.id, name: lib.name, kind: lib.kind))) {
                            LibraryTile(library: lib).frame(width: 240)
                        }
                        .buttonStyle(CardPressStyle())
                    }
                }
            }
        default:
            EmptyView()
        }
    }

    private func playShelf(_ title: LocalizedStringKey, _ items: [ItemSummary]) -> some View {
        Shelf(title: title) {
            ForEach(items) { item in
                Button { app.play(PlayTarget(id: item.id)) } label: { WideCard(item: item, client: client) }
                    .buttonStyle(CardPressStyle())
            }
        }
    }

    private func posterShelf(_ title: LocalizedStringKey, _ items: [ItemSummary]) -> some View {
        Shelf(title: title) {
            ForEach(items) { item in
                NavigationLink(value: Route.item(item.id)) { PosterCard(item: item, client: client).frame(width: 130) }
                    .buttonStyle(CardPressStyle())
            }
        }
    }

    private func load() async {
        do {
            async let prefs = try? client.preferences()
            let latest = try await client.latest()
            let d = HomeData(latest: latest,
                             cont: (try? await client.continueWatching()) ?? [],
                             nextUp: (try? await client.nextUp()) ?? [],
                             favorites: (try? await client.favorites()) ?? [],
                             collections: (try? await client.collections()) ?? [],
                             libraries: (try? await client.libraries()) ?? [],
                             sections: await prefs?.homeSections)
            data = d
            error = nil
        } catch {
            self.error = error
        }
    }

    private func surprise() async {
        let libs = ((try? await client.libraries()) ?? []).filter { $0.kind != "music" }.shuffled()
        for lib in libs {
            if let item = try? await client.items(library: lib.id, query: ["sort": "random", "unplayed": "true", "limit": "1"]).items.first {
                randomItem = item.id
                return
            }
        }
    }
}
