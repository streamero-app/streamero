import AVKit
import SwiftUI

/// What the player cover is showing.
struct PlayTarget: Identifiable, Hashable { let id: String }

struct LibraryRoute: Hashable {
    let id: String
    let name: String
}

/// Signed-in root: profile picker, then Home / Search / My list tabs.
struct HomeView: View {
    let server: SavedServer
    let user: Components.Schemas.User
    let client: MediaClient
    @State private var profiles: [Components.Schemas.Profile] = []
    @State private var showGate = false
    @State private var generation = 0

    var body: some View {
        TabView {
            HomeTab(server: server, user: user, client: client)
                .tabItem { Label("nav.home", systemImage: "house.fill") }
            SearchTab(client: client)
                .tabItem { Label("nav.search", systemImage: "magnifyingglass") }
            MyListTab(client: client)
                .tabItem { Label("nav.myList", systemImage: "bookmark.fill") }
        }
        .id(generation)
        .tint(.white)
        .task {
            profiles = (try? await client.profiles()) ?? []
            // Like a streaming service: ask who is watching when the app starts.
            if !profiles.isEmpty && user.currentProfile == nil { showGate = true }
        }
        .fullScreenCover(isPresented: $showGate) {
            ProfileGateView(user: user, profiles: profiles, client: client) {
                showGate = false
                generation += 1
            }
        }
    }
}

/// "Who's watching?"
struct ProfileGateView: View {
    let user: Components.Schemas.User
    let profiles: [Components.Schemas.Profile]
    let client: MediaClient
    let done: () -> Void
    @State private var pinFor: Components.Schemas.Profile?
    @State private var pin = ""
    @State private var failed = false

    private let columns = [GridItem(.adaptive(minimum: 120), spacing: 24)]

    var body: some View {
        VStack(spacing: 36) {
            Spacer()
            Text("profiles.whoIsWatching").font(Theme.display(30)).foregroundStyle(Theme.text)
            LazyVGrid(columns: columns, spacing: 28) {
                avatar(name: user.username, color: "#8a5a2b", locked: false) { Task { try? await client.selectMainProfile(); done() } }
                ForEach(profiles, id: \.id) { p in
                    avatar(name: p.name, color: p.color, locked: p.hasPin) {
                        if p.hasPin { pinFor = p } else { Task { try? await client.selectProfile(p.id, pin: nil); done() } }
                    }
                }
            }
            .padding(.horizontal, 32)
            Spacer()
            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.background.ignoresSafeArea())
        .alert(Text("profiles.pin"), isPresented: Binding(get: { pinFor != nil }, set: { if !$0 { pinFor = nil } })) {
            SecureField("profiles.pin", text: $pin).keyboardType(.numberPad)
            Button("profiles.open") {
                guard let p = pinFor else { return }
                Task {
                    do { try await client.selectProfile(p.id, pin: pin); pin = ""; done() } catch { failed = true; pin = "" }
                }
            }
            Button("player.back", role: .cancel) { pin = "" }
        }
        .alert(Text("profiles.wrongPin"), isPresented: $failed) { Button("OK") {} }
    }

    private func avatar(name: String, color: String, locked: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            VStack(spacing: 10) {
                Text(String(name.prefix(1)).uppercased())
                    .font(.system(size: 44, weight: .bold)).foregroundStyle(.white)
                    .frame(width: 104, height: 104)
                    .background(LinearGradient(colors: [Color(hex: color), Color(hex: color).opacity(0.55)], startPoint: .topLeading, endPoint: .bottomTrailing),
                                in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                    .overlay(alignment: .bottomTrailing) {
                        if locked { Image(systemName: "lock.fill").font(.caption).foregroundStyle(.white.opacity(0.85)).padding(8) }
                    }
                Text(name).font(.subheadline).foregroundStyle(Theme.textMuted).lineLimit(1)
            }
        }
        .buttonStyle(CardPressStyle())
    }
}


/// My list (favorites) as a grid of landscape cards.
struct MyListTab: View {
    let client: MediaClient
    @State private var items: [ItemSummary] = []
    @State private var loaded = false

    private let columns = [GridItem(.adaptive(minimum: 160), spacing: 10)]

    var body: some View {
        NavigationStack {
            ScrollView {
                if loaded && items.isEmpty {
                    Text("library.myListEmpty").foregroundStyle(Theme.textMuted).padding(.top, 80).padding(.horizontal, 32)
                        .multilineTextAlignment(.center)
                }
                LazyVGrid(columns: columns, spacing: 14) {
                    ForEach(items, id: \.id) { item in
                        NavigationLink(value: item.id) { WideCard(item: item, client: client, width: nil) }.buttonStyle(CardPressStyle())
                    }
                }
                .padding(.horizontal, 16)
            }
            .background(Theme.background)
            .navigationTitle(Text("nav.myList"))
            .navigationDestination(for: String.self) { ItemView(itemID: $0, client: client) }
            .task { await load() }
            .refreshable { await load() }
        }
    }

