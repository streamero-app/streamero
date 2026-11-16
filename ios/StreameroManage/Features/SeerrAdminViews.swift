import SwiftUI

// MARK: User permissions and limits

/// One Seerr user: permissions by documented bit (unknown bits kept) and request limits.
struct SeerrUserEditView: View {
    let client: ArrClient
    let user: JSONValue
    /// The id of the Seerr account the proxy acts as (the API key's owner).
    let meId: Int?

    @State private var mask: Int64?
    @State private var originalMask: Int64 = 0
    @State private var main: JSONValue = .null
    @State private var quota = SeerrQuota()
    @State private var originalQuota = SeerrQuota()
    @State private var error: Error?
    @State private var busy = false
    @State private var confirmAdminRemoval = false
    @State private var notice: ManageNotice?

    private var userId: Int { user["id"].int ?? 0 }
    private var isSelf: Bool { meId != nil && meId == userId }
    private var hasChanges: Bool { (mask ?? originalMask) != originalMask || quota != originalQuota }

    var body: some View {
        Form {
            Section {
                HStack(spacing: 12) {
                    RemotePoster(url: (user["avatar"].string).flatMap { $0.hasPrefix("http") ? URL(string: $0) : nil }, title: displayName)
                        .frame(width: 48, height: 48)
                        .clipShape(Circle())
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: displayName).headFont(17)
                        if let email = user["email"].string?.nilIfBlank {
                            Text(verbatim: email).font(.caption).foregroundStyle(Theme.textMuted)
                        }
                    }
                    Spacer()
                    if isSelf { TagBadge(text: "manage.you", tone: .accent) }
                }
            }
            .listRowBackground(Color.clear)

            if let current = mask {
                let isAdmin = SeerrPermissions.has(current, SeerrPermissions.admin)
                ForEach(SeerrPermissions.groups) { group in
                    Section {
                        ForEach(group.permissions) { permission in
                            Toggle(isOn: toggle(permission)) {
                                Text(LocalizedStringKey(permission.key))
                            }
                            .disabled(isAdmin && permission != SeerrPermissions.admin)
                        }
                    } header: {
                        Text(LocalizedStringKey(group.titleKey))
                    } footer: {
                        if group.permissions.contains(SeerrPermissions.admin) && isAdmin {
                            Text("manage.perm.admin_hint")
                        }
                    }
                }
                let unknown = SeerrPermissions.unknownPositions(originalMask | current)
                if !unknown.isEmpty {
                    Section {
                        ForEach(unknown, id: \.self) { position in
                            Toggle(isOn: rawBit(position)) { Text("manage.perm.other_bit \(position)") }
                        }
                    } header: { Text("manage.perm.group.unknown") } footer: { Text("manage.perm.unknown_hint") }
                }
                if SeerrQuota.isPresent(in: main) {
                    Section {
                        OptionalIntRow(title: "manage.quota.movie_limit", value: $quota.movieLimit)
                        OptionalIntRow(title: "manage.quota.movie_days", value: $quota.movieDays)
                        OptionalIntRow(title: "manage.quota.tv_limit", value: $quota.tvLimit)
                        OptionalIntRow(title: "manage.quota.tv_days", value: $quota.tvDays)
                    } header: { Text("manage.quota") } footer: { Text("manage.quota_hint") }
                }
                Section {
                    ArrErrorLine(error: error)
                    Button { attemptSave() } label: {
                        HStack { Text("save"); Spacer(); if busy { ProgressView() } }
                    }
                    .disabled(busy || !hasChanges)
                }
            } else if let error {
                ArrErrorState(error: error) { await load() }
            } else {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.permissions"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .manageNotice($notice)
        .alert("manage.perm.remove_own_admin", isPresented: $confirmAdminRemoval) {
            Button("common.cancel", role: .cancel) {}
            Button("manage.perm.remove_anyway", role: .destructive) { Task { await save() } }
        } message: {
            Text("manage.perm.remove_own_admin_hint")
        }
    }

