import SwiftUI
import UIKit

// The adult section (D-053): scenes, studios and performers behind the account's grant and
// the viewer's PIN. Artwork is fetched with the session token, kept in memory only and
// blurred until tapped when the viewer asked for it. Nothing here can be downloaded.

/// Shows the PIN entry while the section is locked, and hides the screen from the app
/// switcher's snapshot.
struct AdultLockGate<Content: View>: View {
    let client: MediaClient
    @ViewBuilder let content: () -> Content
    @Environment(AppState.self) private var app
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        ZStack {
            if app.adult.locked {
                AdultPinView(client: client)
            } else {
                content()
            }
            if scenePhase != .active {
                Theme.background.ignoresSafeArea()
            }
        }
    }
}

/// Entry of the viewer's PIN.
struct AdultPinView: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var pin = ""
    @State private var busy = false
    @State private var error: Error?

    var body: some View {
        VStack(spacing: 18) {
            Spacer()
            Image(systemName: "lock.fill").font(.largeTitle).foregroundStyle(Theme.accent)
            Text("adult.locked.title").displayFont(26).foregroundStyle(Theme.text).multilineTextAlignment(.center)
            Text("adult.locked.hint").font(.subheadline).foregroundStyle(Theme.textMuted).multilineTextAlignment(.center)
            SecureField("adult.pin", text: $pin)
                .keyboardType(.numberPad)
                .font(.title2.monospaced())
                .multilineTextAlignment(.center)
                .padding(.horizontal, 12).frame(height: 50)
                .background(Theme.surface2, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                .frame(maxWidth: 260)
                .onSubmit { Task { await submit() } }
            ErrorLine(error: error)
            Button { Task { await submit() } } label: {
                Group {
                    if busy { ProgressView() } else { Text("adult.unlock") }
                }
                .frame(maxWidth: 220)
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(busy || !AdultGate.validPin(pin))
            Spacer()
            Spacer()
        }
        .padding(.horizontal, 32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.background)
    }

    private func submit() async {
        guard AdultGate.validPin(pin), !busy else { return }
        busy = true
        defer { busy = false }
        let code = pin
        pin = ""
        do {
            try await app.adult.unlock(client: client, pin: code)
            error = nil
        } catch {
            self.error = error
        }
    }
}

/// Artwork of the section: memory only, blurred while `blurKey` was not revealed.
struct AdultImage: View {
    let url: URL?
    let token: String
    /// nil = never blurred (studio logos).
    var blurKey: String? = nil
    var title: String = ""
    var fit = false
    @Environment(AppState.self) private var app
    @State private var image: UIImage?

    var body: some View {
        let blurred = blurKey.map { app.adult.isBlurred($0) } ?? false
        Color.clear
            .overlay {
                ZStack {
                    if !fit { PosterPlaceholder(title: title).opacity(image == nil ? 1 : 0) }
                    if let image {
                        Group {
                            if fit {
                                Image(uiImage: image).resizable().scaledToFit()
                            } else {
                                Image(uiImage: image).resizable().scaledToFill()
                            }
                        }
                        .blur(radius: blurred ? 30 : 0, opaque: true)
                        .transition(.opacity)
                    }
                }
            }
            .clipped()
            .overlay {
                if blurred && image != nil {
                    Image(systemName: "eye.slash").font(.title3).foregroundStyle(.white.opacity(0.85))
                        .shadow(color: .black.opacity(0.6), radius: 6)
                        .accessibilityLabel(Text("adult.tapToReveal"))
                }
            }
            .animation(.easeOut(duration: 0.3), value: image != nil)
            .animation(.easeOut(duration: 0.3), value: blurred)
            .task(id: url) {
                guard let url else { image = nil; return }
                image = await AdultImageCache.shared.image(url: url, token: token)
            }
    }
}

/// A card that opens its page, or, while its picture is blurred, reveals the picture first.
struct AdultCardLink<Content: View>: View {
    let route: Route
    /// nil = nothing to reveal.
    var revealKey: String? = nil
    @ViewBuilder let content: () -> Content
    @Environment(AppState.self) private var app

    var body: some View {
        if let key = revealKey, app.adult.isBlurred(key) {
            Button { app.adult.reveal(key) } label: { content() }
                .buttonStyle(CardPressStyle())
                .accessibilityHint(Text("adult.tapToReveal"))
        } else {
            NavigationLink(value: route) { content() }
                .buttonStyle(CardPressStyle())
        }
    }
}

/// Keys and URLs of the section's artwork.
enum AdultArt {
    static func sceneKey(_ id: String) -> String { "scene/\(id)" }
    static func performerKey(_ id: String) -> String { "performer/\(id)" }

    /// Landscape picture of a scene: the backdrop, else the poster.
    static func sceneURL(id: String, hasBackdrop: Bool, hasPoster: Bool, client: MediaClient) -> URL? {
        if hasBackdrop { return client.adultImageURL(owner: "scene", id: id, type: "backdrop") }
        if hasPoster { return client.adultImageURL(owner: "scene", id: id, type: "poster") }
        return nil
    }
}

struct AdultSearchField: View {
    @Binding var text: String

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass").foregroundStyle(Theme.textMuted)
            // No autocorrection: the keyboard should not learn what is typed here.
            TextField("adult.search", text: $text)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.search)
                .foregroundStyle(Theme.text)
            if !text.isEmpty {
                Button { text = "" } label: { Image(systemName: "xmark.circle.fill").foregroundStyle(Theme.textFaint) }
                    .accessibilityLabel(Text("adult.clearSearch"))
            }
        }
        .padding(.horizontal, 12).frame(height: 44)
        .background(Theme.surface2, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
    }
}

