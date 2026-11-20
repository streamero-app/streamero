import SwiftUI

/// Renders admin content only for administrators on a non-kid profile, even if a route got here.
struct AdminGuarded<Content: View>: View {
    @ViewBuilder let content: () -> Content
    @Environment(AppState.self) private var app

    var body: some View {
        if app.isAdmin {
            content()
        } else {
            ForbiddenView()
        }
    }
}

/// A destructive action that asks once before running.
struct ConfirmButton: View {
    let title: LocalizedStringKey
    let action: () -> Void
    @State private var asking = false

    var body: some View {
        Button(title, role: .destructive) { asking = true }
            .buttonStyle(.borderless)
            .confirmationDialog(title, isPresented: $asking) { Button(title, role: .destructive, action: action) }
    }
}

struct DashboardPage: View {
    let client: MediaClient
    @State private var dash: Dashboard?
    @State private var error: Error?

    var body: some View {
        AdminGuarded {
            List {
                ErrorLine(error: error)
                if let d = dash {
                    Section {
                        if d.nowPlaying.isEmpty { Text("nobody_watching").foregroundStyle(Theme.textMuted) }
                        ForEach(d.nowPlaying, id: \.key) { s in
                            NowPlayingRow(session: s, client: client, onChanged: { Task { await load() } })
                        }
                        Text("transcodes_now \(d.activeTranscodes)").font(.footnote).foregroundStyle(Theme.textFaint)
                    } header: { Text("now_playing") }
                    Section {
                        ForEach(d.storage, id: \.path) { s in
                            VStack(alignment: .leading, spacing: 4) {
                                Text(verbatim: "\(s.kind) · \(s.path)").font(.subheadline)
                                ProgressView(value: 1 - Double(s.freeBytes) / Double(max(1, s.totalBytes))).tint(Theme.accent)
                                Text("free_of \(s.freeBytes / 1_073_741_824) \(s.totalBytes / 1_073_741_824)").font(.caption).foregroundStyle(Theme.textFaint)
                            }
                        }
                    } header: { Text("storage") }
                    Section {
                        Text(verbatim: [d.server.name, d.server.version, d.server.os ?? ""].filter { !$0.isEmpty }.joined(separator: " · "))
                            .foregroundStyle(Theme.textMuted)
                    } header: { Text("server") }
                } else if error == nil {
                    LoadingView()
                }
            }
            .poll(every: { .seconds(10) }) { await load() }
        }
    }

    private func load() async {
        do {
            dash = try await client.dashboard()
            error = nil
        } catch is CancellationError { return } catch { self.error = error }
    }
}

private struct NowPlayingRow: View {
    let session: NowPlaying
    let client: MediaClient
    let onChanged: () -> Void
    @State private var message = ""
    @State private var error: Error?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            NavigationLink(value: Route.item(session.itemId)) {
                Text(verbatim: session.title).font(.subheadline.weight(.semibold))
            }
            Text(verbatim: description).font(.footnote).foregroundStyle(Theme.textMuted)
            if let d = session.durationMs, d > 0 {
                ProgressView(value: min(1, Double(session.positionMs) / Double(d))).tint(Theme.accent)
            }
            HStack {
                TextField("message", text: $message).textFieldStyle(.roundedBorder)
                Button("send") {
                    let text = message
                    message = ""
                    Task { await command(SessionCommand(kind: "message", text: text)) }
                }
                .disabled(message.isEmpty)
                .buttonStyle(.bordered)
            }
            ConfirmButton(title: "stop_playback") {
                Task {
                    await command(SessionCommand(kind: "stop", text: Loc.string("stopped_by_admin")))
                    onChanged()
                }
            }
            ErrorLine(error: error)
        }
        .padding(.vertical, 4)
    }

    private func command(_ c: SessionCommand) async {
        do {
            try await client.sendCommand(sessionKey: session.key, c)
            error = nil
        } catch is CancellationError {
            return
        } catch {
            self.error = error
        }
    }

    private var description: String {
        var parts = [session.user]
        if let p = session.profile { parts.append(p) }
        parts.append(session.device)
        parts.append(session.reason.map { "\(session.method) (\($0))" } ?? session.method)
        return parts.joined(separator: " · ")
    }
}

