import AVKit
import SwiftUI

/// Signed-in home: continue watching and libraries.
struct HomeView: View {
    let server: SavedServer
    let user: Components.Schemas.User
    let client: MediaClient
    @Environment(SessionStore.self) private var session
    @State private var libraries: [Library] = []
    @State private var continueItems: [ItemSummary] = []
    @State private var error: LocalizedStringKey?
    @State private var showQuickConnect = false

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    if !continueItems.isEmpty {
                        Text("home.continue").font(.title2.bold())
                        ScrollView(.horizontal, showsIndicators: false) {
                            LazyHStack(spacing: 12) {
                                ForEach(continueItems, id: \.id) { item in
                                    NavigationLink(value: item.id) { PosterCard(item: item, client: client).frame(width: 120) }
                                }
                            }
                        }
                    }
                    Text("home.libraries").font(.title2.bold())
                    if libraries.isEmpty {
                        Text("home.noLibraries").foregroundStyle(Theme.textMuted)
                    }
                    ForEach(libraries, id: \.id) { lib in
                        NavigationLink(value: LibraryRoute(id: lib.id, name: lib.name)) {
                            HStack {
                                VStack(alignment: .leading) {
                                    Text(lib.name).font(.headline)
                                    Text("home.items \(lib.itemCount)").font(.subheadline).foregroundStyle(Theme.textMuted)
                                }
                                Spacer()
                                Image(systemName: "chevron.right").foregroundStyle(Theme.textMuted)
                            }
                            .padding()
                            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.cornerRadius))
                        }
                        .buttonStyle(.plain)
                    }
                    if let error { Text(error).foregroundStyle(.red) }
                }
                .padding()
            }
            .background(Theme.background)
            .navigationTitle(server.name)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Text(user.username)
                        Button("quickConnect.approveTitle") { showQuickConnect = true }
                        Button("nav.logout", role: .destructive) { Task { await session.signOut(server: server) } }
                    } label: { Image(systemName: "person.crop.circle") }
                }
            }
            .navigationDestination(for: LibraryRoute.self) { LibraryView(route: $0, client: client) }
            .navigationDestination(for: String.self) { ItemView(itemID: $0, client: client) }
            .task { await load() }
            .refreshable { await load() }
            .sheet(isPresented: $showQuickConnect) { QuickConnectApproveView(client: client) }
        }
    }

    private func load() async {
        do {
            async let libs = client.libraries()
            async let cont = client.continueWatching()
            libraries = try await libs
            continueItems = (try? await cont) ?? []
            error = nil
        } catch {
            self.error = "addServer.error.unreachable"
        }
    }
}

struct LibraryRoute: Hashable {
    let id: String
    let name: String
}

struct PosterCard: View {
    let item: ItemSummary
    let client: MediaClient

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            AuthImage(url: item.hasPoster ? client.imageURL(item: item.id, type: "poster") : nil, token: client.token, title: item.title)
                .aspectRatio(2 / 3, contentMode: .fit)
                .clipShape(RoundedRectangle(cornerRadius: 10))
            if let pos = item.positionMs, let dur = item.durationMs, dur > 0 {
                ProgressView(value: Double(pos), total: Double(dur)).tint(Theme.accent)
            }
            Text(item.title).font(.caption.weight(.medium)).lineLimit(1)
            Text(item.year.map(String.init) ?? " ").font(.caption2).foregroundStyle(Theme.textMuted)
        }
    }
}

struct LibraryView: View {
    let route: LibraryRoute
    let client: MediaClient
    @State private var items: [ItemSummary] = []
    @State private var total = 0

    private let columns = [GridItem(.adaptive(minimum: 110), spacing: 12)]

    var body: some View {
        ScrollView {
            LazyVGrid(columns: columns, spacing: 16) {
                ForEach(items, id: \.id) { item in
                    NavigationLink(value: item.id) { PosterCard(item: item, client: client) }
                        .buttonStyle(.plain)
                        .task { if item.id == items.last?.id && items.count < total { await loadMore() } }
                }
            }
            .padding()
        }
        .background(Theme.background)
        .navigationTitle(route.name)
        .task { if items.isEmpty { await loadMore() } }
    }

    private func loadMore() async {
        guard let page = try? await client.items(library: route.id, offset: items.count) else { return }
        items.append(contentsOf: page.items)
        total = page.total
    }
}

struct ItemView: View {
    let itemID: String
    let client: MediaClient
    @State private var detail: ItemDetail?
    @State private var playing = false