struct AdultFilterMenu: View {
    @Binding var selection: String
    let options: [Opt<String>]

    var body: some View {
        Menu {
            Picker(selection: $selection) {
                ForEach(options) { $0.label.tag($0.value) }
            } label: { EmptyView() }
        } label: {
            HStack(spacing: 4) {
                (options.first { $0.value == selection }?.label ?? options.first?.label ?? Text(verbatim: ""))
                    .lineLimit(1)
                Image(systemName: "chevron.down").font(.caption2)
            }
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(Theme.text)
            .padding(.horizontal, 12).frame(height: 40)
            .background(Theme.surface2, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
        }
    }
}

// MARK: Home

/// The section's start: scenes, studios and performers.
struct AdultHomeView: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @Environment(\.locale) private var locale
    @State private var section = "scenes"
    @State private var loaded = false
    @State private var loadError: Error?
    @State private var showSettings = false

    var body: some View {
        Group {
            if let loadError, !AdultGate.isLocked(loadError) {
                VStack(spacing: 16) {
                    if AdultGate.isGone(loadError) {
                        Text("adult.unavailable").foregroundStyle(Theme.textMuted)
                    } else {
                        ErrorLine(error: loadError)
                        Button { Task { await loadStatus() } } label: { Text("adult.retry") }
                            .buttonStyle(MutedButtonStyle())
                    }
                }
                .padding(20)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if !loaded {
                LoadingView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                AdultLockGate(client: client) { browse }
            }
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if loaded && loadError == nil {
                ToolbarItemGroup(placement: .topBarTrailing) {
                    if app.adult.status?.pinSet == true && !app.adult.locked {
                        Button { Task { await app.adult.lock(client: client) } } label: { Image(systemName: "lock") }
                            .accessibilityLabel(Text("adult.lock"))
                    }
                    Button { showSettings = true } label: { Image(systemName: "gearshape") }
                        .accessibilityLabel(Text("adult.settings"))
                }
            }
        }
        .sheet(isPresented: $showSettings) {
            AdultSettingsSheet(client: client)
                .environment(app)
                .environment(\.locale, locale)
        }
        .task { await loadStatus() }
    }

    private var browse: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                PageTitle(title: "adult.title").padding(.top, 8)
                Picker(selection: $section) {
                    Text("adult.scenes").tag("scenes")
                    Text("adult.studios").tag("studios")
                    Text("adult.performers").tag("performers")
                } label: { EmptyView() }
                .pickerStyle(.segmented)
                switch section {
                case "studios":
                    AdultStudioGrid(client: client)
                case "performers":
                    AdultPerformerGrid(client: client)
                default:
                    AdultSceneGrid(client: client, libraries: app.adult.status?.libraries ?? [])
                }
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .scrollDismissesKeyboard(.immediately)
    }

    private func loadStatus() async {
        do {
            try await app.adult.refresh(client: client)
            loadError = nil
        } catch is CancellationError {
            return
        } catch {
            loadError = error
        }
        loaded = true
    }
}

