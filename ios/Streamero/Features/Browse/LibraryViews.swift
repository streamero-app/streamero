import SwiftUI

/// All libraries; music libraries open the music view.
struct LibrariesTab: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var libraries: [Library] = []
    @State private var error: Error?
    @State private var loaded = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                PageTitle(title: "libraries").padding(.top, 8)
                if LiveTV.canWatch(app.me) {
                    NavigationLink(value: Route.liveTv) {
                        Label("livetv.title", systemImage: "dot.radiowaves.left.and.right").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(MutedButtonStyle())
                }
                if !loaded && error == nil { LoadingView() }
                ErrorLine(error: error)
                ForEach(libraries) { lib in
                    NavigationLink(value: Route.library(LibraryRoute(id: lib.id, name: lib.name, kind: lib.kind))) {
                        LibraryTile(library: lib)
                    }
                    .buttonStyle(CardPressStyle())
                }
                HStack(spacing: 10) {
                    NavigationLink(value: Route.collections) { Text("collections").frame(maxWidth: .infinity) }.buttonStyle(MutedButtonStyle())
                    NavigationLink(value: Route.favorites) { Text("my_list").frame(maxWidth: .infinity) }.buttonStyle(MutedButtonStyle())
                }
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .background(Theme.background)
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        do {
            libraries = try await client.libraries()
            error = nil
        } catch { self.error = error }
        loaded = true
    }
}

/// A library with sort, status and genre filters and infinite scroll.
struct LibraryView: View {
    let route: LibraryRoute
    let client: MediaClient
    @State private var items: [ItemSummary] = []
    @State private var total = -1
    @State private var loading = false
    @State private var sort = "added"
    @State private var status = ""
    @State private var genre = ""
    @State private var genres: [String] = []
    @State private var error: Error?

    private let columns = [GridItem(.adaptive(minimum: 108), spacing: 14)]

    private var sortOptions: [Opt<String>] {
        [Opt("added", "sort_added"), Opt("title", "sort_title"), Opt("year", "sort_year"), Opt("rating", "sort_rating"), Opt("random", "sort_random")]
    }
    private var statusOptions: [Opt<String>] {
        [Opt("", "status_all"), Opt("unplayed", "status_unplayed"), Opt("resumable", "status_resumable"), Opt("favorite", "my_list")]
    }

    private var filterKey: String { sort + "|" + status + "|" + genre }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text(verbatim: route.name).font(Theme.display(34)).foregroundStyle(Theme.text)
                if total >= 0 { Text("items_count \(total)").font(.subheadline).foregroundStyle(Theme.textMuted) }
                HStack(spacing: 8) {
                    filterMenu(selection: $sort, options: sortOptions)
                    filterMenu(selection: $status, options: statusOptions)
                    if !genres.isEmpty {
                        filterMenu(selection: $genre, options: [Opt("", "all_genres")] + genres.map { Opt($0, verbatim: $0) })
                    }
                    Spacer(minLength: 0)
                }
                ErrorLine(error: error)
                LazyVGrid(columns: columns, spacing: 22) {
                    ForEach(items) { item in
                        NavigationLink(value: Route.item(item.id)) { PosterCard(item: item, client: client) }
                            .buttonStyle(CardPressStyle())
                            .task(id: item.id) {
                                if item.id == items.last?.id && total > items.count { await load(reset: false) }
                            }
                    }
                }
                if loading && items.isEmpty { LoadingView() }
                if total == 0 { Text("nothing_here").foregroundStyle(Theme.textMuted).padding(.top, 24) }
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .task { genres = (try? await client.genres(library: route.id)) ?? [] }
        .task(id: filterKey) { await load(reset: true) }
    }

    private func filterMenu(selection: Binding<String>, options: [Opt<String>]) -> some View {
        Menu {
            Picker(selection: selection) {
                ForEach(options) { $0.label.tag($0.value) }
            } label: { EmptyView() }
        } label: {
            HStack(spacing: 4) {
                (options.first { $0.value == selection.wrappedValue }?.label ?? Text("status_all"))
                    .lineLimit(1)
                Image(systemName: "chevron.down").font(.caption2)
            }
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(Theme.text)
            .padding(.horizontal, 12).frame(height: 40)
            .background(Theme.surface2, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
        }
    }

    private func load(reset: Bool) async {
        guard reset || !loading else { return }
        loading = true
        defer { loading = false }
        var query: [String: String?] = ["sort": sort, "genre": genre.isEmpty ? nil : genre, "limit": "60", "offset": String(reset ? 0 : items.count)]
        if !status.isEmpty { query[status] = "true" }
        do {
            let page = try await client.items(library: route.id, query: query)
            if reset { items = page.items } else {
                let known = Set(items.map(\.id))
                items += page.items.filter { !known.contains($0.id) }
            }
            total = page.total
            error = nil
        } catch is CancellationError {
            return
        } catch {
            if !Task.isCancelled { self.error = error }
        }
    }
}

/// My list.
struct FavoritesView: View {
    let client: MediaClient
    @State private var items: [ItemSummary]?
    @State private var error: Error?

