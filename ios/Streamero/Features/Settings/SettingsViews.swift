import PhotosUI
import SwiftUI

extension SettingsPage {
    var label: LocalizedStringKey {
        switch self {
        case .account: "my_account"
        case .playback: "playback"
        case .subtitles: "subtitles"
        case .home: "home_screen"
        case .dashboard: "dashboard"
        case .libraries: "libraries"
        case .users: "users"
        case .metadata: "metadata"
        case .transcoding: "transcoding"
        case .requests: "requests"
        case .enhancements: "enhancements"
        case .tasks: "tasks"
        case .activity: "activity"
        case .logs: "logs"
        case .devices: "devices"
        case .invites: "invites"
        case .apikeys: "api_keys"
        case .server: "server"
        case .branding: "branding"
        }
    }

    var icon: String {
        switch self {
        case .account: "person"
        case .playback: "film"
        case .subtitles: "captions.bubble"
        case .home: "house"
        case .dashboard: "gauge.with.dots.needle.33percent"
        case .libraries: "folder"
        case .users: "person.2"
        case .metadata: "sparkles"
        case .transcoding: "cpu"
        case .requests: "safari"
        case .enhancements: "puzzlepiece"
        case .tasks: "checklist"
        case .activity: "clock.arrow.circlepath"
        case .logs: "doc.text"
        case .devices: "ipad.and.iphone"
        case .invites: "envelope"
        case .apikeys: "key"
        case .server: "server.rack"
        case .branding: "paintpalette"
        }
    }
}

/// The Me tab: who is signed in, personal settings and, for admins, administration.
struct MeTab: View {
    let client: MediaClient
    let signOut: () -> Void
    @Environment(AppState.self) private var app
    @State private var showQuickConnect = false
    @State private var showTogether = false
    @State private var togetherPlay: PlayTarget?

    var body: some View {
        List {
            if let me = app.me { header(me) }
            Section {
                NavigationLink(value: Route.downloads) {
                    Label { Text("downloads.title") } icon: { Image(systemName: "arrow.down.circle") }
                }
            }
            if !app.isKid {
                Section {
                    ForEach(SettingsPage.userPages, id: \.self) { page in link(page) }
                }
            }
            if app.isAdmin {
                Section {
                    ForEach(SettingsPage.adminPages, id: \.self) { page in link(page) }
                } header: {
                    Text("administration")
                }
            }
            if app.canWatchTogether {
                Section {
                    Button { showTogether = true } label: {
                        Label { Text("together.title").foregroundStyle(Theme.text) } icon: { Image(systemName: "person.2.fill") }
                    }
                }
            }
            Section {
                // A kids profile cannot sign other devices in to the account.
                if !app.isKid {
                    Button { showQuickConnect = true } label: { Text("quickConnect.approveTitle").foregroundStyle(Theme.text) }
                }
                Button { app.showProfileGate = true } label: { Text("switch_profile").foregroundStyle(Theme.text) }
                Button(role: .destructive, action: signOut) { Text("sign_out") }
            }
        }
        .scrollContentBackground(.hidden)
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .sheet(isPresented: $showQuickConnect) { QuickConnectApproveView(client: client) }
        .sheet(isPresented: $showTogether, onDismiss: {
            if let target = togetherPlay {
                togetherPlay = nil
                app.play(target)
            }
        }) {
            TogetherView { itemId, start in togetherPlay = PlayTarget(id: itemId, start: start) }
                .environment(app)
                .environment(\.locale, app.locale ?? Locale.current)
        }
    }

    private func link(_ page: SettingsPage) -> some View {
        NavigationLink(value: Route.settings(page)) {
            Label { Text(page.label) } icon: { Image(systemName: page.icon) }
        }
    }

