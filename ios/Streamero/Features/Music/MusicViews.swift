import SwiftUI

private let trackColumns = [GridItem(.flexible())]
private let coverColumns = [GridItem(.adaptive(minimum: 140), spacing: 14)]

private func coverURL(_ client: MediaClient, _ item: ItemSummary) -> URL? {
    guard item.hasPoster else { return nil }
    return client.imageURL(item: item.id, type: "poster")
}

/// Square album or artist cover (artist round) with an icon when there is no picture.
struct SquareCover: View {
    let item: ItemSummary
    let client: MediaClient
    var round = false

    var body: some View {
        ZStack {
            Theme.surface2
            Image(systemName: round ? "person.fill" : "opticaldisc").font(.title).foregroundStyle(Theme.textFaint)
            if let url = coverURL(client, item) { AuthImage(url: url, token: client.token) }
        }
        .aspectRatio(1, contentMode: .fit)
        .clipShape(round ? AnyShape(Circle()) : AnyShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous)))
    }
}

/// A music library: albums, artists and all songs.
struct MusicLibraryView: View {
    let library: LibraryRoute
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var tab = "album"
    @State private var page: ItemPage?
    @State private var error: Error?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text(verbatim: library.name).font(Theme.display(34)).textCase(.uppercase).foregroundStyle(Theme.text)
                HStack(spacing: 8) {
                    tabButton("album", "albums")
                    tabButton("artist", "artists")
                    tabButton("track", "songs")
                }
                Button { Task { await shuffleAll() } } label: { Label("shuffle_all", systemImage: "shuffle") }.buttonStyle(PrimaryButtonStyle())
                ErrorLine(error: error)
                if page == nil && error == nil { LoadingView() }
                content
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: tab) { await load() }
    }

    @ViewBuilder private var content: some View {
        let items = page?.items ?? []
        if tab == "track" {
            LazyVGrid(columns: trackColumns, spacing: 0) {
                ForEach(Array(items.enumerated()), id: \.element.id) { i, _ in
                    TrackRow(tracks: items, index: i, showAlbum: true, client: client)
                }
            }
        } else {
            LazyVGrid(columns: coverColumns, spacing: 18) {
                ForEach(items) { item in
                    NavigationLink(value: Route.item(item.id)) {
                        VStack(alignment: tab == "artist" ? .center : .leading, spacing: 8) {
                            SquareCover(item: item, client: client, round: tab == "artist")
                            Text(verbatim: item.title).font(.footnote.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
                            if tab == "album" {
                                Text(verbatim: [item.artist, item.year.map(String.init)].compactMap { $0 }.joined(separator: " · "))
                                    .font(.caption).foregroundStyle(Theme.textFaint).lineLimit(1)
                            }
                        }
                    }
                    .buttonStyle(CardPressStyle())
                }
            }
        }
    }

    private func tabButton(_ key: String, _ title: LocalizedStringKey) -> some View {
        Button { tab = key } label: { Text(title) }.buttonStyle(ToggleCapsuleStyle(active: tab == key))
    }

    private func load() async {
        page = nil
        do {
            page = try await client.items(library: library.id, query: ["kind": tab, "sort": "title", "limit": "200"])
            error = nil
        } catch is CancellationError { return } catch { self.error = error }
    }

    private func shuffleAll() async {
        guard let result = try? await client.items(library: library.id, query: ["kind": "track", "sort": "random", "limit": "200"]) else { return }
        app.music.play(result.items)
    }
}

struct TrackRow: View {
    let tracks: [ItemSummary]
    let index: Int
    var showAlbum = false
    let client: MediaClient
    @Environment(AppState.self) private var app

    var body: some View {
        let t = tracks[index]
        let active = app.music.current?.id == t.id
        Button { app.music.play(tracks, start: index) } label: {
            HStack(spacing: 14) {
                Text(verbatim: active ? "▶" : String(t.episodeNumber ?? (index + 1)))
                    .font(.subheadline).foregroundStyle(active ? Theme.accent : Theme.textFaint).frame(width: 26, alignment: .leading)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: t.title).foregroundStyle(active ? Theme.accent : Theme.text).fontWeight(active ? .semibold : .regular).lineLimit(1)
                    let sub = [t.artist, showAlbum ? t.album : nil].compactMap { $0 }.joined(separator: " · ")
                    if !sub.isEmpty { Text(verbatim: sub).font(.footnote).foregroundStyle(Theme.textFaint).lineLimit(1) }
                }
                Spacer(minLength: 0)
                if let d = t.durationMs { Text(verbatim: clock(d)).font(.footnote).foregroundStyle(Theme.textFaint) }
            }
            .padding(.vertical, 10)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

struct AlbumView: View {
    let detail: ItemDetail
    let client: MediaClient
    @Environment(AppState.self) private var app