    private func load() async {
        items = (try? await client.favorites()) ?? []
        loaded = true
    }
}

/// Home: spotlight hero, shelves and libraries.
struct HomeTab: View {
    let server: SavedServer
    let user: Components.Schemas.User
    let client: MediaClient
    @Environment(SessionStore.self) private var session
    @State private var libraries: [Library] = []
    @State private var continueItems: [ItemSummary] = []
    @State private var latest: [ItemSummary] = []
    @State private var nextUp: [ItemSummary] = []
    @State private var favorites: [ItemSummary] = []
    @State private var error: LocalizedStringKey?
    @State private var showQuickConnect = false
    @State private var playing: PlayTarget?

    private var featured: ItemSummary? {
        latest.first { $0.hasBackdrop == true } ?? latest.first { $0.hasPoster } ?? latest.first
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 30) {
                    if let featured {
                        HeroView(item: featured, client: client) { playing = PlayTarget(id: featured.id) }
                    } else {
                        Color.clear.frame(height: 100)
                    }
                    if !continueItems.isEmpty {
                        Shelf(title: "home.continue") {
                            ForEach(continueItems, id: \.id) { item in
                                Button { playing = PlayTarget(id: item.id) } label: { WideCard(item: item, client: client) }
                                    .buttonStyle(CardPressStyle())
                            }
                        }
                    }
                    if !nextUp.isEmpty {
                        Shelf(title: "home.nextUp") {
                            ForEach(nextUp, id: \.id) { item in
                                Button { playing = PlayTarget(id: item.id) } label: { WideCard(item: item, client: client) }
                                    .buttonStyle(CardPressStyle())
                            }
                        }
                    }
                    if !latest.isEmpty {
                        Shelf(title: "home.latest") {
                            ForEach(latest, id: \.id) { item in
                                NavigationLink(value: item.id) { WideCard(item: item, client: client, showPlay: false) }
                                    .buttonStyle(CardPressStyle())
                            }
                        }
                    }
                    if !favorites.isEmpty {
                        Shelf(title: "nav.myList") {
                            ForEach(favorites, id: \.id) { item in
                                NavigationLink(value: item.id) { WideCard(item: item, client: client, showPlay: false) }
                                    .buttonStyle(CardPressStyle())
                            }
                        }
                    }
                    VStack(alignment: .leading, spacing: 12) {
                        Text("home.libraries").font(.title3.bold())
                        if libraries.isEmpty {
                            Text("home.noLibraries").foregroundStyle(Theme.textMuted)
                        }
                        ForEach(libraries, id: \.id) { lib in
                            NavigationLink(value: LibraryRoute(id: lib.id, name: lib.name)) { LibraryTile(library: lib) }
                                .buttonStyle(CardPressStyle())
                        }
                    }
                    .padding(.horizontal, 20)
                    if let error { Text(error).foregroundStyle(.red).padding(.horizontal, 20) }
                }
                .padding(.bottom, 24)
            }
            .ignoresSafeArea(edges: .top)
            .background(AmbientBackground(colors: featured?.colors))
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    HStack(spacing: 8) {
                        Image("AppMark").resizable().frame(width: 26, height: 26).clipShape(RoundedRectangle(cornerRadius: 7, style: .continuous))
                        Text(server.name).font(.headline)
                    }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Text(user.username)
                        Button("quickConnect.approveTitle", systemImage: "qrcode") { showQuickConnect = true }
                        Button("nav.logout", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) {
                            Task { await session.signOut(server: server) }
                        }
                    } label: {
                        Text(String(user.username.prefix(1)).uppercased())
                            .font(.subheadline.bold()).foregroundStyle(Theme.accentContrast)
                            .frame(width: 32, height: 32).background(Theme.accent, in: Circle())
                    }
                    .accessibilityLabel(Text(user.username))
                }
            }
            .toolbarBackground(.hidden, for: .navigationBar)
            .navigationDestination(for: LibraryRoute.self) { LibraryView(route: $0, client: client) }
            .navigationDestination(for: String.self) { ItemView(itemID: $0, client: client) }
            .task { await load() }
            .refreshable { await load() }
            .sheet(isPresented: $showQuickConnect) { QuickConnectApproveView(client: client) }
            .fullScreenCover(item: $playing, onDismiss: { Task { await load() } }) { PlayerView(itemID: $0.id, client: client) }
        }
    }

    private func load() async {
        do {
            async let libs = client.libraries()
            async let cont = client.continueWatching()
            async let new = client.latest()
            async let favs = client.favorites()
            async let up = client.nextUp()
            libraries = try await libs
            continueItems = (try? await cont) ?? []
            latest = (try? await new) ?? []
            favorites = (try? await favs) ?? []
            nextUp = (try? await up) ?? []
            error = nil
        } catch {
            self.error = "addServer.error.unreachable"
        }
    }
}