    private var displayName: String { user.firstString("displayName", "username", "email") ?? "#\(userId)" }

    private func toggle(_ permission: SeerrPermission) -> Binding<Bool> {
        Binding(get: { SeerrPermissions.has(mask ?? 0, permission) },
                set: { mask = SeerrPermissions.setting(mask ?? 0, permission, $0) })
    }

    private func rawBit(_ position: Int) -> Binding<Bool> {
        let bit = Int64(1) << position
        return Binding(get: { (mask ?? 0) & bit != 0 }, set: { on in mask = on ? (mask ?? 0) | bit : (mask ?? 0) & ~bit })
    }

    private func attemptSave() {
        if isSelf, SeerrPermissions.removesAdmin(from: originalMask, to: mask ?? originalMask) {
            confirmAdminRemoval = true
        } else {
            Task { await save() }
        }
    }

    private func load() async {
        do {
            let perms = try await client.get("user/\(userId)/settings/permissions")
            let value = perms["permissions"].int64 ?? user["permissions"].int64 ?? 0
            originalMask = value
            mask = value
            main = (try? await client.get("user/\(userId)/settings/main")) ?? .null
            quota = SeerrQuota(main)
            originalQuota = quota
            error = nil
        } catch { self.error = error }
    }

    private func save() async {
        guard let mask else { return }
        busy = true
        defer { busy = false }
        do {
            if mask != originalMask {
                try await client.send("POST", "user/\(userId)/settings/permissions", json: ["permissions": .int(mask)])
            }
            if quota != originalQuota {
                try await client.send("POST", "user/\(userId)/settings/main", json: quota.applied(to: main))
            }
            notice = ManageNotice(text: "saved")
            await load()
        } catch { self.error = error }
    }
}

/// A whole number that may be empty ("use the default").
struct OptionalIntRow: View {
    let title: LocalizedStringKey
    @Binding var value: Int?
    @State private var text = ""

    var body: some View {
        LabeledContent {
            TextField(text: $text, prompt: Text("manage.default")) { EmptyView() }
                .keyboardType(.numberPad)
                .multilineTextAlignment(.trailing)
                .frame(minWidth: 80)
                .onAppear { text = value.map(String.init) ?? "" }
                .onChange(of: text) { _, now in
                    let parsed = Int(now.trimmingCharacters(in: .whitespaces))
                    if parsed != value, parsed != nil || now.trimmingCharacters(in: .whitespaces).isEmpty { value = parsed }
                }
                .onChange(of: value) { _, now in
                    if Int(text.trimmingCharacters(in: .whitespaces)) != now { text = now.map(String.init) ?? "" }
                }
        } label: { Text(title) }
    }
}

// MARK: Radarr / Sonarr servers

/// Seerr's Radarr or Sonarr connections.
struct SeerrServersView: View {
    let client: ArrClient
    let kind: ArrService
    @State private var servers: [JSONValue]?
    @State private var error: Error?
    @State private var pendingDelete: JSONValue?

    private var resource: String { "settings/\(kind.rawValue)" }

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let servers {
                ForEach(servers, id: \.rowID) { server in
                    NavigationLink {
                        SeerrServerEditor(client: client, kind: kind, server: server, isNew: false)
                    } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(verbatim: server.firstString("name", "hostname") ?? "#\(server["id"].text)").font(.body.weight(.semibold))
                            HStack(spacing: 6) {
                                Text(verbatim: "\(server["hostname"].text):\(server["port"].text)").font(.caption.monospaced()).foregroundStyle(Theme.textMuted)
                                if server["is4k"].bool == true { PlainBadge(text: "4K", tone: .accent) }
                                if server["isDefault"].bool == true { TagBadge(text: "manage.default", tone: .success) }
                            }
                        }
                    }
                    .swipeActions {
                        Button(role: .destructive) { pendingDelete = server } label: { Label("manage.delete", systemImage: "trash") }
                    }
                }
                if servers.isEmpty { EmptyRow(text: "manage.empty") }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: kind.name))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                NavigationLink {
                    SeerrServerEditor(client: client, kind: kind, server: SeerrServerForm.template(kind), isNew: true)
                } label: { Image(systemName: "plus") }
                .accessibilityLabel(Text("manage.add"))
            }
        }
        .onAppear { Task { await load() } }
        .refreshable { await load() }
        .confirmationDialog("manage.delete_confirm", isPresented: Binding(get: { pendingDelete != nil }, set: { if !$0 { pendingDelete = nil } }), titleVisibility: .visible) {
            Button("manage.delete", role: .destructive) {
                if let s = pendingDelete { Task { await delete(s) } }
            }
        }
    }

    private func load() async {
        do {
            servers = try await client.get(resource).list
            error = nil
        } catch { self.error = error }
    }

    private func delete(_ server: JSONValue) async {
        do {
            try await client.send("DELETE", "\(resource)/\(server["id"].text)")
            await load()
        } catch { self.error = error }
    }
}