    @ViewBuilder private func header(_ me: User) -> some View {
        let p = me.currentProfile
        let name = p?.name ?? me.displayName ?? me.username
        let picture: URL? = {
            if let p, p.hasAvatar == true { return client.avatarURL(owner: p.id) }
            if p == nil && me.hasAvatar == true { return client.avatarURL(owner: me.id) }
            return nil
        }()
        HStack(spacing: 14) {
            AvatarView(name: name, color: p?.color, url: picture, token: client.token, size: 64)
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: name).font(Theme.display(24)).foregroundStyle(Theme.text).lineLimit(2)
                HStack(spacing: 4) {
                    Text(verbatim: "@\(me.username)")
                    if app.isAdmin { Text(verbatim: "·"); Text("administrator") }
                }
                .font(.footnote).foregroundStyle(Theme.textMuted)
            }
        }
        .listRowBackground(Color.clear)
    }
}

/// A settings page; admin pages refuse to render without administrator rights.
struct SettingsPageView: View {
    let page: SettingsPage
    let client: MediaClient
    @Environment(AppState.self) private var app

    var body: some View {
        Group {
            if page.isAdminOnly && !app.isAdmin {
                ForbiddenView()
            } else {
                content
            }
        }
        .scrollContentBackground(.hidden)
        .background(Theme.background)
        .navigationTitle(Text(page.label))
        .navigationBarTitleDisplayMode(.inline)
    }

    @ViewBuilder private var content: some View {
        switch page {
        case .account: AccountPage(client: client)
        case .playback: PlaybackPage(client: client)
        case .subtitles: SubtitleLookPage(client: client)
        case .home: HomeSectionsPage(client: client)
        case .dashboard: DashboardPage(client: client)
        case .libraries: LibrariesAdminPage(client: client)
        case .users: UsersPage(client: client)
        case .metadata: MetadataPage(client: client)
        case .transcoding: TranscodingPage(client: client)
        case .requests: RequestsPage(client: client)
        case .enhancements: EnhancementsPage(client: client)
        case .tasks: TasksPage(client: client)
        case .activity: ActivityPageView(client: client)
        case .logs: LogsPage(client: client)
        case .devices: DevicesPage(client: client)
        case .invites: InvitesPage(client: client)
        case .apikeys: ApiKeysPage(client: client)
        case .server: ServerPage(client: client)
        case .branding: BrandingPage(client: client)
        }
    }
}

struct ForbiddenView: View {
    var body: some View {
        Text("err_forbidden").foregroundStyle(.red).padding(20)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
    }
}

/// Load-edit-save for one settings document, with a status row.
struct SettingsDoc<T: Equatable, Content: View>: View {
    let load: @MainActor () async throws -> T
    let save: @MainActor (T) async throws -> T
    @ViewBuilder let content: (Binding<T>) -> Content
    @State private var value: T?
    @State private var lastSaved: T?
    @State private var saved = false
    @State private var error: Error?

    var body: some View {
        Group {
            if let binding = Binding($value) {
                content(binding)
                Section {
                    Button { Task { await commit() } } label: { Text("save").frame(maxWidth: .infinity) }
                    if saved { Text("saved").foregroundStyle(.green) }
                    ErrorLine(error: error)
                }
            } else if let error {
                Section { ErrorLine(error: error) }
            } else {
                Section { LoadingView() }
            }
        }
        .task { await fetch() }
        .onChange(of: value) { _, current in if current != lastSaved { saved = false } }
    }

    private func fetch() async {
        guard value == nil else { return }
        do {
            value = try await load()
            error = nil
        } catch { self.error = error }
    }

    private func commit() async {
        guard let v = value else { return }
        do {
            let result = try await save(v)
            lastSaved = result
            value = result
            saved = true
            error = nil
        } catch { self.error = error }
    }
}

// MARK: Personal pages