/// Full-bleed spotlight: backdrop, scrims, serif title and actions.
struct HeroView: View {
    let item: ItemSummary
    let client: MediaClient
    let onPlay: () -> Void

    private var playable: Bool { item.kind == .movie || item.kind == .episode }

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            Group {
                if item.hasBackdrop == true {
                    AuthImage(url: client.imageURL(item: item.id, type: "backdrop"), token: client.token)
                } else if item.hasPoster {
                    AuthImage(url: client.imageURL(item: item.id, type: "poster"), token: client.token).blur(radius: 40).saturation(1.3)
                } else {
                    PosterPlaceholder(title: "")
                }
            }
            .frame(height: 560).frame(maxWidth: .infinity).clipped()
            // The picture dissolves into the lit room instead of ending in a hard edge.
            .mask(LinearGradient(colors: [.black, .black, .black.opacity(0.6), .clear], startPoint: .top, endPoint: .bottom))
            LinearGradient(colors: [.black.opacity(0.5), .clear, .black.opacity(0.25), .black.opacity(0.45)],
                           startPoint: .top, endPoint: .bottom)
            VStack(alignment: .leading, spacing: 12) {
                TitleArt(id: item.id, title: item.title, hasLogo: item.hasLogo == true, client: client, size: 44)
                MetaLine(item: item)
                HStack(spacing: 12) {
                    if playable {
                        Button(action: onPlay) { Label("player.play", systemImage: "play.fill") }.buttonStyle(PrimaryButtonStyle(hex: item.colors?.first))
                    }
                    NavigationLink(value: item.id) { Label("home.moreInfo", systemImage: "info.circle") }
                        .buttonStyle(MutedButtonStyle())
                }
                .padding(.top, 4)
            }
            .padding(.horizontal, 20).padding(.bottom, 8)
        }
        .frame(height: 560)
    }
}

/// The title as its logo artwork when there is one, else heavy type.
struct TitleArt: View {
    let id: String
    let title: String
    let hasLogo: Bool
    let client: MediaClient
    var size: CGFloat = 40
    var maxHeight: CGFloat = 110

    var body: some View {
        if hasLogo {
            AuthImage(url: client.imageURL(item: id, type: "logo"), token: client.token, fit: true)
                .frame(maxWidth: 300, maxHeight: maxHeight, alignment: .bottomLeading)
                .shadow(color: .black.opacity(0.5), radius: 10)
                .accessibilityLabel(Text(title))
        } else {
            Text(title).font(Theme.display(size)).foregroundStyle(Theme.text).lineLimit(3).minimumScaleFactor(0.6)
        }
    }
}

struct MetaLine: View {
    let item: ItemSummary

    var body: some View {
        HStack(spacing: 10) {
            if let r = item.rating {
                Label { Text(r, format: .number.precision(.fractionLength(1))) } icon: { Image(systemName: "star.fill").foregroundStyle(Theme.accent) }
                    .labelStyle(.titleAndIcon).fontWeight(.semibold)
            }
            if let y = item.year { Text(String(y)) }
            if let d = item.durationMs, d > 0 { Text(Duration.milliseconds(d), format: .units(allowed: [.hours, .minutes], width: .narrow)) }
        }
        .font(.subheadline.monospacedDigit())
        .foregroundStyle(Theme.text.opacity(0.75))
    }
}

/// Titled horizontal row.
struct Shelf<Content: View>: View {
    let title: LocalizedStringKey
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title).font(.title3.bold()).padding(.horizontal, 20)
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(alignment: .top, spacing: 14) { content() }
                    .padding(.horizontal, 20)
                    .scrollTargetLayout()
            }
            .scrollTargetBehavior(.viewAligned)
        }
    }
}

private func progress(_ item: ItemSummary) -> Double? {
    guard let pos = item.positionMs, let dur = item.durationMs, dur > 0, pos > 0 else { return nil }
    return min(1, Double(pos) / Double(dur))
}