/// A Radarr or Sonarr connection of Seerr. "Test" asks Seerr to reach the server and fills the
/// pickers (quality profiles, root folders, language profiles, tags) from its answer; values
/// can still be typed by hand.
struct SeerrServerEditor: View {
    let client: ArrClient
    let kind: ArrService
    let isNew: Bool
    @State private var state: ObjectFormState
    @State private var test: SeerrServerTest?
    @State private var manual = false
    @State private var testing = false
    @State private var busy = false
    @State private var failures: [ArrValidationFailure] = []
    @State private var error: Error?
    @State private var notice: ManageNotice?
    @Environment(\.dismiss) private var dismiss

    init(client: ArrClient, kind: ArrService, server: JSONValue, isNew: Bool) {
        self.client = client
        self.kind = kind
        self.isNew = isNew
        _state = State(initialValue: ObjectFormState(server))
    }

    private var resource: String { "settings/\(kind.rawValue)" }

    private var allKeys: [String] {
        (state.original.object ?? [:]).keys.filter { !SeerrServerForm.hiddenKeys.contains($0) }.sorted()
    }

    private struct KeySection: Identifiable {
        let title: String
        let keys: [String]
        var id: String { title }
    }

    private var sections: [KeySection] {
        let keys = allKeys
        let connection = SeerrServerForm.connectionKeys.filter(keys.contains)
        let defaults = SeerrServerForm.defaultsKeys(kind).filter(keys.contains)
        let anime = kind == .sonarr ? SeerrServerForm.animeKeys.filter(keys.contains) : []
        let used = Set(connection + defaults + anime)
        let rest = keys.filter { !used.contains($0) }
        return [KeySection(title: "manage.connection", keys: connection), KeySection(title: "manage.defaults", keys: defaults),
                KeySection(title: "manage.anime", keys: anime), KeySection(title: "manage.other", keys: rest)]
            .filter { !$0.keys.isEmpty }
    }

    private var options: [String: [ArrSelectOption]] {
        var out: [String: [ArrSelectOption]] = [
            "minimumAvailability": apiOptions(["announced", "inCinemas", "released"]),
            "seriesType": apiOptions(["standard", "daily", "anime"]),
            "animeSeriesType": apiOptions(["standard", "daily", "anime"]),
        ]
        if !manual, let test { out.merge(test.options) { _, new in new } }
        return out
    }