// MARK: Scenes

/// Scenes with search, kind, status and sort, loaded a page at a time.
struct AdultSceneGrid: View {
    let client: MediaClient
    var base = AdultSceneFilter()
    var libraries: [AdultLibrary] = []
    var showsKind = true
    @Environment(AppState.self) private var app
    @State private var q = ""
    @State private var library = ""
    @State private var kind = ""
    @State private var status = "all"
    @State private var sort = "date"
    @State private var scenes: [AdultScene] = []
    @State private var total = -1
    @State private var loading = false
    @State private var error: Error?

    private let columns = [GridItem(.adaptive(minimum: 160), spacing: 14)]

    private var filter: AdultSceneFilter {
        var f = base
        if !library.isEmpty { f.libraryId = library }
        f.kind = kind
        f.status = status
        f.sort = sort
        f.q = q
        return f
    }

    private var kindOptions: [Opt<String>] {
        [Opt("", "adult.kind.all"), Opt("scene", "adult.kind.scene"), Opt("movie", "adult.kind.movie")]
    }
    private var statusOptions: [Opt<String>] {
        [Opt("all", "adult.status.all"), Opt("unwatched", "adult.status.unwatched"),
         Opt("inProgress", "adult.status.inProgress"), Opt("watched", "adult.status.watched")]
    }
    private var sortOptions: [Opt<String>] {
        [Opt("date", "adult.sort.date"), Opt("added", "adult.sort.added"), Opt("title", "adult.sort.title"), Opt("played", "adult.sort.played")]
    }
    private var libraryOptions: [Opt<String>] {
        [Opt("", "adult.allLibraries")] + libraries.map { Opt($0.id, verbatim: $0.name) }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            AdultSearchField(text: $q)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    if libraries.count > 1 { AdultFilterMenu(selection: $library, options: libraryOptions) }
                    if showsKind { AdultFilterMenu(selection: $kind, options: kindOptions) }
                    AdultFilterMenu(selection: $status, options: statusOptions)
                    AdultFilterMenu(selection: $sort, options: sortOptions)
                }
            }
            if total >= 0 { Text("adult.scenesCount \(total)").font(.subheadline).foregroundStyle(Theme.textMuted) }
            ErrorLine(error: error)
            LazyVGrid(columns: columns, alignment: .leading, spacing: 20) {
                ForEach(scenes) { scene in
                    AdultCardLink(route: .adultScene(scene.id),
                                  revealKey: scene.hasBackdrop || scene.hasPoster ? AdultArt.sceneKey(scene.id) : nil) {
                        AdultSceneCard(scene: scene, client: client)
                    }
                    .task(id: scene.id) {
                        if scene.id == scenes.last?.id && total > scenes.count { await load(reset: false) }
                    }
                }
            }
            if loading && scenes.isEmpty { LoadingView() }
            if total == 0 { Text("nothing_here").foregroundStyle(Theme.textMuted).padding(.top, 24) }
        }
        .task(id: filter) {
            // Typing waits for a pause before asking the server.
            if !q.isEmpty {
                try? await Task.sleep(for: .milliseconds(300))
                if Task.isCancelled { return }
            }
            await load(reset: true)
        }
    }

    private func load(reset: Bool) async {
        guard reset || !loading else { return }
        loading = true
        defer { loading = false }
        do {
            let page = try await client.adultScenes(filter, offset: reset ? 0 : scenes.count)
            if reset {
                scenes = page.items
            } else {
                let known = Set(scenes.map(\.id))
                scenes += page.items.filter { !known.contains($0.id) }
            }
            total = page.total
            error = nil
        } catch is CancellationError {
            return
        } catch {
            if Task.isCancelled { return }
            if !app.adult.note(error) { self.error = error }
        }
    }
}

struct AdultSceneCard: View {
    let scene: AdultScene
    let client: MediaClient