struct ProgressStrip: View {
    let value: Double
    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(.black.opacity(0.55))
                Capsule().fill(Theme.accent).frame(width: g.size.width * value)
            }
        }
        .frame(height: 4)
    }
}

struct PosterCard: View {
    let item: ItemSummary
    let client: MediaClient

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ZStack(alignment: .topTrailing) {
                AuthImage(url: item.hasPoster ? client.imageURL(item: item.id, type: item.kind == .episode ? "still" : "poster") : nil,
                          token: client.token, title: item.title, year: item.year)
                    .aspectRatio(2 / 3, contentMode: .fill)
                if item.played == true {
                    Image(systemName: "checkmark").font(.caption.bold()).foregroundStyle(Theme.accentContrast)
                        .frame(width: 22, height: 22).background(Theme.accent, in: Circle()).padding(7)
                } else if item.favorite == true {
                    Image(systemName: "bookmark.fill").font(.caption2).foregroundStyle(Theme.accent)
                        .frame(width: 22, height: 22).background(.black.opacity(0.55), in: Circle()).padding(7)
                }
            }
            .overlay(alignment: .bottom) {
                if let p = progress(item) { ProgressStrip(value: p).padding(8) }
            }
            .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous).strokeBorder(Theme.border))
            VStack(alignment: .leading, spacing: 2) {
                Text(item.title).font(.footnote.weight(.medium)).foregroundStyle(Theme.text).lineLimit(1)
                Text(item.year.map(String.init) ?? " ").font(.caption2.monospacedDigit()).foregroundStyle(Theme.textMuted)
            }
        }
    }
}

/// Landscape card for continue watching.
struct WideCard: View {
    let item: ItemSummary
    let client: MediaClient
    var showPlay = true
    var width: CGFloat? = 240

    private var art: URL? {
        if item.hasBackdrop == true { return client.imageURL(item: item.id, type: "backdrop") }
        if item.kind == .episode && item.hasPoster { return client.imageURL(item: item.id, type: "still") }
        return nil
    }

    var body: some View {
        Color.clear
            .frame(width: width)
            .aspectRatio(16 / 9, contentMode: .fit)
            .overlay { AuthImage(url: art, token: client.token, title: item.title) }
            .overlay(alignment: .bottomLeading) {
                if art != nil {
                    ZStack(alignment: .bottomLeading) {
                        LinearGradient(colors: [.clear, .black.opacity(0.75)], startPoint: .center, endPoint: .bottom)
                        Group {
                            if item.hasLogo == true {
                                AuthImage(url: client.imageURL(item: item.id, type: "logo"), token: client.token, fit: true)
                                    .frame(maxWidth: 130, maxHeight: 40, alignment: .bottomLeading)
                            } else {
                                Text(item.title).font(.subheadline.weight(.heavy)).foregroundStyle(.white).lineLimit(2)
                            }
                        }
                        .padding(.horizontal, 10).padding(.bottom, 12)
                    }
                }
            }
            .overlay {
                if showPlay {
                    Image(systemName: "play.fill").font(.callout).foregroundStyle(.white)
                        .frame(width: 34, height: 34).background(.black.opacity(0.5), in: Circle())
                        .overlay(Circle().strokeBorder(.white, lineWidth: 1.5))
                }
            }
            .overlay(alignment: .bottom) {
                if let p = progress(item) { ProgressStrip(value: p).padding(.horizontal, 8).padding(.bottom, 5) }
            }
            .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
            .contentShape(Rectangle())
    }
}

struct LibraryTile: View {
    let library: Library

    private var icon: String {
        switch library.kind.rawValue {
        case "movies": "film"
        case "shows": "tv"
        case "music": "music.note"
        default: "film.stack"
        }
    }

    var body: some View {
        HStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 6) {
                Text(library.name).font(Theme.display(28)).foregroundStyle(Theme.text)
                Text("home.items \(library.itemCount)").font(.subheadline).foregroundStyle(Theme.textMuted)
            }
            Spacer()
            Image(systemName: icon).font(.title2).foregroundStyle(Theme.accent.opacity(0.8))
        }
        .padding(20)
        .frame(height: 110)
        .background(
            RadialGradient(colors: [Color(hue: Theme.hue(for: library.name), saturation: 0.25, brightness: 0.16), Theme.surface],
                           center: .bottomLeading, startRadius: 0, endRadius: 360),
            in: RoundedRectangle(cornerRadius: Theme.cornerRadius, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.cornerRadius, style: .continuous).strokeBorder(Theme.border))
    }
}

struct LibraryView: View {
    let route: LibraryRoute
    let client: MediaClient
    @State private var items: [ItemSummary] = []
    @State private var total = 0
    @State private var loaded = false