struct LibrariesAdminPage: View {
    let client: MediaClient
    @State private var libs: [Library]?
    @State private var error: Error?
    @State private var name = ""
    @State private var kind = "movies"
    @State private var path = "/media/"

    var body: some View {
        AdminGuarded {
            Form {
                if let error { ErrorLine(error: error) }
                ForEach(libs ?? []) { l in
                    Section {
                        Text(verbatim: "\(l.kind) · \(l.paths.joined(separator: ", ")) · \(l.itemCount)").font(.footnote).foregroundStyle(Theme.textMuted)
                        Text(verbatim: l.scan.state + (l.scan.error.map { " · \($0)" } ?? "")).font(.caption).foregroundStyle(Theme.textFaint)
                        Button("scan") { perform { try await client.scanLibrary(l.id) } }
                        Button("refresh_metadata") { perform { try await client.refreshLibraryMetadata(l.id) } }
                        ConfirmButton(title: "delete") { perform { try await client.deleteLibrary(l.id) } }
                    } header: { Text(verbatim: l.name) }
                }
                Section {
                    TextField("name", text: $name)
                    OptPicker(title: "kind", selection: $kind, options: [Opt("movies", "movies"), Opt("shows", "shows"), Opt("music", "music"), Opt("mixed", "mixed"),
                                       Opt("books", "books"), Opt("audiobooks", "audiobooks"), Opt("photos", "photos")])
                    TextField("folder", text: $path).textInputAutocapitalization(.never).autocorrectionDisabled()
                    Button("add_library") {
                        let input = LibraryInput(name: name, kind: kind, paths: [path])
                        perform {
                            _ = try await client.createLibrary(input)
                            name = ""
                        }
                    }
                    .disabled(name.isEmpty || path.count <= 1)
                } header: { Text("add_library") }
            }
            .task { await load() }
        }
    }