private struct AccountPage: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var photo: PhotosPickerItem?
    @State private var avatarVersion = 0
    @State private var error: Error?
    @State private var current = ""
    @State private var new = ""
    @State private var passwordMessage = false

    private var avatarURL: URL? {
        guard app.me?.hasAvatar == true, let id = app.me?.id, let base = client.avatarURL(owner: id) else { return nil }
        return avatarVersion == 0 ? base : URL(string: base.absoluteString + "?v=\(avatarVersion)")
    }

    var body: some View {
        Form {
            Section {
                HStack(spacing: 14) {
                    AvatarView(name: app.me?.displayName ?? app.me?.username ?? "", color: nil, url: avatarURL, token: client.token, size: 88)
                    VStack(alignment: .leading, spacing: 8) {
                        PhotosPicker(selection: $photo, matching: .images) { Text("change_picture") }
                        if app.me?.hasAvatar == true {
                            Button("remove_picture", role: .destructive) { Task { await removeAvatar() } }.buttonStyle(.borderless)
                        }
                    }
                }
                ErrorLine(error: error)
            }
            .onChange(of: photo) { _, item in
                guard let item else { return }
                Task { await upload(item) }
            }
            SettingsDoc(load: { UpdateMe(displayName: app.me?.displayName ?? "", email: app.me?.email ?? "", language: app.me?.language ?? "en") },
                        save: { u in
                            let result = try await client.updateMe(u)
                            app.me = result
                            Loc.apply(language: result.language)
                            return u
                        }) { v in
                Section {
                    TextField("display_name", text: keptText(v.displayName))
                    TextField("email", text: keptText(v.email)).keyboardType(.emailAddress).textInputAutocapitalization(.never)
                    OptPicker(title: "language", selection: withDefault(v.language, "en"),
                              options: [Opt("en", verbatim: "English"), Opt("hr", verbatim: "Hrvatski")])
                }
            }
            Section {
                SecureField("current_password", text: $current)
                SecureField("new_password", text: $new)
                Text("password_hint").font(.caption).foregroundStyle(Theme.textFaint)
                Button { Task { await changePassword() } } label: { Text("change_password") }
                    .disabled(current.isEmpty || new.count < 8)
                if passwordMessage { Text("password_changed").foregroundStyle(.green) }
            } header: {
                Text("change_password")
            }
        }
    }

    private func upload(_ item: PhotosPickerItem) async {
        do {
            guard let data = try await item.loadTransferable(type: Data.self), let jpeg = AvatarImage.jpeg(from: data) else { return }
            try await client.uploadAvatar(jpeg)
            await app.refreshMe()
            avatarVersion += 1
            error = nil
        } catch { self.error = error }
    }

    private func removeAvatar() async {
        do {
            try await client.removeAvatar()
            await app.refreshMe()
        } catch { self.error = error }
    }

    private func changePassword() async {
        passwordMessage = false
        do {
            try await client.changePassword(current: current, new: new)
            passwordMessage = true
            current = ""
            new = ""
        } catch { self.error = error }
    }
}

/// Avatars go up as JPEG, sized down so even large photos stay under the server's limit.
enum AvatarImage {
    @MainActor static func jpeg(from data: Data) -> Data? {
        guard let image = UIImage(data: data) else { return nil }
        let maxSide: CGFloat = 1024
        let scale = min(1, maxSide / max(image.size.width, image.size.height))
        guard scale < 1 else { return image.jpegData(compressionQuality: 0.9) }
        let size = CGSize(width: image.size.width * scale, height: image.size.height * scale)
        let renderer = UIGraphicsImageRenderer(size: size)
        return renderer.image { _ in image.draw(in: CGRect(origin: .zero, size: size)) }.jpegData(compressionQuality: 0.9)
    }
}

private struct PlaybackPage: View {
    let client: MediaClient

    private var languages: [Opt<String>] {
        [Opt("", "any")] + ["en", "hr", "de", "fr", "es", "it", "sr", "bs", "sl", "pl", "ru", "ja"].map { Opt($0, verbatim: $0.languageName) }
    }

    private func seconds(_ values: [Int]) -> [Opt<Int>] { values.map { Opt($0, verbatim: "\($0) s") } }