    private let columns = [GridItem(.adaptive(minimum: 108), spacing: 14)]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(route.name).font(Theme.display(40)).foregroundStyle(Theme.text)
                    if loaded { Text("home.items \(total)").font(.subheadline).foregroundStyle(Theme.textMuted) }
                }
                LazyVGrid(columns: columns, spacing: 22) {
                    ForEach(items, id: \.id) { item in
                        NavigationLink(value: item.id) { PosterCard(item: item, client: client) }
                            .buttonStyle(CardPressStyle())
                            .task { if item.id == items.last?.id && items.count < total { await loadMore() } }
                    }
                }
                if !loaded { ProgressView().frame(maxWidth: .infinity).padding(40) }
            }
            .padding(.horizontal, 20)
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .task { if items.isEmpty { await loadMore() } }
    }

    private func loadMore() async {
        guard let page = try? await client.items(library: route.id, offset: items.count) else { return }
        items.append(contentsOf: page.items)
        total = page.total
        loaded = true
    }
}

struct ItemView: View {
    let itemID: String
    let client: MediaClient
    @State private var detail: ItemDetail?
    @State private var playing: PlayTarget?

    var body: some View {
        ScrollView {
            if let d = detail {
                VStack(alignment: .leading, spacing: 28) {
                    header(d)
                    content(d).padding(.horizontal, 20)
                }
                .padding(.bottom, 32)
            } else {
                ProgressView().frame(maxWidth: .infinity).padding(.top, 200)
            }
        }
        .ignoresSafeArea(edges: .top)
        .background(AmbientBackground(colors: detail?.item.colors))
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(.hidden, for: .navigationBar)
        .task { await reload() }
        .fullScreenCover(item: $playing, onDismiss: { Task { await reload() } }) { PlayerView(itemID: $0.id, client: client) }
    }

    private func reload() async { detail = try? await client.item(itemID) }