    private func load() async {
        do {
            libs = try await client.libraries()
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func perform(_ action: @MainActor @escaping () async throws -> Void) {
        Task {
            do {
                try await action()
                await load()
            } catch is CancellationError {} catch { self.error = error }
        }
    }
}

struct UsersPage: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var users: [User]?
    @State private var error: Error?
    @State private var name = ""
    @State private var password = ""
    @State private var makeAdmin = false

    var body: some View {
        AdminGuarded {
            Form {
                if let error { ErrorLine(error: error) }
                ForEach(users ?? []) { u in row(u) }
                Section {
                    TextField("username", text: $name).textInputAutocapitalization(.never).autocorrectionDisabled()
                    SecureField("password", text: $password)
                    Text("password_hint").font(.caption).foregroundStyle(Theme.textFaint)
                    Toggle("administrator", isOn: $makeAdmin)
                    Button("add_user") { createUser() }.disabled(name.isEmpty || password.count < 8)
                } header: { Text("add_user") }
            }
            .task { await load() }
        }
    }

    @ViewBuilder private func row(_ u: User) -> some View {
        Section {
            HStack(spacing: 4) {
                Text(verbatim: "@\(u.username)")
                if u.isAdmin { Text(verbatim: "·"); Text("administrator") }
                if u.disabled { Text(verbatim: "·"); Text("disabled") }
            }
            .font(.footnote).foregroundStyle(Theme.textMuted)
            if u.id != app.me?.id {
                Toggle("administrator", isOn: Binding(get: { u.isAdmin }, set: { on in update(u.id, UpdateUser(isAdmin: on)) }))
                Toggle("disabled", isOn: Binding(get: { u.disabled }, set: { on in update(u.id, UpdateUser(disabled: on)) }))
                ConfirmButton(title: "delete") { perform { try await client.deleteUser(u.id) } }
            }
        } header: { Text(verbatim: u.displayName ?? u.username) }
    }

    private func update(_ id: String, _ body: UpdateUser) {
        perform { _ = try await client.updateUser(id, body) }
    }

    private func createUser() {
        let body = CreateUser(username: name, password: password, isAdmin: makeAdmin)
        perform {
            _ = try await client.createUser(body)
            name = ""
            password = ""
            makeAdmin = false
        }
    }

    private func load() async {
        do {
            users = try await client.users()
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func perform(_ action: @MainActor @escaping () async throws -> Void) {
        Task {
            do {
                try await action()
                await load()
            } catch is CancellationError {} catch { self.error = error }
        }
    }
}

struct MetadataPage: View {
    let client: MediaClient
    @State private var key = ""

    var body: some View {
        AdminGuarded {
            Form {
                SettingsDoc(load: { try await client.metadataSettings() },
                            save: { s in
                                let result = try await client.saveMetadataSettings(MetadataSettingsInput(language: s.language, country: s.country, tmdbApiKey: key.isEmpty ? nil : key))
                                key = ""
                                return result
                            }) { s in
                    Section {
                        if s.wrappedValue.tmdbConfigured {
                            Text("tmdb_connected").foregroundStyle(.green)
                        } else {
                            Text("tmdb_steps").font(.subheadline).foregroundStyle(Theme.textMuted)
                        }
                        SecureField("tmdb_key", text: $key)
                        OptPicker(title: "metadata_language", selection: s.language, options: ["en-US", "en-GB", "hr-HR", "de-DE", "fr-FR", "es-ES", "it-IT", "sr-RS", "bs-BA", "sl-SI"].map { Opt($0, verbatim: $0) })
                        OptPicker(title: "country", selection: s.country, options: ["US", "GB", "HR", "DE", "AT", "FR", "ES", "IT", "RS", "BA", "SI"].map { Opt($0, verbatim: $0) })
                    }
                }
            }
        }
    }
}

struct TranscodingPage: View {
    let client: MediaClient

    var body: some View {
        AdminGuarded {
            Form {
                SettingsDoc(load: { try await client.encoding() }, save: { try await client.saveEncoding($0) }) { e in
                    Section {
                        OptPicker(title: "hw_accel", selection: e.hardwareAcceleration, options: [
                            Opt("none", "hw_none"), Opt("vaapi", verbatim: "VAAPI (Intel, AMD)"), Opt("qsv", verbatim: "Intel Quick Sync"), Opt("nvenc", verbatim: "NVIDIA NVENC"),
                        ])
                        if e.wrappedValue.hardwareAcceleration == "vaapi" {
                            TextField(text: withDefault(e.vaapiDevice, "/dev/dri/renderD128")) { Text(verbatim: "VAAPI") }
                                .textInputAutocapitalization(.never).autocorrectionDisabled()
                        }
                        OptPicker(title: "encoder_speed", selection: e.preset, options: ["ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow"].map { Opt($0, verbatim: $0) })
                        VStack(alignment: .leading) {
                            Text("quality_value \(e.wrappedValue.quality)")
                            Slider(value: Binding(get: { Double(e.wrappedValue.quality) }, set: { e.wrappedValue.quality = Int($0) }), in: 14...35, step: 1)
                        }
                        OptPicker(title: "audio_bitrate", selection: e.audioBitrateKbps, options: [96, 128, 160, 192, 256, 320].map { Opt($0, verbatim: "\($0) kbit/s") })
                        OptPicker(title: "segment_length", selection: e.segmentSeconds, options: [2, 3, 4, 6, 8, 10].map { Opt($0, verbatim: "\($0) s") })
                        OptPicker(title: "max_transcodes", selection: e.maxSessions, options: [Opt<Int>(0, "no_limit")] + [1, 2, 3, 4, 6, 8, 12, 16].map { Opt($0, verbatim: "\($0)") })
                        Toggle("tone_mapping", isOn: e.toneMapping)
                    } header: { Text("transcoding") }
                }
                SettingsDoc(load: { try await client.resume() }, save: { try await client.saveResume($0) }) { r in
                    Section {
                        LabeledContent("resume_min") { TextField("", value: r.minResumePercent, format: .number).keyboardType(.numberPad).multilineTextAlignment(.trailing) }
                        LabeledContent("resume_max") { TextField("", value: r.maxResumePercent, format: .number).keyboardType(.numberPad).multilineTextAlignment(.trailing) }
                        LabeledContent("resume_duration") { TextField("", value: r.minResumeDurationSeconds, format: .number).keyboardType(.numberPad).multilineTextAlignment(.trailing) }
                    } header: { Text("resume") }
                }
            }
        }
    }
}

struct RequestsPage: View {
    let client: MediaClient
    @State private var keys: [String: String] = [:]
    @State private var checks: [ConnectionCheck] = []
    @State private var testError: Error?
    @State private var testing = false

    var body: some View {
        AdminGuarded {
            Form {
                SettingsDoc(load: { try await client.requestSettings() }, save: { s in
                    var input = RequestSettingsInput(
                        usersCanRequest: s.usersCanRequest, seerrUrl: s.seerrUrl, seerrApiKey: keys["seerr"],
                        radarrUrl: s.radarrUrl, radarrApiKey: keys["radarr"], sonarrUrl: s.sonarrUrl, sonarrApiKey: keys["sonarr"],
                        bazarrUrl: s.bazarrUrl, bazarrApiKey: keys["bazarr"])
                    #if MANAGE
                    input.lidarrUrl = s.lidarrUrl
                    input.lidarrApiKey = keys["lidarr"]
                    input.prowlarrUrl = s.prowlarrUrl
                    input.prowlarrApiKey = keys["prowlarr"]
                    #endif
                    let result = try await client.saveRequestSettings(input)
                    keys = [:]
                    return result
                }) { s in
                    Section {
                        urlField("address", keptText(s.seerrUrl))
                        keyField("seerr", configured: s.wrappedValue.seerrConfigured)
                        Toggle("everyone_can_request", isOn: s.usersCanRequest)
                    } header: { Text(verbatim: "Seerr") }
                    Section {
                        urlFieldVerbatim("Radarr", keptText(s.radarrUrl))
                        keyField("radarr", configured: s.wrappedValue.radarrConfigured, app: "Radarr")
                        urlFieldVerbatim("Sonarr", keptText(s.sonarrUrl))
                        keyField("sonarr", configured: s.wrappedValue.sonarrConfigured, app: "Sonarr")
                    } header: { Text(verbatim: "Radarr / Sonarr") }
                    Section {
                        urlField("address", keptText(s.bazarrUrl))
                        keyField("bazarr", configured: s.wrappedValue.bazarrConfigured == true)
                    } header: { Text(verbatim: "Bazarr") }
                    #if MANAGE
                    Section {
                        urlFieldVerbatim("Lidarr", keptText(s.lidarrUrl))
                        keyField("lidarr", configured: s.wrappedValue.lidarrConfigured == true, app: "Lidarr")
                        urlFieldVerbatim("Prowlarr", keptText(s.prowlarrUrl))
                        keyField("prowlarr", configured: s.wrappedValue.prowlarrConfigured == true, app: "Prowlarr")
                    } header: { Text(verbatim: "Lidarr / Prowlarr") }
                    #endif
                    Section {
                        Button("test_connections") {
                            testing = true
                            Task {
                                defer { testing = false }
                                do {
                                    checks = try await client.testRequestSettings()
                                    testError = nil
                                } catch is CancellationError {
                                    return
                                } catch {
                                    checks = []
                                    testError = error
                                }
                            }
                        }
                        .disabled(testing)
                        ErrorLine(error: testError)
                        ForEach(checks, id: \.app) { c in
                            // Icon and words, not only the colour.
                            Label {
                                Text(verbatim: "\(c.app): " + (c.ok ? (c.version ?? "") : (c.error ?? "")))
                            } icon: {
                                Image(systemName: c.ok ? "checkmark.circle.fill" : "xmark.octagon.fill")
                            }
                            .foregroundStyle(c.ok ? .green : .red)
                            .accessibilityValue(c.ok ? Text("check.ok") : Text("check.failed"))
                        }
                    }
                }
            }
        }
    }

    private func urlField(_ title: LocalizedStringKey, _ value: Binding<String>) -> some View {
        TextField(title, text: value).textInputAutocapitalization(.never).autocorrectionDisabled().keyboardType(.URL)
    }

    private func urlFieldVerbatim(_ title: String, _ value: Binding<String>) -> some View {
        TextField(text: value) { Text(verbatim: title) }.textInputAutocapitalization(.never).autocorrectionDisabled().keyboardType(.URL)
    }

    private func keyField(_ id: String, configured: Bool, app appName: String? = nil) -> some View {
        SecureField(text: Binding(get: { keys[id] ?? "" }, set: { keys[id] = $0.isEmpty ? nil : $0 })) {
            if let appName { Text(verbatim: appName + " ") + Text("api_key") + Text(verbatim: configured ? " ✓" : "") }
            else { Text("api_key") + Text(verbatim: configured ? " ✓" : "") }
        }
    }
}

struct EnhancementsPage: View {
    let client: MediaClient
    @Environment(AppState.self) private var app

    var body: some View {
        AdminGuarded {
            Form {
                SettingsDoc(load: { try await client.enhancements() }, save: { e in
                    let result = try await client.saveEnhancements(e)
                    app.enhancements = result
                    return result
                }) { e in
                    Section {
                        Text("enhancements_intro").font(.subheadline).foregroundStyle(Theme.textMuted)
                        Toggle("enh_quality", isOn: e.qualityTags)
                        Toggle("enh_random", isOn: e.randomButton)
                        Toggle("enh_pause", isOn: e.pauseScreen)
                        Toggle("enh_continue", isOn: e.removeFromContinue)
                        Toggle("enh_search", isOn: e.discoverInSearch)
                        Toggle("enh_arr", isOn: e.arrLinks)
                        Toggle("enh_shortcuts", isOn: e.shortcutsHelp)
                        Toggle("enh_pip", isOn: e.autoPip)
                    }
                }
            }
        }
    }
}

struct TasksPage: View {
    let client: MediaClient
    @State private var tasks: [TaskInfo]?
    @State private var error: Error?

    static func title(_ key: String) -> LocalizedStringKey {
        switch key {
        case "scan_all": "task_scan"
        case "refresh_metadata": "task_metadata"
        case "cleanup_images": "task_cleanup"
        case "analyze_media": "task_analyze"
        case "detect_segments": "task_segments"
        case "backup": "task_backup"
        case "refresh_guide": "task_guide"
        default: LocalizedStringKey(key)
        }
    }

    private func title(_ key: String) -> LocalizedStringKey { Self.title(key) }

    /// completed / failed / cancelled, translated.
    static func state(_ state: String) -> String {
        switch state {
        case "completed": Loc.string("task_state.completed")
        case "failed": Loc.string("task_state.failed")
        case "cancelled": Loc.string("task_state.cancelled")
        default: state
        }
    }

    var body: some View {
        AdminGuarded {
            List {
                if let error { ErrorLine(error: error) }
                ForEach(tasks ?? [], id: \.key) { t in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(title(t.key)).font(.subheadline.weight(.semibold))
                        let lastRun = t.lastRunAt.map { shortTime($0) }
                        Group {
                            if let every = t.interval {
                                Text(verbatim: [lastRun, t.lastState.map(Self.state)].compactMap { $0 }.joined(separator: " · ") + " ") + Text("every \(every)")
                            } else {
                                Text(verbatim: [lastRun, t.lastState.map(Self.state)].compactMap { $0 }.joined(separator: " · "))
                            }
                        }
                        .font(.footnote).foregroundStyle(Theme.textMuted)
                        Button(t.running ? "running" : "run_now") {
                            Task {
                                do {
                                    try await client.runTask(t.key)
                                } catch is CancellationError {
                                    return
                                } catch {
                                    self.error = error
                                }
                                await load()
                            }
                        }
                        .buttonStyle(.bordered)
                        .disabled(t.running)
                    }
                }
            }
            // While a task runs, its state is refreshed every few seconds; otherwise rarely.
            .poll(every: { .seconds(tasks?.contains { $0.running } == true ? 3 : 30) }) { await load() }
        }
    }

    private func load() async {
        do {
            tasks = try await client.tasks()
            error = nil
        } catch is CancellationError {
            return
        } catch { self.error = error }
    }
}

struct ActivityPageView: View {
    let client: MediaClient
    @State private var offset = 0
    @State private var page: ActivityPage?
    @State private var error: Error?

    var body: some View {
        AdminGuarded {
            List {
                if let error { ErrorLine(error: error) }
                ForEach(Array((page?.entries ?? []).enumerated()), id: \.offset) { _, e in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: e.action + (e.user.map { " · \($0)" } ?? "")).font(.subheadline)
                        Text(verbatim: shortTime(e.at, length: 19)).font(.caption).foregroundStyle(Theme.textFaint)
                    }
                }
                HStack {
                    Button("newer") { offset = max(0, offset - 50) }.disabled(offset == 0).buttonStyle(.bordered)
                    Spacer()
                    Button("older") { offset += 50 }.disabled((page?.total ?? 0) <= offset + 50).buttonStyle(.bordered)
                }
            }
            .task(id: offset) {
                do {
                    page = try await client.activity(offset: offset)
                    error = nil
                } catch is CancellationError { return } catch { self.error = error }
            }
        }
    }
}