    private var subtitle: String {
        [scene.studio?.name, scene.date ?? scene.year.map(String.init)].compactMap { $0 }.joined(separator: " · ")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            AdultImage(url: AdultArt.sceneURL(id: scene.id, hasBackdrop: scene.hasBackdrop, hasPoster: scene.hasPoster, client: client),
                       token: client.token, blurKey: AdultArt.sceneKey(scene.id), title: scene.title)
                .aspectRatio(16 / 9, contentMode: .fit)
                .overlay(alignment: .topTrailing) {
                    if scene.played {
                        Image(systemName: "checkmark").font(.caption.bold()).foregroundStyle(Theme.accentContrast)
                            .frame(width: 22, height: 22).background(Theme.accent, in: Circle()).padding(6)
                    }
                }
                .overlay(alignment: .topLeading) {
                    if let d = scene.durationMs, d > 0 {
                        QualityTag(text: clock(d)).padding(6)
                    }
                }
                .overlay(alignment: .bottom) {
                    if let p = AdultGate.progress(positionMs: scene.positionMs, durationMs: scene.durationMs, played: scene.played) {
                        ProgressStrip(value: p).padding(6)
                    }
                }
                .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous).strokeBorder(Theme.border))
            Text(verbatim: scene.title).font(.footnote.weight(.medium)).foregroundStyle(Theme.text).lineLimit(2)
                .multilineTextAlignment(.leading)
            Text(verbatim: subtitle.isEmpty ? " " : subtitle).font(.caption2).foregroundStyle(Theme.textMuted).lineLimit(1)
        }
    }
}

// MARK: Studios and performers

struct AdultStudioGrid: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var q = ""
    @State private var studios: [AdultStudio] = []
    @State private var total = -1
    @State private var loading = false
    @State private var error: Error?

    private let columns = [GridItem(.adaptive(minimum: 150), spacing: 14)]

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            AdultSearchField(text: $q)
            ErrorLine(error: error)
            LazyVGrid(columns: columns, alignment: .leading, spacing: 18) {
                ForEach(studios) { studio in
                    AdultCardLink(route: .adultStudio(AdultRef(id: studio.id, name: studio.name))) {
                        AdultStudioTile(studio: studio, client: client)
                    }
                    .task(id: studio.id) {
                        if studio.id == studios.last?.id && total > studios.count { await load(reset: false) }
                    }
                }
            }
            if loading && studios.isEmpty { LoadingView() }
            if total == 0 { Text("nothing_here").foregroundStyle(Theme.textMuted).padding(.top, 24) }
        }
        .task(id: q) {
            if !q.isEmpty {
                try? await Task.sleep(for: .milliseconds(300))
                if Task.isCancelled { return }
            }
            await load(reset: true)
        }
    }

    private func load(reset: Bool) async {
        guard reset || !loading else { return }
        loading = true
        defer { loading = false }
        do {
            let page = try await client.adultStudios(q: q, offset: reset ? 0 : studios.count)
            if reset {
                studios = page.items
            } else {
                let known = Set(studios.map(\.id))
                studios += page.items.filter { !known.contains($0.id) }
            }
            total = page.total
            error = nil
        } catch is CancellationError {
            return
        } catch {
            if Task.isCancelled { return }
            if !app.adult.note(error) { self.error = error }
        }
    }
}

/// A studio's logo (never blurred) with its name and scene count.
struct AdultStudioTile: View {
    let studio: AdultStudio
    let client: MediaClient

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ZStack {
                Theme.surface2
                if studio.hasImage {
                    AdultImage(url: client.adultImageURL(owner: "studio", id: studio.id, type: "image"), token: client.token, fit: true)
                        .padding(14)
                } else {
                    Text(verbatim: studio.name).displayFont(18).foregroundStyle(Theme.text).lineLimit(3)
                        .minimumScaleFactor(0.6).padding(10)
                }
            }
            .aspectRatio(16 / 9, contentMode: .fit)
            .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous).strokeBorder(Theme.border))
            Text(verbatim: studio.name).font(.footnote.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
            Text("adult.scenesCount \(studio.sceneCount)").font(.caption).foregroundStyle(Theme.textFaint)
        }
    }
}