    @ViewBuilder private func header(_ d: ItemDetail) -> some View {
        let it = d.item
        let isEpisode = it.kind == .episode
        ZStack(alignment: .bottomLeading) {
            Group {
                if d.hasBackdrop {
                    AuthImage(url: client.imageURL(item: it.id, type: "backdrop"), token: client.token)
                } else if it.hasPoster {
                    AuthImage(url: client.imageURL(item: it.id, type: isEpisode ? "still" : "poster"), token: client.token)
                        .blur(radius: isEpisode ? 0 : 40)
                } else {
                    PosterPlaceholder(title: "")
                }
            }
            .frame(height: 480).frame(maxWidth: .infinity).clipped()
            .mask(LinearGradient(colors: [.black, .black, .black.opacity(0.6), .clear], startPoint: .top, endPoint: .bottom))
            LinearGradient(colors: [.black.opacity(0.45), .clear, .black.opacity(0.25), .black.opacity(0.45)],
                           startPoint: .top, endPoint: .bottom)
            HStack(alignment: .bottom, spacing: 16) {
                if !isEpisode {
                    AuthImage(url: it.hasPoster ? client.imageURL(item: it.id, type: "poster") : nil, token: client.token, title: it.title, year: it.year)
                        .frame(width: 112, height: 168)
                        .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                        .overlay(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous).strokeBorder(.white.opacity(0.12)))
                        .shadow(color: .black.opacity(0.7), radius: 20, y: 12)
                }
                VStack(alignment: .leading, spacing: 8) {
                    if isEpisode {
                        Text(episodeTitle(it)).font(Theme.display(34)).foregroundStyle(Theme.text).lineLimit(3).minimumScaleFactor(0.6)
                    } else {
                        TitleArt(id: it.id, title: it.title, hasLogo: d.hasLogo == true, client: client, size: 34, maxHeight: 90)
                    }
                    MetaLine(item: it)
                    if !d.genres.isEmpty {
                        Text(d.genres.joined(separator: " · ")).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(1)
                    }
                }
            }
            .padding(.horizontal, 20)
        }
        .frame(height: 480)
    }

    @ViewBuilder private func content(_ d: ItemDetail) -> some View {
        let it = d.item
        VStack(alignment: .leading, spacing: 18) {
            HStack(spacing: 12) {
                if !d.files.isEmpty {
                    Button { playing = PlayTarget(id: it.id) } label: {
                        Label(it.positionMs != nil ? "player.resume" : "player.play", systemImage: "play.fill").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(PrimaryButtonStyle(hex: it.colors?.first))
                }
                Button { Task { try? await client.setUserData(item: it.id, favorite: !(it.favorite ?? false)); await reload() } } label: {
                    Image(systemName: it.favorite == true ? "checkmark" : "plus")
                }
                .buttonStyle(CircleButtonStyle(active: it.favorite == true))
                .accessibilityLabel(Text("library.addToList"))
                Button { Task { try? await client.setUserData(item: it.id, played: !(it.played ?? false)); await reload() } } label: {
                    Image(systemName: "checkmark")
                }
                .buttonStyle(CircleButtonStyle(active: it.played == true))
                .accessibilityLabel(Text("library.watched"))
            }
            if let p = progress(it) { ProgressStrip(value: p).frame(maxWidth: 240) }
            if let tagline = d.tagline { Text(tagline).font(Theme.display(22).italic()).foregroundStyle(Theme.text.opacity(0.9)) }
            if let overview = d.overview { Text(overview).font(.callout).foregroundStyle(Theme.text.opacity(0.82)).lineSpacing(3) }
        }

        let seasons = d.children.filter { $0.kind == .season }
        let episodes = d.children.filter { $0.kind == .episode }
        if !seasons.isEmpty {
            VStack(alignment: .leading, spacing: 12) {
                Text("library.seasons").font(.title3.bold())
                ScrollView(.horizontal, showsIndicators: false) {
                    LazyHStack(alignment: .top, spacing: 14) {
                        ForEach(seasons, id: \.id) { s in
                            NavigationLink(value: s.id) { PosterCard(item: s, client: client).frame(width: 118) }
                                .buttonStyle(CardPressStyle())
                        }
                    }
                }
            }
        }
        if !episodes.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                Text("library.episodes").font(.title3.bold())
                ForEach(episodes, id: \.id) { e in
                    NavigationLink(value: e.id) { EpisodeRow(item: e, client: client) }.buttonStyle(CardPressStyle())
                }
            }
        }
        if !d.cast.isEmpty {
            VStack(alignment: .leading, spacing: 12) {
                Text("library.cast").font(.title3.bold())
                ScrollView(.horizontal, showsIndicators: false) {
                    LazyHStack(alignment: .top, spacing: 14) {
                        ForEach(Array(d.cast.prefix(20).enumerated()), id: \.offset) { _, p in
                            VStack(spacing: 6) {
                                Text(initials(p.name)).font(Theme.display(24)).foregroundStyle(Theme.text.opacity(0.7))
                                    .frame(width: 72, height: 72)
                                    .overlay {
                                        if let url = p.imageUrl.flatMap(client.absolute) {
                                            AuthImage(url: url, token: client.token).clipShape(Circle())
                                        }
                                    }
                                    .background(RadialGradient(colors: [Color(hue: Theme.hue(for: p.name), saturation: 0.25, brightness: 0.22), Theme.surface],
                                                               center: .topLeading, startRadius: 0, endRadius: 80), in: Circle())
                                    .overlay(Circle().strokeBorder(Theme.border))
                                Text(p.name).font(.caption.weight(.medium)).lineLimit(2).multilineTextAlignment(.center)
                                if let role = p.role { Text(role).font(.caption2).foregroundStyle(Theme.textMuted).lineLimit(1) }
                            }
                            .frame(width: 92)
                        }
                    }
                }
            }
        }
    }

    private func initials(_ name: String) -> String {
        name.split(separator: " ").prefix(2).compactMap(\.first).map { String($0).uppercased() }.joined()
    }
}

func episodeTitle(_ it: ItemSummary) -> String {
    guard let e = it.episodeNumber else { return it.title }
    return "E\(String(format: "%02d", e)) · \(it.title)"
}

struct EpisodeRow: View {
    let item: ItemSummary
    let client: MediaClient

    var body: some View {
        HStack(spacing: 14) {
            AuthImage(url: item.hasPoster ? client.imageURL(item: item.id, type: "still") : nil, token: client.token)
                .frame(width: 128, height: 72)
                .overlay(alignment: .bottom) { if let p = progress(item) { ProgressStrip(value: p).padding(6) } }
                .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(Theme.border))
            VStack(alignment: .leading, spacing: 3) {
                if let e = item.episodeNumber {
                    Text("E\(String(format: "%02d", e))").font(.caption.weight(.semibold).monospacedDigit()).foregroundStyle(Theme.accent)
                }
                Text(item.title).font(.subheadline.weight(.medium)).foregroundStyle(Theme.text).lineLimit(2)
            }
            Spacer(minLength: 0)
            if item.played == true { Image(systemName: "checkmark").font(.footnote.bold()).foregroundStyle(Theme.accent) }
        }
        .contentShape(Rectangle())
    }
}