struct LogsPage: View {
    let client: MediaClient
    @State private var level = "info"
    @State private var query = ""
    @State private var logs: [LogEntry]?
    @State private var error: Error?

    var body: some View {
        AdminGuarded {
            List {
                OptPicker(title: "level", selection: $level, options: ["debug", "info", "warn", "error"].map { Opt($0, verbatim: $0) })
                TextField("search", text: $query).autocorrectionDisabled().textInputAutocapitalization(.never)
                if let error { ErrorLine(error: error) }
                if logs == nil && error == nil { LoadingView() }
                ForEach(Array((logs ?? []).enumerated()), id: \.offset) { _, l in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: "\(shortTime(l.time, length: 19)) \(l.level.uppercased()) \(l.source)")
                            .font(.caption2).foregroundStyle(l.level == "error" ? .red : (l.level == "warn" ? Theme.accent : Theme.textFaint))
                        Text(verbatim: l.message + (l.attrs.map { "  \($0)" } ?? "")).font(.caption.monospaced())
                    }
                }
            }
            .task(id: level + "|" + query) {
                try? await Task.sleep(for: .milliseconds(300))
                guard !Task.isCancelled else { return }
                do {
                    logs = try await client.logs(level: level, q: query)
                    error = nil
                } catch is CancellationError { return } catch { self.error = error }
            }
        }
    }
}

