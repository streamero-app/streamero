import SwiftUI

private let albumColumns = [GridItem(.adaptive(minimum: 150), spacing: 14)]
private let mediaColumns = [GridItem(.adaptive(minimum: 96), spacing: 2)]

/// Which picture the viewer opens at.
struct PhotoViewerTarget: Identifiable, Hashable {
    let id: String
}

/// A photo library: folders (top-level albums and loose pictures), all photos, all videos.
struct PhotoLibraryView: View {
    let library: LibraryRoute
    let client: MediaClient
    @State private var tab = Tab.folders
    @State private var paged: PagedItems

    enum Tab: Hashable { case folders, photos, videos }

    init(library: LibraryRoute, client: MediaClient) {
        self.library = library
        self.client = client
        _paged = State(initialValue: PagedItems(client: client, libraryId: library.id, query: [:]))
    }

    private var query: [String: String?] {
        switch tab {
        case .folders: return ["sort": "title"]
        case .photos: return ["kind": "photo", "sort": "taken"]
        case .videos: return ["kind": "video", "sort": "taken"]
        }
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text(verbatim: library.name).font(Theme.display(34)).foregroundStyle(Theme.text).padding(.horizontal, 20)
                HStack(spacing: 8) {
                    tabButton(.folders, "photos.folders")
                    tabButton(.photos, "photos.photos")
                    tabButton(.videos, "photos.videos")
                }
                .padding(.horizontal, 20)
                PhotoGrid(paged: paged, libraryId: library.id, client: client)
            }
            .padding(.bottom, 20)
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: tab) { await paged.reload(query: query) }
        .refreshable { await paged.reload() }
    }

    private func tabButton(_ value: Tab, _ title: LocalizedStringKey) -> some View {
        Button { tab = value } label: { Text(title) }.buttonStyle(ToggleCapsuleStyle(active: tab == value))
    }
}

/// One album: its sub-albums first, then its photos and videos by date taken.
struct PhotoAlbumView: View {
    let route: PhotoAlbumRoute
    let client: MediaClient
    @State private var paged: PagedItems

    init(route: PhotoAlbumRoute, client: MediaClient) {
        self.route = route
        self.client = client
        _paged = State(initialValue: PagedItems(client: client, libraryId: route.libraryId,
                                                query: ["parentId": route.id, "sort": "taken"]))
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text(verbatim: route.title).font(Theme.display(30)).foregroundStyle(Theme.text).padding(.horizontal, 20)
                if paged.total >= 0 {
                    Text("items_count \(paged.total)").font(.subheadline).foregroundStyle(Theme.textMuted).padding(.horizontal, 20)
                }
                PhotoGrid(paged: paged, libraryId: route.libraryId, client: client)
            }
            .padding(.bottom, 20)
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .task { await paged.reload() }
        .refreshable { await paged.reload() }
    }
}

/// Album tiles, then a tight grid of thumbnails; a picture opens the full-screen viewer.
struct PhotoGrid: View {
    let paged: PagedItems
    let libraryId: String
    let client: MediaClient
    @Environment(AppState.self) private var app
    @Environment(\.locale) private var locale
    @State private var viewing: PhotoViewerTarget?

    var body: some View {
        let albums = paged.items.filter { $0.kind == "photoalbum" }
        let media = paged.items.filter { $0.kind != "photoalbum" }
        VStack(alignment: .leading, spacing: 18) {
            ErrorLine(error: paged.error).padding(.horizontal, 20)
            if !albums.isEmpty {
                LazyVGrid(columns: albumColumns, spacing: 18) {
                    ForEach(albums) { album in
                        NavigationLink(value: Route.photoAlbum(PhotoAlbumRoute(libraryId: libraryId, id: album.id, title: album.title))) {
                            AlbumTile(album: album, client: client)
                        }
                        .buttonStyle(CardPressStyle())
                        .task(id: album.id) { await paged.loadMoreIfNeeded(after: album) }
                    }
                }
                .padding(.horizontal, 20)
            }
            if !media.isEmpty {
                LazyVGrid(columns: mediaColumns, spacing: 2) {
                    ForEach(media) { item in
                        Button { viewing = PhotoViewerTarget(id: item.id) } label: { PhotoThumb(item: item, client: client) }
                            .buttonStyle(CardPressStyle())
                            .task(id: item.id) { await paged.loadMoreIfNeeded(after: item) }
                    }
                }
            }
            if paged.loading && paged.items.isEmpty { LoadingView() }
            if paged.total == 0 { Text("nothing_here").foregroundStyle(Theme.textMuted).padding(.horizontal, 20).padding(.top, 24) }
        }
        .fullScreenCover(item: $viewing) { target in
            PhotoViewer(paged: paged, startId: target.id, client: client)
                .environment(app)
                .environment(\.locale, locale)
        }
    }
}