/// Search tab with debounced queries.
struct SearchTab: View {
    let client: MediaClient
    @State private var query = ""
    @State private var results: [ItemSummary] = []
    @State private var searched = false

    private let columns = [GridItem(.adaptive(minimum: 108), spacing: 14)]

    var body: some View {
        NavigationStack {
            ScrollView {
                if searched && results.isEmpty {
                    ContentUnavailableView.search(text: query).padding(.top, 60)
                } else {
                    LazyVGrid(columns: columns, spacing: 22) {
                        ForEach(results, id: \.id) { item in
                            NavigationLink(value: item.id) { PosterCard(item: item, client: client) }.buttonStyle(CardPressStyle())
                        }
                    }
                    .padding(.horizontal, 20)
                }
            }
            .background(Theme.background)
            .navigationTitle(Text("nav.search"))
            .searchable(text: $query, prompt: Text("search.prompt"))
            .navigationDestination(for: String.self) { ItemView(itemID: $0, client: client) }
            .task(id: query) {
                let q = query.trimmingCharacters(in: .whitespaces)
                guard !q.isEmpty else { results = []; searched = false; return }
                try? await Task.sleep(for: .milliseconds(300))
                guard !Task.isCancelled else { return }
                results = (try? await client.search(q)) ?? []
                searched = true
            }
        }
    }
}

/// Full-screen AVPlayer with resume, progress reporting, skip intro and the next episode.
struct PlayerView: View {
    let itemID: String
    let client: MediaClient
    @Environment(\.dismiss) private var dismiss
    @State private var current: String = ""
    @State private var player: AVPlayer?
    @State private var info: PlaybackInfo?
    @State private var error: String?
    @State private var observer: Any?
    @State private var position: Int64 = 0
    @State private var lastReport = Date.distantPast
    @State private var next: ItemSummary?
    @State private var autoplay = true
    @State private var countdown = 10
    @State private var dismissedNext = false
    @State private var skippedIntro = false
    @State private var ended = false

    private var duration: Int64 { info?.durationMs ?? 0 }

    private func segment(_ kinds: [Components.Schemas.MediaSegment.kindPayload]) -> Components.Schemas.MediaSegment? {
        info?.segments?.first { kinds.contains($0.kind) && position >= $0.startMs && position < $0.endMs - 1000 }
    }