/// Signed-in devices: the account's own for everyone, every user's for administrators.
struct DevicesPage: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var sessions: [SessionInfo]?
    @State private var error: Error?
    @State private var signingOut: SessionInfo?

    var body: some View {
        List {
            Section {
                Text(app.isAdmin ? LocalizedStringKey("devices.introAdmin") : LocalizedStringKey("devices.intro")).font(.subheadline).foregroundStyle(Theme.textMuted)
                    .listRowBackground(Color.clear)
            }
            if let error { ErrorLine(error: error) }
            if sessions == nil && error == nil { LoadingView().listRowBackground(Color.clear) }
            ForEach(sessions ?? []) { s in
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 8) {
                        Text(verbatim: s.deviceName.isEmpty ? Loc.string("devices.unknown") : s.deviceName)
                            .font(.subheadline.weight(.semibold))
                        if s.current { TagBadge(text: "devices.current", tone: .success) }
                    }
                    Text(verbatim: [app.isAdmin ? s.username : nil, s.client, shortTime(s.lastSeenAt)].compactMap { $0 }.joined(separator: " · "))
                        .font(.footnote).foregroundStyle(Theme.textMuted)
                }
                .swipeActions {
                    if !s.current {
                        Button("devices.signOut", role: .destructive) { signingOut = s }
                    }
                }
                .contextMenu {
                    if !s.current {
                        Button("devices.signOut", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) { signingOut = s }
                    }
                }
            }
        }
        .confirmationDialog(Text(verbatim: signingOut.map { String(format: Loc.string("devices.confirmSignOut %@"),
                                                                     $0.deviceName.isEmpty ? Loc.string("devices.unknown") : $0.deviceName) } ?? ""),
                            isPresented: Binding(get: { signingOut != nil }, set: { if !$0 { signingOut = nil } }),
                            titleVisibility: .visible, presenting: signingOut) { s in
            Button("devices.signOut", role: .destructive) {
                Task {
                    do {
                        try await client.revokeSession(s.id)
                        await load()
                    } catch is CancellationError {} catch { self.error = error }
                }
            }
        }
        .refreshable { await load() }
        .task { await load() }
    }

    private func load() async {
        do {
            sessions = try await client.sessions()
            error = nil
        } catch is CancellationError { return } catch { self.error = error }
    }
}