struct AlbumTile: View {
    let album: ItemSummary
    let client: MediaClient

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Theme.surface2
                .aspectRatio(4 / 3, contentMode: .fit)
                .overlay {
                    Image(systemName: "photo.on.rectangle").font(.title2).foregroundStyle(Theme.textFaint)
                    if album.hasPoster {
                        AuthImage(url: client.imageURL(item: album.id, type: "poster"), token: client.token)
                    }
                }
                .clipShape(RoundedRectangle(cornerRadius: Theme.cornerRadius, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: Theme.cornerRadius, style: .continuous).strokeBorder(Theme.border))
            Text(verbatim: album.title).font(.footnote.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
            if let n = album.childCount {
                Text("items_count \(n)").font(.caption2).foregroundStyle(Theme.textMuted)
            }
        }
    }
}

/// A square thumbnail; videos carry their length.
struct PhotoThumb: View {
    let item: ItemSummary
    let client: MediaClient

    var body: some View {
        Theme.surface2
            .aspectRatio(1, contentMode: .fit)
            .overlay {
                if item.hasPoster {
                    AuthImage(url: client.imageURL(item: item.id, type: "poster"), token: client.token)
                } else {
                    Image(systemName: item.kind == "video" ? "video" : "photo").foregroundStyle(Theme.textFaint)
                }
            }
            .overlay(alignment: .bottomTrailing) {
                if item.kind == "video" {
                    HStack(spacing: 3) {
                        Image(systemName: "play.fill")
                        if let d = item.durationMs, d > 0 { Text(verbatim: clock(d)) }
                    }
                    .font(.caption2.weight(.bold).monospacedDigit())
                    .foregroundStyle(.white)
                    .padding(.horizontal, 5).padding(.vertical, 2)
                    .background(.black.opacity(0.6), in: RoundedRectangle(cornerRadius: 3))
                    .padding(5)
                }
            }
            .clipped()
            .contentShape(Rectangle())
    }
}

// MARK: Viewer

/// Full-screen pictures of a listing: swipe between them, pinch or double-tap to zoom, tap for
/// the controls. Videos open the normal player.
struct PhotoViewer: View {
    let paged: PagedItems
    let client: MediaClient
    @Environment(AppState.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.locale) private var locale
    @State private var current: String
    @State private var chrome = true
    @State private var infoFor: ItemSummary?
    @State private var playTarget: PlayTarget?

    init(paged: PagedItems, startId: String, client: MediaClient) {
        self.paged = paged
        self.client = client
        _current = State(initialValue: startId)
    }

    private var media: [ItemSummary] { paged.items.filter { $0.kind != "photoalbum" } }
    private var currentItem: ItemSummary? { media.first { $0.id == current } }

    var body: some View {
        TabView(selection: $current) {
            ForEach(media) { item in
                PhotoPage(item: item, client: client,
                          onTap: { withAnimation(.easeOut(duration: 0.2)) { chrome.toggle() } },
                          onPlay: {
                              app.audiobook.pause()
                              playTarget = PlayTarget(id: item.id)
                          })
                    .tag(item.id)
                    .task(id: item.id) { await paged.loadMoreIfNeeded(after: item) }
            }
        }
        .tabViewStyle(.page(indexDisplayMode: .never))
        .background(Color.black)
        .ignoresSafeArea()
        .overlay(alignment: .top) {
            if chrome {
                HStack(spacing: 12) {
                    Button { dismiss() } label: {
                        Image(systemName: "xmark").font(.body.weight(.semibold)).frame(width: 40, height: 40)
                    }
                    .accessibilityLabel(Text("close"))
                    Spacer(minLength: 0)
                    if let item = currentItem {
                        Button { infoFor = item } label: {
                            Image(systemName: "info.circle").font(.body.weight(.semibold)).frame(width: 40, height: 40)
                        }
                        .accessibilityLabel(Text("photos.info"))
                    }
                }
                .foregroundStyle(.white)
                .padding(.horizontal, 12)
                .background(LinearGradient(colors: [.black.opacity(0.6), .clear], startPoint: .top, endPoint: .bottom).ignoresSafeArea())
                .transition(.opacity)
            }
        }
        .overlay(alignment: .bottom) {
            if chrome, let item = currentItem {
                VStack(spacing: 2) {
                    Text(verbatim: item.title).font(.footnote.weight(.semibold)).lineLimit(1)
                    if let date = ServerDate.parse(item.takenAt) {
                        Text(date, format: .dateTime.day().month(.wide).year().hour().minute()).font(.caption)
                            .foregroundStyle(.white.opacity(0.7))
                    }
                }
                .foregroundStyle(.white)
                .frame(maxWidth: .infinity)
                .padding(.top, 24).padding(.bottom, 12)
                .background(LinearGradient(colors: [.clear, .black.opacity(0.6)], startPoint: .top, endPoint: .bottom).ignoresSafeArea())
                .transition(.opacity)
            }
        }
        .statusBarHidden(!chrome)
        .persistentSystemOverlays(chrome ? .automatic : .hidden)
        .sheet(item: $infoFor) { item in PhotoInfoSheet(item: item, client: client) }
        .fullScreenCover(item: $playTarget) { target in
            PlayerView(target: target, client: client, music: app.music, together: app.together)
                .environment(app)
                .environment(\.locale, locale)
        }
    }
}