    var body: some View {
        ScrollView {
            if let d = detail {
                VStack(alignment: .leading, spacing: 16) {
                    HStack(alignment: .top, spacing: 16) {
                        if d.item.hasPoster {
                            AuthImage(url: client.imageURL(item: d.item.id, type: d.item.kind == .episode ? "still" : "poster"),
                                      token: client.token)
                                .frame(width: 120, height: d.item.kind == .episode ? 68 : 180)
                                .clipShape(RoundedRectangle(cornerRadius: 10))
                        }
                        VStack(alignment: .leading, spacing: 6) {
                            Text(d.item.title).font(.title2.bold())
                            if let year = d.item.year { Text(String(year)).foregroundStyle(Theme.textMuted) }
                            if let rating = d.item.rating { Text("★ \(rating, specifier: "%.1f")").foregroundStyle(Theme.accent) }
                        }
                    }
                    if !d.files.isEmpty {
                        Button { playing = true } label: {
                            Label(d.item.positionMs != nil ? "player.resume" : "player.play", systemImage: "play.fill")
                                .frame(maxWidth: .infinity).padding(.vertical, 12).bold()
                        }
                        .buttonStyle(.borderedProminent)
                    }
                    if let overview = d.overview { Text(overview) }
                    if !d.cast.isEmpty {
                        Text("library.cast").font(.headline)
                        Text(d.cast.prefix(10).map(\.name).joined(separator: ", ")).foregroundStyle(Theme.textMuted)
                    }
                    ForEach(d.children, id: \.id) { child in
                        NavigationLink(value: child.id) {
                            HStack {
                                if let e = child.episodeNumber { Text("E\(e)").font(.caption.monospaced()).foregroundStyle(Theme.textMuted) }
                                Text(child.title)
                                Spacer()
                                if child.played == true { Image(systemName: "checkmark").foregroundStyle(Theme.accent) }
                            }
                            .padding(.vertical, 8)
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding()
            } else {
                ProgressView().padding(40)
            }
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .task { detail = try? await client.item(itemID) }
        .fullScreenCover(isPresented: $playing, onDismiss: { Task { detail = try? await client.item(itemID) } }) {
            PlayerView(itemID: itemID, client: client)
        }
    }
}

/// Full-screen AVPlayer with resume and progress reporting.
struct PlayerView: View {
    let itemID: String
    let client: MediaClient
    @Environment(\.dismiss) private var dismiss
    @State private var player: AVPlayer?
    @State private var info: PlaybackInfo?
    @State private var error: String?
    @State private var observer: Any?

    var body: some View {
        ZStack(alignment: .topLeading) {
            Color.black.ignoresSafeArea()
            if let player {
                VideoPlayer(player: player).ignoresSafeArea()
            } else if let error {
                Text(error).foregroundStyle(.white).padding()
            } else {
                ProgressView().tint(.white).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
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
        }
        .task { await start() }
    }

    private var offsetMs: Int64 { info?.method == .direct ? 0 : (info?.startMs ?? 0) }

    private func positionMs() -> Int64 {
        guard let player else { return 0 }
        let seconds = player.currentTime().seconds
        return seconds.isFinite ? offsetMs + Int64(seconds * 1000) : 0
    }

    /// Switches the audio track: a new stream from the current position.
    private func restart(audioIndex: Int) async {
        let pos = positionMs()
        if let observer { player?.removeTimeObserver(observer) }
        observer = nil
        player?.pause()
        player = nil
        await start(startMs: pos, audioIndex: audioIndex)
    }

    private func start(startMs: Int64? = nil, audioIndex: Int? = nil) async {
        do {
            let info = try await client.startPlayback(item: itemID, startMs: startMs, audioIndex: audioIndex)
            guard let url = client.absolute(info.url) else { throw APIError(status: 0, code: "bad_url") }
            self.info = info
            let p = AVPlayer(url: url)
            if info.method == .direct && info.startMs > 0 {
                await p.seek(to: CMTime(value: info.startMs, timescale: 1000))
            }
            observer = p.addPeriodicTimeObserver(forInterval: CMTime(seconds: 10, preferredTimescale: 1), queue: .main) { _ in
                Task { @MainActor in await report() }
            }
            player = p
            p.play()
        } catch {
            self.error = String(localized: "player.error")
        }
    }

    private func report() async {
        let pos = positionMs()
        if pos > 0 { await client.reportProgress(item: itemID, positionMs: pos, durationMs: info?.durationMs) }
    }

    private func close() {
        Task {
            await report()
            if let observer { player?.removeTimeObserver(observer) }
            player?.pause()
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