struct InvitesPage: View {
    let client: MediaClient
    @State private var invites: [Invite]?
    @State private var error: Error?
    @State private var link: String?

    var body: some View {
        AdminGuarded {
            List {
                if let error { ErrorLine(error: error) }
                Section {
                    Button("create_invite") { Task { await createInvite() } }
                    if let link { Text(verbatim: link).textSelection(.enabled).font(.footnote) }
                }
                ForEach(invites ?? []) { i in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(verbatim: i.note.isEmpty ? String(i.id.prefix(8)) : i.note).font(.subheadline.weight(.semibold))
                        Text(verbatim: "\(i.uses)/\(i.maxUses) · \(shortTime(i.expiresAt))").font(.footnote).foregroundStyle(Theme.textMuted)
                        ConfirmButton(title: "delete") {
                            Task {
                                do {
                                    try await client.deleteInvite(i.id)
                                    error = nil
                                } catch is CancellationError {
                                    return
                                } catch {
                                    self.error = error
                                }
                                await load()
                            }
                        }
                    }
                }
            }
            .task { await load() }
        }
    }

    private func createInvite() async {
        do {
            let created = try await client.createInvite(CreateInvite(expiresInHours: 72, maxUses: 1))
            link = client.inviteLink(token: created.token)?.absoluteString
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func load() async {
        do {
            invites = try await client.invites()
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}

struct ApiKeysPage: View {
    let client: MediaClient
    @State private var keys: [ApiKey]?
    @State private var error: Error?
    @State private var name = ""
    @State private var created: String?
    @State private var readOnly = false
    @State private var busy = false

    var body: some View {
        AdminGuarded {
            Form {
                if let error { ErrorLine(error: error) }
                Section {
                    TextField("name", text: $name)
                    Toggle("apikeys.readOnly", isOn: $readOnly)
                    Button("create_api_key") {
                        let n = name
                        let ro = readOnly
                        busy = true
                        Task {
                            defer { busy = false }
                            do {
                                created = try await client.createApiKey(name: n, readOnly: ro).key
                                name = ""
                                readOnly = false
                                error = nil
                                await load()
                            } catch is CancellationError {} catch { self.error = error }
                        }
                    }
                    .disabled(name.trimmingCharacters(in: .whitespaces).isEmpty || busy)
                    if let created {
                        Text(verbatim: created).textSelection(.enabled).font(.caption.monospaced())
                        CopyButton(text: created, sensitive: true)
                        Text("apikeys.shownOnce").font(.caption).foregroundStyle(Theme.textMuted)
                    }
                } header: { Text("create_api_key") } footer: { Text("apikeys.readOnlyHint") }
                ForEach(keys ?? []) { k in
                    Section {
                        Text(verbatim: "\(k.prefix)… · \(k.createdBy) · \(k.lastUsedAt.map { ServerDate.short($0) } ?? "—")").font(.footnote).foregroundStyle(Theme.textMuted)
                        if k.readOnly == true { TagBadge(text: "apikeys.readOnly") }
                        ConfirmButton(title: "delete") {
                            Task {
                                do {
                                    try await client.deleteApiKey(k.id)
                                    error = nil
                                } catch is CancellationError {} catch { self.error = error }
                                await load()
                            }
                        }
                    } header: { Text(verbatim: k.name) }
                }
            }
            .hiddenInAppSwitcher(created != nil)
            .task { await load() }
            .onDisappear { created = nil }
        }
    }

    private func load() async {
        do {
            keys = try await client.apiKeys()
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}

struct ServerPage: View {
    let client: MediaClient

    var body: some View {
        AdminGuarded {
            Form {
                SettingsDoc(load: { try await client.serverSettings() }, save: { try await client.saveServerSettings($0) }) { s in
                    Section { TextField("server_name", text: s.serverName) }
                }
            }
        }
    }
}

struct BrandingPage: View {
    let client: MediaClient

    var body: some View {
        AdminGuarded {
            Form {
                SettingsDoc(load: { try await client.branding() }, save: { try await client.saveBranding($0) }) { b in
                    Section {
                        TextField("login_message", text: b.loginDisclaimer, axis: .vertical).lineLimit(3...6)
                    }
                    Section {
                        TextField("custom_css", text: b.customCss, axis: .vertical).lineLimit(6...12).font(.caption.monospaced())
                    }
                }
            }
        }
    }
}