struct AdultPerformerGrid: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var q = ""
    @State private var performers: [AdultPerformer] = []
    @State private var total = -1
    @State private var loading = false
    @State private var error: Error?

    private let columns = [GridItem(.adaptive(minimum: 108), spacing: 14)]

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            AdultSearchField(text: $q)
            ErrorLine(error: error)
            LazyVGrid(columns: columns, alignment: .leading, spacing: 18) {
                ForEach(performers) { performer in
                    AdultCardLink(route: .adultPerformer(AdultRef(id: performer.id, name: performer.name)),
                                  revealKey: performer.hasImage ? AdultArt.performerKey(performer.id) : nil) {
                        AdultPerformerTile(performer: performer, client: client)
                    }
                    .task(id: performer.id) {
                        if performer.id == performers.last?.id && total > performers.count { await load(reset: false) }
                    }
                }
            }
            if loading && performers.isEmpty { LoadingView() }
            if total == 0 { Text("nothing_here").foregroundStyle(Theme.textMuted).padding(.top, 24) }
        }
        .task(id: q) {
            if !q.isEmpty {
                try? await Task.sleep(for: .milliseconds(300))
                if Task.isCancelled { return }
            }
            await load(reset: true)
        }
    }

    private func load(reset: Bool) async {
        guard reset || !loading else { return }
        loading = true
        defer { loading = false }
        do {
            let page = try await client.adultPerformers(q: q, offset: reset ? 0 : performers.count)
            if reset {
                performers = page.items
            } else {
                let known = Set(performers.map(\.id))
                performers += page.items.filter { !known.contains($0.id) }
            }
            total = page.total
            error = nil
        } catch is CancellationError {
            return
        } catch {
            if Task.isCancelled { return }
            if !app.adult.note(error) { self.error = error }
        }
    }
}

struct AdultPerformerTile: View {
    let performer: AdultPerformer
    let client: MediaClient

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            AdultImage(url: performer.hasImage ? client.adultImageURL(owner: "performer", id: performer.id, type: "image") : nil,
                       token: client.token, blurKey: AdultArt.performerKey(performer.id), title: performer.name)
                .aspectRatio(2 / 3, contentMode: .fit)
                .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous).strokeBorder(Theme.border))
            Text(verbatim: performer.name).font(.footnote.weight(.medium)).foregroundStyle(Theme.text).lineLimit(1)
            Text("adult.scenesCount \(performer.sceneCount)").font(.caption2).foregroundStyle(Theme.textMuted)
        }
    }
}

/// One studio and its scenes.
struct AdultStudioView: View {
    let studio: AdultRef
    let client: MediaClient
    @State private var detail: AdultStudio?

    var body: some View {
        AdultLockGate(client: client) {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    if detail?.hasImage == true {
                        AdultImage(url: client.adultImageURL(owner: "studio", id: studio.id, type: "image"), token: client.token, fit: true)
                            .frame(maxWidth: 220, maxHeight: 70, alignment: .leading)
                    }
                    Text(verbatim: detail?.name ?? studio.name).displayFont(30).foregroundStyle(Theme.text)
                    if let parent = detail?.parent, !parent.isEmpty {
                        Text(verbatim: parent).font(.subheadline).foregroundStyle(Theme.textMuted)
                    }
                    AdultSceneGrid(client: client, base: AdultSceneFilter(studioId: studio.id))
                }
                .padding(.horizontal, 20).padding(.bottom, 20)
            }
            .scrollDismissesKeyboard(.immediately)
            .task(id: studio.id) { detail = try? await client.adultStudio(studio.id) }
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// One performer and their scenes.
struct AdultPerformerView: View {
    let performer: AdultRef
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var detail: AdultPerformer?

    var body: some View {
        AdultLockGate(client: client) {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    header
                    if let text = detail?.description, !text.isEmpty {
                        Text(verbatim: text).font(.callout).foregroundStyle(Theme.text.opacity(0.85)).lineSpacing(3)
                    }
                    AdultSceneGrid(client: client, base: AdultSceneFilter(performerId: performer.id))
                }
                .padding(.horizontal, 20).padding(.bottom, 20)
            }
            .scrollDismissesKeyboard(.immediately)
            .task(id: performer.id) { detail = try? await client.adultPerformer(performer.id) }
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
    }

    private var facts: String {
        guard let d = detail else { return "" }
        return [d.country, d.birthdate].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private var header: some View {
        let key = AdultArt.performerKey(performer.id)
        return HStack(alignment: .bottom, spacing: 16) {
            if detail?.hasImage == true {
                AdultImage(url: client.adultImageURL(owner: "performer", id: performer.id, type: "image"), token: client.token,
                           blurKey: key, title: performer.name)
                    .frame(width: 112, height: 168)
                    .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous).strokeBorder(Theme.border))
                    .onTapGesture { app.adult.reveal(key) }
            }
            VStack(alignment: .leading, spacing: 6) {
                Text(verbatim: detail?.name ?? performer.name).displayFont(28).foregroundStyle(Theme.text)
                    .lineLimit(3).minimumScaleFactor(0.6)
                if let aliases = detail?.aliases, !aliases.isEmpty {
                    Text("adult.aliases \(aliases.joined(separator: ", "))").font(.caption).foregroundStyle(Theme.textMuted).lineLimit(2)
                }
                if !facts.isEmpty { Text(verbatim: facts).font(.caption).foregroundStyle(Theme.textMuted) }
                if let n = detail?.sceneCount { Text("adult.scenesCount \(n)").font(.caption).foregroundStyle(Theme.textFaint) }
            }
        }
    }
}