    var body: some View {
        ItemGridPage(title: "my_list", items: items, error: error, client: client)
            .task { await load() }
            .refreshable { await load() }
    }

    private func load() async {
        do {
            items = try await client.favorites()
            error = nil
        } catch { self.error = error }
    }
}

/// A titled poster grid (collections, playlists, favorites).
struct ItemGridPage: View {
    var title: LocalizedStringKey? = nil
    var titleText: String? = nil
    var overview: String? = nil
    let items: [ItemSummary]?
    let error: Error?
    let client: MediaClient
    var playAll: (() -> Void)? = nil

    private let columns = [GridItem(.adaptive(minimum: 108), spacing: 14)]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                if let title {
                    Text(title).font(Theme.display(34)).foregroundStyle(Theme.text)
                } else if let titleText {
                    Text(verbatim: titleText).font(Theme.display(34)).foregroundStyle(Theme.text)
                }
                if let overview { Text(verbatim: overview).foregroundStyle(Theme.textMuted) }
                if let playAll {
                    Button(action: playAll) { Label("player.play", systemImage: "play.fill") }.buttonStyle(PrimaryButtonStyle())
                }
                ErrorLine(error: error)
                if items == nil && error == nil { LoadingView() }
                LazyVGrid(columns: columns, spacing: 22) {
                    ForEach(items ?? []) { item in
                        NavigationLink(value: Route.item(item.id)) { PosterCard(item: item, client: client) }
                            .buttonStyle(CardPressStyle())
                    }
                }
                if items?.isEmpty == true { Text("nothing_here").foregroundStyle(Theme.textMuted) }
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
    }
}

struct CollectionTile: View {
    let collection: MediaCollection
    let client: MediaClient

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ZStack(alignment: .bottomLeading) {
                Theme.surface2
                if collection.hasPoster {
                    AuthImage(url: client.collectionImageURL(collection.id, type: "poster"), token: client.token, title: collection.name)
                } else {
                    Text(verbatim: collection.name).font(Theme.display(18)).foregroundStyle(Theme.text).padding(10).lineLimit(4)
                }
            }
            .aspectRatio(2 / 3, contentMode: .fit)
            .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
            Text(verbatim: collection.name).font(.footnote.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
            Text("items_count \(collection.itemCount)").font(.caption).foregroundStyle(Theme.textFaint)
        }
    }
}

/// Collections and playlists.
struct CollectionsView: View {
    let client: MediaClient
    @State private var collections: [MediaCollection]?
    @State private var playlists: [Playlist] = []
    @State private var error: Error?

    private let columns = [GridItem(.adaptive(minimum: 108), spacing: 14)]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                PageTitle(title: "collections")
                ErrorLine(error: error)
                if collections == nil && error == nil { LoadingView() }
                LazyVGrid(columns: columns, spacing: 22) {
                    ForEach(collections ?? []) { c in
                        NavigationLink(value: Route.collection(c.id)) { CollectionTile(collection: c, client: client) }.buttonStyle(CardPressStyle())
                    }
                }
                if !playlists.isEmpty {
                    SectionHead(title: "playlists").padding(.top, 12)
                    LazyVGrid(columns: columns, spacing: 22) {
                        ForEach(playlists) { p in
                            NavigationLink(value: Route.playlist(p.id)) { playlistTile(p) }.buttonStyle(CardPressStyle())
                        }
                    }
                }
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func playlistTile(_ p: Playlist) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Theme.surface2
                .aspectRatio(1, contentMode: .fit)
                .overlay { if let first = p.coverItemIds?.first { AuthImage(url: client.imageURL(item: first, type: "poster"), token: client.token, title: p.name) } }
                .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
            Text(verbatim: p.name).font(.footnote.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
            Text("items_count \(p.itemCount)").font(.caption).foregroundStyle(Theme.textFaint)
        }
    }

    private func load() async {
        do {
            collections = try await client.collections()
            error = nil
        } catch { self.error = error }
        playlists = (try? await client.playlists()) ?? []
    }
}

struct CollectionView: View {
    let id: String
    let client: MediaClient
    @State private var detail: CollectionDetail?
    @State private var error: Error?

    var body: some View {
        ItemGridPage(titleText: detail?.collection.name ?? "", overview: detail?.collection.overview, items: detail?.items, error: error, client: client)
            .task(id: id) {
                do { detail = try await client.collection(id); error = nil } catch { self.error = error }
            }
    }
}

struct PlaylistView: View {
    let id: String
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var detail: PlaylistDetail?
    @State private var error: Error?

    private var playAll: (() -> Void)? {
        guard let first = detail?.items.first else { return nil }
        return { app.play(PlayTarget(id: first.id)) }
    }

    var body: some View {
        ItemGridPage(titleText: detail?.playlist.name ?? "", items: detail?.items, error: error, client: client, playAll: playAll)
            .task(id: id) {
                do { detail = try await client.playlist(id); error = nil } catch { self.error = error }
            }
    }
}