    private var showNext: Bool {
        guard next != nil, !dismissedNext else { return false }
        return ended || segment([.credits]) != nil || (duration > 0 && position > 0 && duration - position <= 20_000)
    }

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if let player {
                VideoPlayer(player: player).ignoresSafeArea()
            } else if let error {
                Text(error).foregroundStyle(.white).padding()
            } else {
                ProgressView().tint(.white).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            VStack {
                HStack {
                    Button { close() } label: {
                        Image(systemName: "xmark.circle.fill").font(.title).foregroundStyle(.white.opacity(0.8)).padding()
                    }
                    .accessibilityLabel(Text("player.back"))
                    Spacer()
                    if let tracks = info?.audio, tracks.count > 1 {
                        Menu {
                            ForEach(Array(tracks.enumerated()), id: \.offset) { idx, a in
                                Button {
                                    Task { await restart(audioIndex: idx) }
                                } label: {
                                    Text([a.language?.uppercased() ?? "#\(idx + 1)", a.codec.uppercased(), a.title].compactMap { $0 }.joined(separator: " "))
                                }
                            }
                        } label: {
                            Image(systemName: "speaker.wave.2.circle.fill").font(.title).foregroundStyle(.white.opacity(0.8)).padding()
                        }
                        .accessibilityLabel(Text("player.audio"))
                    }
                }
                Spacer()
                HStack(alignment: .bottom) {
                    Spacer()
                    if showNext, let next {
                        nextCard(next)
                    } else if let seg = segment([.intro, .recap]), !skippedIntro {
                        Button {
                            skippedIntro = true
                            Task { await player?.seek(to: CMTime(value: seg.endMs - offsetMs, timescale: 1000)) }
                        } label: {
                            Label(seg.kind == .recap ? "player.skipRecap" : "player.skipIntro", systemImage: "forward.end.fill")
                        }
                        .buttonStyle(MutedButtonStyle())
                    }
                }
                .padding(.horizontal, 24).padding(.bottom, 90)
            }
        }
        .task {
            current = itemID
            autoplay = (try? await client.preferences().autoplayNext) ?? true
            await start()
        }
        .onChange(of: showNext) { _, shown in
            if shown { countdown = 10 }
        }
        .task(id: showNext) {
            guard showNext, autoplay else { return }
            while countdown > 0 && !Task.isCancelled {
                try? await Task.sleep(for: .seconds(1))
                countdown -= 1
            }
            if !Task.isCancelled && showNext { await playNext() }
        }
    }

    private func nextCard(_ n: ItemSummary) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Group {
                if autoplay { Text("player.nextIn \(max(0, countdown))") } else { Text("player.upNext") }
            }
            .font(.caption).foregroundStyle(.white.opacity(0.7))
            Text(episodeTitle(n)).font(.subheadline.bold()).foregroundStyle(.white).lineLimit(2)
            HStack {
                Button { Task { await playNext() } } label: { Label("player.playNext", systemImage: "play.fill") }
                    .buttonStyle(PrimaryButtonStyle())
                Button { dismissedNext = true } label: { Image(systemName: "xmark") }
                    .buttonStyle(MutedButtonStyle())
                    .accessibilityLabel(Text("player.back"))
            }
        }
        .padding(14)
        .frame(maxWidth: 320, alignment: .leading)
        .background(.black.opacity(0.8), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    private var offsetMs: Int64 { info?.method == .direct ? 0 : (info?.startMs ?? 0) }

    private func positionMs() -> Int64 {
        guard let player else { return 0 }
        let seconds = player.currentTime().seconds
        return seconds.isFinite ? offsetMs + Int64(seconds * 1000) : 0
    }

    private func teardown() {
        if let observer { player?.removeTimeObserver(observer) }
        observer = nil
        player?.pause()
        player = nil
    }

    /// Switches the audio track: a new stream from the current position.
    private func restart(audioIndex: Int) async {
        let pos = positionMs()
        teardown()
        await start(startMs: pos, audioIndex: audioIndex)
    }

    private func start(startMs: Int64? = nil, audioIndex: Int? = nil) async {
        do {
            let info = try await client.startPlayback(item: current, startMs: startMs, audioIndex: audioIndex)
            guard let url = client.absolute(info.url) else { throw APIError(status: 0, code: "bad_url") }
            self.info = info
            let p = AVPlayer(url: url)
            if info.method == .direct && info.startMs > 0 {
                await p.seek(to: CMTime(value: info.startMs, timescale: 1000))
            }
            observer = p.addPeriodicTimeObserver(forInterval: CMTime(seconds: 1, preferredTimescale: 1), queue: .main) { _ in
                Task { @MainActor in
                    position = positionMs()
                    if let pl = player, let item = pl.currentItem, item.duration.isNumeric, pl.currentTime() >= item.duration { ended = true }
                    if Date().timeIntervalSince(lastReport) >= 10 {
                        lastReport = Date()
                        await report()
                    }
                }
            }
            player = p
            p.play()
            next = try? await client.nextEpisode(current)
        } catch {
            self.error = String(localized: "player.error")
        }
    }

    private func playNext() async {
        guard let n = next else { return }
        await report()
        teardown()
        current = n.id
        info = nil
        next = nil
        position = 0
        ended = false
        dismissedNext = false
        skippedIntro = false
        await start(startMs: 0)
    }

    private func report() async {
        let pos = positionMs()
        if pos > 0 { await client.reportProgress(item: current, positionMs: pos, durationMs: info?.durationMs) }
    }

    private func close() {
        Task {
            await report()
            teardown()
            dismiss()
        }
    }
}


/// Approve a Quick Connect code shown on a TV or browser with this phone's account.
struct QuickConnectApproveView: View {
    let client: MediaClient
    @Environment(\.dismiss) private var dismiss
    @State private var code = ""
    @State private var busy = false
    @State private var message: String?
    @State private var failed = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("quickConnect.code", text: $code)
                        .keyboardType(.numberPad)
                        .font(.title2.monospaced())
                } footer: {
                    Text("quickConnect.approveHint")
                }
                if let message {
                    Text(message).foregroundStyle(failed ? .red : Theme.accent)
                }
                Button {
                    Task { await approve() }
                } label: {
                    if busy { ProgressView() } else { Text("quickConnect.approve").bold() }
                }
                .disabled(busy || code.count < 6)
            }
            .navigationTitle(Text("quickConnect.approveTitle"))
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("player.back") { dismiss() } } }
        }
    }

    private func approve() async {
        busy = true
        defer { busy = false }
        do {
            let device = try await client.authorizeQuickConnect(code: code.trimmingCharacters(in: .whitespaces))
            failed = false
            message = String(localized: "quickConnect.approved \(device)")
            code = ""
        } catch {
            failed = true
            message = String(localized: "quickConnect.expired")
        }
    }
}