    private var tracks: [ItemSummary] { detail.children.filter { $0.kind == "track" } }

    var body: some View {
        let it = detail.item
        let accent = Color(hex: it.colors?.first ?? "#d6763f")
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                SquareCover(item: it, client: client).frame(maxWidth: 280).frame(maxWidth: .infinity)
                Text("album").font(.caption2.bold()).textCase(.uppercase).foregroundStyle(Theme.textFaint)
                Text(verbatim: it.title).font(Theme.display(32)).textCase(.uppercase).foregroundStyle(Theme.text)
                Group {
                    let meta = [it.artist, it.year.map(String.init), detail.genres.first].compactMap { $0 }.joined(separator: " · ")
                    if let parent = detail.parentId {
                        NavigationLink(value: Route.item(parent)) { metaLine(meta) }
                    } else {
                        metaLine(meta)
                    }
                }
                HStack(spacing: 10) {
                    Button { app.music.play(tracks, start: 0) } label: { Label("player.play", systemImage: "play.fill") }.buttonStyle(PrimaryButtonStyle())
                    Button {
                        app.music.setShuffleOn()
                        app.music.play(tracks, start: Int.random(in: 0..<max(1, tracks.count)))
                    } label: { Label("shuffle", systemImage: "shuffle") }.buttonStyle(MutedButtonStyle())
                }
                ForEach(Array(tracks.enumerated()), id: \.element.id) { i, _ in
                    TrackRow(tracks: tracks, index: i, client: client)
                }
            }
            .padding(20)
        }
        .background(
            LinearGradient(colors: [accent.opacity(0.25), Theme.background, Theme.background], startPoint: .top, endPoint: .bottom).ignoresSafeArea()
        )
    }

    private func metaLine(_ meta: String) -> some View {
        (Text(verbatim: meta.isEmpty ? "" : meta + " · ") + Text("songs_count \(tracks.count)"))
            .font(.subheadline).foregroundStyle(Theme.textMuted)
    }
}

struct ArtistView: View {
    let detail: ItemDetail
    let client: MediaClient
    @Environment(AppState.self) private var app

    private var albums: [ItemSummary] {
        detail.children.filter { $0.kind == "album" }.sorted { ($0.year ?? 0) > ($1.year ?? 0) }
    }

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                SquareCover(item: detail.item, client: client, round: true).frame(width: 180, height: 180)
                Text(verbatim: detail.item.title).font(Theme.display(32)).textCase(.uppercase).foregroundStyle(Theme.text).multilineTextAlignment(.center)
                Text("albums_count \(albums.count)").foregroundStyle(Theme.textMuted)
                Button { Task { await playAll() } } label: { Label("play_all", systemImage: "play.fill") }.buttonStyle(PrimaryButtonStyle())
                LazyVGrid(columns: coverColumns, spacing: 18) {
                    ForEach(albums) { a in
                        NavigationLink(value: Route.item(a.id)) {
                            VStack(alignment: .leading, spacing: 4) {
                                SquareCover(item: a, client: client)
                                Text(verbatim: a.title).font(.footnote.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
                                if let y = a.year { Text(verbatim: String(y)).font(.caption).foregroundStyle(Theme.textFaint) }
                            }
                        }
                        .buttonStyle(CardPressStyle())
                    }
                }
                .padding(.top, 12)
            }
            .padding(20)
        }
        .background(Theme.background)
    }

    private func playAll() async {
        var all: [ItemSummary] = []
        for album in albums {
            if let d = try? await client.item(album.id) { all += d.children.filter { $0.kind == "track" } }
        }
        app.music.play(all)
    }
}