/// One page of the viewer: a zoomable picture (thumbnail first, then the full one) or a
/// video's still with a play button.
struct PhotoPage: View {
    let item: ItemSummary
    let client: MediaClient
    let onTap: () -> Void
    let onPlay: () -> Void
    @State private var full: URL?

    private var thumb: URL? { item.hasPoster ? client.imageURL(item: item.id, type: "poster") : nil }

    var body: some View {
        if item.kind == "video" {
            ZStack {
                Color.black
                AuthImage(url: thumb, token: client.token, fit: true)
                Button(action: onPlay) {
                    Image(systemName: "play.fill").font(.title).foregroundStyle(.white)
                        .frame(width: 72, height: 72).background(.black.opacity(0.55), in: Circle())
                        .overlay(Circle().strokeBorder(.white, lineWidth: 1.5))
                }
                .accessibilityLabel(Text("player.play"))
            }
            .contentShape(Rectangle())
            .onTapGesture { onTap() }
        } else {
            ZoomableImage(url: full ?? thumb, token: client.token) { _, _ in onTap() }
                .task(id: item.id) {
                    guard full == nil, let source = try? await client.photo(item.id) else { return }
                    full = client.absolute(source.url)
                }
        }
    }
}

/// Date, size and camera of a photo.
struct PhotoInfoSheet: View {
    let item: ItemSummary
    let client: MediaClient
    @Environment(\.dismiss) private var dismiss
    @State private var details: PhotoDetails?

    var body: some View {
        NavigationStack {
            List {
                if let date = ServerDate.parse(details?.takenAt ?? item.takenAt) {
                    row("photos.taken") { Text(date, format: .dateTime.day().month(.wide).year().hour().minute()) }
                }
                if let w = details?.width ?? item.width, let h = details?.height ?? item.height {
                    row("photos.dimensions") { Text(verbatim: "\(w) × \(h)") }
                }
                if let camera = details?.camera { row("photos.camera") { Text(verbatim: camera) } }
                if let lens = details?.lens { row("photos.lens") { Text(verbatim: lens) } }
                if let exposure = exposureLine, !exposure.isEmpty { row("photos.exposure") { Text(verbatim: exposure) } }
                row("name") { Text(verbatim: item.title) }
            }
            .listStyle(.insetGrouped)
            .scrollContentBackground(.hidden)
            .background(Theme.surface.ignoresSafeArea())
            .navigationTitle(Text("photos.info"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("close") { dismiss() } } }
        }
        .presentationDetents([.medium, .large])
        .task(id: item.id) { details = try? await client.item(item.id).photo }
    }

    private var exposureLine: String? {
        guard let d = details else { return nil }
        var parts: [String] = []
        if let f = d.fNumber { parts.append(String(format: "ƒ/%.1f", f)) }
        if let t = d.exposureTime { parts.append(t + " s") }
        if let iso = d.iso { parts.append("ISO \(iso)") }
        if let fl = d.focalLength { parts.append(String(format: "%.0f mm", fl)) }
        return parts.joined(separator: " · ")
    }

    private func row<V: View>(_ label: LocalizedStringKey, @ViewBuilder value: () -> V) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text(label).foregroundStyle(Theme.textMuted)
            Spacer(minLength: 12)
            value().foregroundStyle(Theme.text).multilineTextAlignment(.trailing)
        }
        .listRowBackground(Theme.surface2)
    }
}

/// A single photo opened on its own (search, links).
struct SinglePhotoView: View {
    let item: ItemSummary
    let client: MediaClient
    @State private var info = false

    var body: some View {
        PhotoPage(item: item, client: client, onTap: {}, onPlay: {})
            .background(Color.black.ignoresSafeArea())
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button { info = true } label: { Image(systemName: "info.circle") }
                        .accessibilityLabel(Text("photos.info"))
                }
            }
            .sheet(isPresented: $info) { PhotoInfoSheet(item: item, client: client) }
    }
}