    var body: some View {
        Form {
            SettingsDoc(load: { try await client.preferences() }, save: { try await client.savePreferences($0) }) { p in
                Section {
                    OptPicker(title: "audio_language", selection: optionalText(p.audioLanguage), options: languages)
                    OptPicker(title: "subtitle_language", selection: optionalText(p.subtitleLanguage), options: languages)
                    OptPicker(title: "subtitle_mode", selection: p.subtitleMode, options: [
                        Opt("default", "mode_default"), Opt("smart", "mode_smart"), Opt("always", "mode_always"),
                        Opt("forced", "mode_forced"), Opt("none", "mode_none"),
                    ])
                } header: { Text("language") }
                Section {
                    OptPicker(title: "skip_back", selection: withDefault(p.skipBackSeconds, 10), options: seconds([5, 10, 15, 30, 60]))
                    OptPicker(title: "skip_forward", selection: withDefault(p.skipForwardSeconds, 10), options: seconds([5, 10, 15, 30, 60, 120]))
                    OptPicker(title: "intros", selection: withDefault(p.skipIntro, "button"), options: [
                        Opt("button", "seg_button"), Opt("auto", "seg_auto"), Opt("off", "seg_off"),
                    ])
                    OptPicker(title: "credits", selection: withDefault(p.skipCredits, "button"), options: [
                        Opt("button", "credits_button"), Opt("auto", "credits_auto"), Opt("off", "seg_off"),
                    ])
                    Toggle("autoplay", isOn: p.autoplayNext)
                } header: { Text("controls") }
                Section {
                    OptPicker(title: "max_resolution", selection: zeroAsNil(p.maxHeight), options: [
                        Opt(0, "original"), Opt(2160, verbatim: "4K"), Opt(1080, verbatim: "1080p"), Opt(720, verbatim: "720p"), Opt(480, verbatim: "480p"),
                    ])
                    OptPicker(title: "max_bitrate", selection: zeroAsNil(p.maxBitrateKbps), options: [
                        Opt(0, "no_limit"), Opt(40000, verbatim: "40 Mbit/s"), Opt(20000, verbatim: "20 Mbit/s"), Opt(10000, verbatim: "10 Mbit/s"),
                        Opt(4000, verbatim: "4 Mbit/s"), Opt(2000, verbatim: "2 Mbit/s"), Opt(1000, verbatim: "1 Mbit/s"),
                    ])
                } header: { Text("quality") }
            }
        }
    }
}

private struct SubtitleLookPage: View {
    let client: MediaClient

    var body: some View {
        Form {
            SettingsDoc(load: { try await client.preferences() }, save: { try await client.savePreferences($0) }) { p in
                Section {
                    OptPicker(title: "size", selection: withDefault(p.subtitleSize, "normal"), options: [
                        Opt("small", "small"), Opt("normal", "normal"), Opt("large", "large"), Opt("xlarge", "xlarge"),
                    ])
                    OptPicker(title: "colour", selection: withDefault(p.subtitleColor, "white"), options: [
                        Opt("white", "white"), Opt("yellow", "yellow"), Opt("cyan", "cyan"),
                    ])
                    OptPicker(title: "background", selection: withDefault(p.subtitleBackground, "shadow"), options: [
                        Opt("none", "none"), Opt("shadow", "shadow"), Opt("box", "box"),
                    ])
                }
            }
        }
    }
}

private struct HomeSectionsPage: View {
    let client: MediaClient
    private let all = ["continue", "nextUp", "latest", "favorites", "collections", "libraries"]

    private func title(_ section: String) -> LocalizedStringKey {
        switch section {
        case "continue": "continue_watching"
        case "nextUp": "next_up"
        case "latest": "recently_added"
        case "favorites": "my_list"
        case "collections": "collections"
        default: "libraries"
        }
    }

    var body: some View {
        Form {
            SettingsDoc(load: { try await client.preferences() }, save: { try await client.savePreferences($0) }) { p in
                let chosen = p.wrappedValue.homeSections ?? all
                Section {
                    ForEach(chosen + all.filter { !chosen.contains($0) }, id: \.self) { s in
                        Toggle(title(s), isOn: Binding(get: { chosen.contains(s) }, set: { on in
                            p.wrappedValue.homeSections = on ? chosen + [s] : chosen.filter { $0 != s }
                        }))
                    }
                }
            }
        }
    }
}