    var body: some View {
        Form {
            ForEach(sections) { section in
                Section {
                    ForEach(section.keys, id: \.self) { key in
                        VStack(alignment: .leading, spacing: 4) {
                            SeerrServerFieldRow(key: key, state: $state, options: options[key], tested: test != nil && !manual)
                            ForEach(failures.filter { $0.matches(key) }, id: \.self) { f in
                                Text(verbatim: f.message).font(.caption).foregroundStyle(f.isWarning ? .orange : .red)
                            }
                        }
                    }
                } header: { Text(LocalizedStringKey(section.title)) }
            }
            Section {
                Button { Task { await runTest(silent: false) } } label: {
                    HStack {
                        Label("manage.test", systemImage: "bolt.horizontal.circle")
                        Spacer()
                        if testing { ProgressView() } else if test != nil { Image(systemName: "checkmark.circle.fill").foregroundStyle(.green) }
                    }
                }
                .disabled(testing)
                Toggle(isOn: $manual) { Text("manage.manual_entry") }
            } footer: {
                Text(test == nil ? LocalizedStringKey("manage.server_test_hint") : LocalizedStringKey("manage.server_test_loaded"))
            }
            Section {
                ForEach(failures.filter { $0.propertyName.isEmpty }, id: \.self) { f in
                    Text(verbatim: f.message).font(.footnote).foregroundStyle(.red)
                }
                ArrErrorLine(error: error)
                Button { Task { await save() } } label: {
                    HStack { Text("save"); Spacer(); if busy { ProgressView() } }
                }
                .disabled(busy)
            }
        }
        .manageBackground()
        .navigationTitle(Text(isNew ? LocalizedStringKey("manage.add") : LocalizedStringKey("manage.edit")))
        .navigationBarTitleDisplayMode(.inline)
        .task { if !isNew { await runTest(silent: true) } }
        .manageNotice($notice)
    }

    private func runTest(silent: Bool) async {
        testing = true
        defer { testing = false }
        do {
            let result = try await client.send("POST", "\(resource)/test", json: state.fullObject())
            withAnimation(.snappy) { test = SeerrServerTest(result) }
            if !silent {
                error = nil
                notice = ManageNotice(text: "manage.test_ok")
            }
        } catch {
            if !silent {
                self.error = error
                notice = ManageNotice(text: "manage.test_failed", failed: true)
            }
        }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        var body = state.fullObject()
        if let test { body = test.syncingNames(body) }
        do {
            if isNew {
                try await client.send("POST", resource, json: body)
            } else {
                try await client.send("PUT", "\(resource)/\(state.original["id"].text)", json: body)
            }
            dismiss()
        } catch let e as ArrError where !e.failures.isEmpty {
            failures = e.failures
        } catch { self.error = error }
    }
}

/// One key of a Seerr server form: a picker when the test gave choices, else a typed value.
private struct SeerrServerFieldRow: View {
    let key: String
    @Binding var state: ObjectFormState
    let options: [ArrSelectOption]?
    let tested: Bool

    private var binding: Binding<JSONValue> { Binding(get: { state.value(key) }, set: { state.set(key, $0) }) }

    var body: some View {
        let value = binding.wrappedValue
        if let options, !options.isEmpty {
            ObjectFieldRow(path: key, state: $state, options: options, tags: [])
        } else if key.hasSuffix("Id"), value.isNull || value.isNumber {
            // Profile ids typed by hand (no test answer yet).
            LabeledContent {
                JSONNumberField(value: binding)
            } label: { Text(verbatim: key.humanizedKey) }
        } else if key == "tags" || key == "animeTags" {
            // Without a test there are no tag names; keep the ids as they are.
            MultiSelectRow(title: key.humanizedKey,
                           options: value.list.map { ArrSelectOption(value: $0, name: "#\($0.text)", hint: nil) },
                           selection: arrayBinding(binding)) { Text(verbatim: key.humanizedKey) }
        } else {
            ObjectFieldRow(path: key, state: $state, options: nil, tags: [])
        }
    }
}

// MARK: Media server

/// The media server Seerr signs users in with and syncs availability from (here: Streamero
/// through its compatible API, or Plex): connection, libraries and scans.
struct SeerrMediaServerView: View {
    let client: ArrClient
    @State private var resource: String?
    @State private var noServer = false
    @State private var error: Error?