/// The music bar above the tab bar; tap to open the queue.
struct MiniPlayerBar: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var queueOpen = false

    var body: some View {
        let m = app.music
        if let c = m.current {
            VStack(spacing: 0) {
                GeometryReader { geo in
                    Rectangle().fill(.white.opacity(0.12))
                    Rectangle().fill(Theme.accent).frame(width: geo.size.width * pct(m))
                }
                .frame(height: 2)
                HStack(spacing: 10) {
                    Button { queueOpen = true } label: {
                        HStack(spacing: 10) {
                            AuthImage(url: client.imageURL(item: c.hasPoster ? c.id : (c.parentId ?? c.id), type: "poster"), token: client.token)
                                .frame(width: 44, height: 44)
                                .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: c.title).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
                                Text(verbatim: [c.artist, c.album].compactMap { $0 }.joined(separator: " · ")).font(.caption).foregroundStyle(Theme.textFaint).lineLimit(1)
                            }
                            Spacer(minLength: 0)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    Button { m.prev() } label: { Image(systemName: "backward.fill") }.accessibilityLabel(Text("previous"))
                    Button { m.toggle() } label: { Image(systemName: m.playing ? "pause.fill" : "play.fill") }.accessibilityLabel(Text("player.play"))
                    Button { m.next() } label: { Image(systemName: "forward.fill") }.accessibilityLabel(Text("next"))
                }
                .padding(.horizontal, 12).padding(.vertical, 8)
            }
            .background(Theme.surface)
            .foregroundStyle(Theme.text)
            .sheet(isPresented: $queueOpen) { QueueSheet(client: client) }
        }
    }

    private func pct(_ m: MusicPlayer) -> CGFloat {
        m.duration > 0 ? min(1, CGFloat(m.position) / CGFloat(m.duration)) : 0
    }
}

struct QueueSheet: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        let m = app.music
        VStack(alignment: .leading, spacing: 12) {
            if let c = m.current {
                Text(verbatim: c.title).font(Theme.display(26)).textCase(.uppercase).foregroundStyle(Theme.text).lineLimit(2)
                Text(verbatim: [c.artist, c.album].compactMap { $0 }.joined(separator: " · ")).foregroundStyle(Theme.textMuted).lineLimit(1)
            }
            Slider(value: Binding(get: { Double(m.position) }, set: { m.seek(Int64($0)) }), in: 0...max(1, Double(m.duration))).tint(Theme.accent)
            HStack {
                Text(verbatim: clock(m.position)).font(.caption).foregroundStyle(Theme.textFaint)
                Spacer()
                Button { m.toggleShuffle() } label: { Image(systemName: "shuffle").foregroundStyle(m.shuffle ? Theme.accent : Theme.textMuted) }
                    .accessibilityLabel(Text("shuffle"))
                Spacer()
                Button { m.prev() } label: { Image(systemName: "backward.fill").font(.title2) }.accessibilityLabel(Text("previous"))
                Spacer()
                Button { m.toggle() } label: {
                    Image(systemName: m.playing ? "pause.fill" : "play.fill").font(.title).foregroundStyle(Theme.background)
                        .frame(width: 56, height: 56).background(Theme.text, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                }
                .accessibilityLabel(Text("player.play"))
                Spacer()
                Button { m.next() } label: { Image(systemName: "forward.fill").font(.title2) }.accessibilityLabel(Text("next"))
                Spacer()
                Button { m.cycleRepeat() } label: {
                    Image(systemName: m.repeatMode == .one ? "repeat.1" : "repeat").foregroundStyle(m.repeatMode != .off ? Theme.accent : Theme.textMuted)
                }
                .accessibilityLabel(Text("repeat"))
                Spacer()
                Text(verbatim: clock(m.duration)).font(.caption).foregroundStyle(Theme.textFaint)
            }
            .foregroundStyle(Theme.text)
            SectionHead(title: "queue").padding(.top, 8)
            List {
                ForEach(Array(m.queue.enumerated()), id: \.offset) { i, t in
                    Button { m.jump(i) } label: {
                        HStack(spacing: 12) {
                            Text(verbatim: String(i + 1)).foregroundStyle(i == m.index ? Theme.accent : Theme.textFaint).frame(width: 24, alignment: .leading)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: t.title).foregroundStyle(i == m.index ? Theme.accent : Theme.text).lineLimit(1)
                                if let a = t.artist { Text(verbatim: a).font(.caption).foregroundStyle(Theme.textFaint).lineLimit(1) }
                            }
                        }
                    }
                    .listRowBackground(Color.clear)
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            Button { m.stop(); dismiss() } label: { Text("stop").foregroundStyle(Theme.textMuted) }
        }
        .padding(20)
        .background(Theme.surface.ignoresSafeArea())
    }
}