// MARK: Scene

/// One scene: picture, Play / Resume, studio, performers, tags and files.
struct AdultSceneView: View {
    let sceneID: String
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var detail: AdultSceneDetail?
    @State private var error: Error?
    @State private var fileIndex = 0

    var body: some View {
        AdultLockGate(client: client) {
            Group {
                if let d = detail {
                    page(d)
                } else if let error {
                    ErrorLine(error: error).padding(20).frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    LoadingView().frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .task(id: "\(sceneID)-\(app.reloadTick)") { await load() }
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
    }

    private func load() async {
        do {
            detail = try await client.adultScene(sceneID)
            error = nil
        } catch is CancellationError {
            return
        } catch {
            if !app.adult.note(error) && detail == nil { self.error = error }
        }
    }

    private func page(_ d: AdultSceneDetail) -> some View {
        let key = AdultArt.sceneKey(d.id)
        return ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                AdultImage(url: AdultArt.sceneURL(id: d.id, hasBackdrop: d.hasBackdrop, hasPoster: d.hasPoster, client: client),
                           token: client.token, blurKey: key, title: d.title)
                    .aspectRatio(16 / 9, contentMode: .fit)
                    .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                    .onTapGesture { app.adult.reveal(key) }
                VStack(alignment: .leading, spacing: 8) {
                    Text(verbatim: d.title).displayFont(26).foregroundStyle(Theme.text).lineLimit(4).minimumScaleFactor(0.6)
                    if !meta(d).isEmpty {
                        Text(verbatim: meta(d)).font(.subheadline.monospacedDigit()).foregroundStyle(Theme.text.opacity(0.75))
                    }
                    if let studio = d.studio {
                        NavigationLink(value: Route.adultStudio(studio)) {
                            HStack(spacing: 4) {
                                Text(verbatim: studio.name).headFont(15).textCase(.uppercase)
                                Image(systemName: "chevron.right").font(.caption2)
                            }
                            .foregroundStyle(Theme.textMuted)
                        }
                    }
                }
                actions(d)
                if let text = d.description, !text.isEmpty {
                    Text(verbatim: text).font(.callout).foregroundStyle(Theme.text.opacity(0.85)).lineSpacing(3)
                }
                if !d.performers.isEmpty {
                    SectionHead(title: "adult.performers")
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            ForEach(d.performers) { p in
                                NavigationLink(value: Route.adultPerformer(p)) {
                                    Text(verbatim: p.name).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text)
                                        .padding(.horizontal, 12).frame(height: 36)
                                        .background(Theme.surface2, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                                }
                                .buttonStyle(CardPressStyle())
                            }
                        }
                    }
                }
                if !d.tags.isEmpty {
                    SectionHead(title: "adult.tags")
                    Text(verbatim: d.tags.joined(separator: " · ")).font(.footnote).foregroundStyle(Theme.textMuted)
                }
                if !d.files.isEmpty {
                    SectionHead(title: "adult.files")
                    ForEach(d.files) { f in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: f.name).font(.footnote.weight(.medium)).foregroundStyle(Theme.text).lineLimit(2)
                            Text(verbatim: fileLine(f)).font(.caption.monospacedDigit()).foregroundStyle(Theme.textFaint)
                        }
                    }
                }
            }
            .padding(.horizontal, 20).padding(.top, 8).padding(.bottom, 32)
        }
    }

    private func meta(_ d: AdultSceneDetail) -> String {
        var parts: [String] = []
        if let date = d.date { parts.append(date) } else if let y = d.year { parts.append(String(y)) }
        if let ms = d.durationMs, ms > 0 { parts.append(clock(ms)) }
        if let h = d.height, h > 0 { parts.append("\(h)p") }
        return parts.joined(separator: " · ")
    }

    private func fileLine(_ f: AdultFile) -> String {
        var parts = [f.container.uppercased()]
        if let codec = f.videoCodec { parts.append(codec.uppercased()) }
        if let w = f.width, let h = f.height { parts.append("\(w)×\(h)") }
        parts.append(ByteCountFormatter.string(fromByteCount: f.size, countStyle: .file))
        return parts.joined(separator: " · ")
    }

    @ViewBuilder private func actions(_ d: AdultSceneDetail) -> some View {
        if !d.files.isEmpty {
            let fileID = d.files.count > 1 && d.files.indices.contains(fileIndex) ? d.files[fileIndex].id : nil
            VStack(alignment: .leading, spacing: 12) {
                if d.files.count > 1 {
                    Picker(selection: $fileIndex) {
                        ForEach(Array(d.files.enumerated()), id: \.offset) { idx, f in
                            Text(verbatim: f.height.map { "\($0)p · \(f.name)" } ?? f.name).tag(idx)
                        }
                    } label: { Text("adult.version") }
                    .tint(Theme.text)
                }
                Button {
                    app.play(PlayTarget(id: d.id, file: fileID, adult: true, title: d.title))
                } label: {
                    Group {
                        if !d.played && d.positionMs > 0 {
                            Label { Text("resume_from \(clock(d.positionMs))") } icon: { Image(systemName: "play.fill") }
                        } else {
                            Label("player.play", systemImage: "play.fill")
                        }
                    }
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(PrimaryButtonStyle())
                if !d.played && d.positionMs > 0 {
                    Button { app.play(PlayTarget(id: d.id, start: 0, file: fileID, adult: true, title: d.title)) } label: {
                        Label("from_start", systemImage: "arrow.counterclockwise").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(MutedButtonStyle())
                }
            }
        }
    }
}

// MARK: Settings

/// Blurring and the PIN of this account's adult section.
struct AdultSettingsSheet: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var currentPin = ""
    @State private var newPin = ""
    @State private var confirmPin = ""
    @State private var busy = false
    @State private var error: Error?
    @State private var message: LocalizedStringKey?

    private var pinSet: Bool { app.adult.status?.pinSet == true }

    private var canSavePin: Bool {
        !busy && AdultGate.validPin(newPin) && newPin == confirmPin && (!pinSet || !currentPin.isEmpty)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Toggle(isOn: Binding(get: { app.adult.blur },
                                         set: { value in Task { await save(AdultPreferencesInput(blur: value)) } })) {
                        Text("adult.blur")
                    }
                    .disabled(busy)
                } footer: {
                    Text("adult.blurHint")
                }
                Section {
                    if pinSet {
                        SecureField("adult.currentPin", text: $currentPin).keyboardType(.numberPad)
                    }
                    SecureField("adult.newPin", text: $newPin).keyboardType(.numberPad)
                    SecureField("adult.confirmPin", text: $confirmPin).keyboardType(.numberPad)
                    Button {
                        Task { await save(AdultPreferencesInput(pin: newPin, currentPin: pinSet ? currentPin : nil)) }
                    } label: {
                        Text(pinSet ? LocalizedStringKey("adult.changePin") : LocalizedStringKey("adult.setPin"))
                    }
                    .disabled(!canSavePin)
                    if pinSet {
                        Button(role: .destructive) {
                            Task { await save(AdultPreferencesInput(pin: "", currentPin: currentPin)) }
                        } label: {
                            Text("adult.removePin")
                        }
                        .disabled(busy || currentPin.isEmpty)
                    }
                } header: {
                    Text("adult.pin")
                } footer: {
                    Text("adult.pinHint")
                }
                if let error {
                    ErrorLine(error: error)
                }
                if let message {
                    Text(message).foregroundStyle(Theme.accent)
                }
            }
            .navigationTitle(Text("adult.settings"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("close") { dismiss() } }
            }
        }
    }

    private func save(_ input: AdultPreferencesInput) async {
        busy = true
        defer { busy = false }
        do {
            let status = try await client.updateAdultPreferences(input)
            app.adult.apply(status)
            error = nil
            if let pin = input.pin {
                message = pin.isEmpty ? "adult.pinRemoved" : "adult.pinSaved"
                currentPin = ""
                newPin = ""
                confirmPin = ""
            } else {
                message = nil
            }
        } catch {
            message = nil
            self.error = error
        }
    }
}