    var body: some View {
        List {
            if let error { ArrErrorState(error: error) { await load() } }
            if noServer {
                Section { Text("manage.media_server_none").foregroundStyle(Theme.textMuted) }
            } else if let resource {
                Section {
                    NavigationLink {
                        ConfigEditorView(client: client, resource: resource, titleKey: "manage.connection", saveMode: .post,
                                         hiddenKeys: ["id", "libraries", "machineId", "serverId"])
                    } label: { MenuRow(title: "manage.connection", icon: "link") }
                    NavigationLink {
                        SeerrLibrariesView(client: client, resource: resource)
                    } label: { MenuRow(title: "manage.libraries", icon: "books.vertical") }
                } footer: { Text("manage.media_server_hint") }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.media_server"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        do {
            let settings = try await client.get("settings/public")
            switch settings["mediaServerType"].int {
            case 1: resource = "settings/plex"
            // Seerr's documented endpoint for servers speaking the compatible API (D-011). It is
            // an external API path only; the app never shows the name.
            case 2, 3: resource = "settings/jellyfin"
            default: noServer = true
            }
            error = nil
        } catch { self.error = error }
    }
}

/// Libraries the media server offers to Seerr: pick which ones Seerr syncs, and start a scan.
struct SeerrLibrariesView: View {
    let client: ArrClient
    let resource: String
    @State private var libraries: [JSONValue]?
    @State private var enabled: Set<String> = []
    @State private var original: Set<String> = []
    @State private var sync: JSONValue = .null
    @State private var error: Error?
    @State private var busy = false
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let libraries {
                Section {
                    ForEach(libraries, id: \.rowID) { lib in
                        let id = lib["id"].text
                        Toggle(isOn: Binding(get: { enabled.contains(id) }, set: { if $0 { enabled.insert(id) } else { enabled.remove(id) } })) {
                            Text(verbatim: lib["name"].string ?? id)
                        }
                    }
                    if libraries.isEmpty { EmptyRow(text: "manage.empty") }
                    Button { Task { await save() } } label: {
                        HStack { Text("save"); Spacer(); if busy { ProgressView() } }
                    }
                    .disabled(busy || enabled == original)
                }
                Section {
                    if sync["running"].bool == true {
                        VStack(alignment: .leading, spacing: 6) {
                            Text(verbatim: sync["currentLibrary"]["name"].string ?? "").font(.subheadline)
                            let total = sync["total"].double ?? 0
                            ThinProgress(value: total > 0 ? (sync["progress"].double ?? 0) / total : 0)
                        }
                        Button("manage.cancel_scan", role: .destructive) { Task { await scan(cancel: true) } }
                    } else {
                        Button { Task { await scan(cancel: false) } } label: { Label("manage.scan_libraries", systemImage: "arrow.triangle.2.circlepath") }
                    }
                } footer: { Text("manage.scan_libraries_hint") }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.libraries"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .task(id: sync["running"].bool == true) {
            // Follow a running scan.
            while sync["running"].bool == true, !Task.isCancelled {
                try? await Task.sleep(for: .seconds(2))
                if let s = try? await client.get("\(resource)/sync") { sync = s }
            }
        }
        .refreshable { await load() }
        .manageNotice($notice)
    }

    private func apply(_ list: [JSONValue]) {
        libraries = list
        enabled = Set(list.filter { $0["enabled"].bool == true }.map { $0["id"].text })
        original = enabled
    }

    private func load() async {
        do {
            apply(try await client.get("\(resource)/library", query: [.q("sync", "true")]).list)
            sync = (try? await client.get("\(resource)/sync")) ?? .null
            error = nil
        } catch { self.error = error }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        let ids = (libraries ?? []).map { $0["id"].text }.filter(enabled.contains)
        do {
            apply(try await client.get("\(resource)/library", query: [.q("enable", ids.joined(separator: ","))]).list)
            notice = ManageNotice(text: "saved")
            error = nil
        } catch { self.error = error }
    }

    private func scan(cancel: Bool) async {
        do {
            let body: JSONValue = cancel ? ["cancel": true] : ["start": true]
            sync = try await client.send("POST", "\(resource)/sync", json: body)
            if !cancel { notice = ManageNotice(text: "manage.command_started") }
            error = nil
        } catch { self.error = error }
    }
}
