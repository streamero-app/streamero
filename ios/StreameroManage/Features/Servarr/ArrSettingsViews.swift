import SwiftUI

/// Known enum values of Servarr config keys (shown as the API names them).
enum ServarrOptions {
    static let config: [String: [ArrSelectOption]] = [
        "authenticationMethod": apiOptions(["none", "basic", "forms", "external"]),
        "authenticationRequired": apiOptions(["enabled", "disabledForLocalAddresses"]),
        "certificateValidation": apiOptions(["enabled", "disabledForLocalAddresses", "disabled"]),
        "proxyType": apiOptions(["http", "socks4", "socks5"]),
        "logLevel": apiOptions(["info", "debug", "trace"]),
        "consoleLogLevel": apiOptions(["", "info", "debug", "trace"]),
        "updateMechanism": apiOptions(["builtIn", "script", "external", "apt", "docker"]),
        "branch": apiOptions(["master", "develop", "nightly"]),
        "fileDate": apiOptions(["none", "cinemas", "release", "localAirDate", "utcAirDate", "albumReleaseDate"]),
        "rescanAfterRefresh": apiOptions(["always", "afterManual", "never"]),
        "downloadPropersAndRepacks": apiOptions(["preferAndUpgrade", "doNotUpgrade", "doNotPrefer"]),
        "episodeTitleRequired": apiOptions(["always", "bulkSeasonReleases", "never"]),
        "allowFingerprinting": apiOptions(["never", "newFiles", "allFiles"]),
        "colonReplacementFormat": apiOptions(["delete", "dash", "spaceDash", "spaceDashSpace", "smart"]),
        "preferredProtocol": apiOptions(["usenet", "torrent"]),
        "theme": apiOptions(["auto", "dark", "light"]),
    ]

    /// `config/host` split the way the services' own settings pages are.
    static let hostSections: [ConfigSection] = [
        ConfigSection(titleKey: "manage.section.host", keys: ["bindAddress", "port", "urlBase", "instanceName", "applicationUrl", "launchBrowser"]),
        ConfigSection(titleKey: "manage.section.security", keys: ["authenticationMethod", "authenticationRequired", "username", "password", "passwordConfirmation", "apiKey", "certificateValidation", "trustCgnatIpAddresses"]),
        ConfigSection(titleKey: "manage.section.ssl", keys: ["enableSsl", "sslPort", "sslCertPath", "sslCertPassword", "sslKeyPath"]),
        ConfigSection(titleKey: "manage.section.proxy", keys: ["proxyEnabled", "proxyType", "proxyHostname", "proxyPort", "proxyUsername", "proxyPassword", "proxyBypassFilter", "proxyBypassLocalAddresses"]),
        ConfigSection(titleKey: "manage.section.logging", keys: ["logLevel", "consoleLogLevel", "logSizeLimit", "analyticsEnabled"]),
        ConfigSection(titleKey: "manage.section.backup", keys: ["backupFolder", "backupInterval", "backupRetention"]),
        ConfigSection(titleKey: "manage.section.updates", keys: ["branch", "updateAutomatically", "updateMechanism", "updateScriptPath"]),
    ]
}

/// Every settings area of Radarr, Sonarr, Lidarr or Prowlarr.
struct ServarrSettingsMenu: View {
    let client: ArrClient

    var body: some View {
        List {
            if client.service.hasLibrary {
                Section {
                    link("manage.media_management", "folder") {
                        ConfigEditorView(client: client, resource: "config/mediamanagement", titleKey: "manage.media_management", options: ServarrOptions.config)
                    }
                    link("manage.naming", "textformat") {
                        ConfigEditorView(client: client, resource: "config/naming", titleKey: "manage.naming", options: ServarrOptions.config,
                                         examplesResource: "config/naming/examples")
                    }
                    link("manage.root_folders", "externaldrive.connected.to.line.below") { RootFoldersView(client: client) }
                    link("manage.remote_path_mappings", "arrow.left.arrow.right") {
                        ObjectListView(client: client, resource: "remotepathmapping", titleKey: "manage.remote_path_mappings",
                                       titleKeys: ["host"], subtitleKeys: ["remotePath", "localPath"],
                                       template: ["host": "", "remotePath": "", "localPath": ""])
                    }
                } header: { Text("manage.section.media") }
                Section {
                    link("manage.quality_profiles", "slider.horizontal.3") { QualityProfilesView(client: client) }
                    link("manage.quality_definitions", "ruler") { QualityDefinitionsView(client: client) }
                    link("manage.custom_formats", "wand.and.stars") { CustomFormatsView(client: client) }
                    link("manage.delay_profiles", "hourglass") {
                        ObjectListView(client: client, resource: "delayprofile", titleKey: "manage.delay_profiles",
                                       titleKeys: ["preferredProtocol"], subtitleKeys: ["usenetDelay", "torrentDelay"],
                                       template: ["enableUsenet": true, "enableTorrent": true, "preferredProtocol": "usenet",
                                                  "usenetDelay": 0, "torrentDelay": 0, "bypassIfHighestQuality": false,
                                                  "bypassIfAboveCustomFormatScore": false, "minimumCustomFormatScore": 0, "order": 1, "tags": []],
                                       options: ServarrOptions.config)
                    }
                    link("manage.release_profiles", "line.3.horizontal.decrease") {
                        ObjectListView(client: client, resource: "releaseprofile", titleKey: "manage.release_profiles",
                                       titleKeys: ["name"], subtitleKeys: ["required", "ignored"],
                                       template: ["name": "", "enabled": true, "required": [], "ignored": [], "indexerId": 0, "tags": []])
                    }
                    if client.service == .lidarr {
                        link("manage.metadata_profiles", "music.note.list") { MetadataProfilesView(client: client) }
                    }
                } header: { Text("manage.section.profiles") }
            }
            Section {
                if client.service == .prowlarr {
                    link("manage.indexers", ProviderKind.indexers.icon) { ProviderListView(client: client, kind: .indexers) }
                    link("manage.apps", ProviderKind.applications.icon) { ProviderListView(client: client, kind: .applications) }
                    link("manage.app_profiles", "person.crop.rectangle.stack") {
                        ObjectListView(client: client, resource: "appprofile", titleKey: "manage.app_profiles",
                                       template: ["name": "", "enableRss": true, "enableAutomaticSearch": true,
                                                  "enableInteractiveSearch": true, "minimumSeeders": 1])
                    }
                    link("manage.indexer_proxies", ProviderKind.indexerProxies.icon) { ProviderListView(client: client, kind: .indexerProxies) }
                } else {
                    link("manage.indexers", ProviderKind.indexers.icon) { ProviderListView(client: client, kind: .indexers) }
                    link("manage.indexer_options", "gearshape.2") {
                        ConfigEditorView(client: client, resource: "config/indexer", titleKey: "manage.indexer_options", options: ServarrOptions.config)
                    }
                }
                link("manage.download_clients", ProviderKind.downloadClients.icon) { ProviderListView(client: client, kind: .downloadClients) }
                if client.service.hasLibrary {
                    link("manage.download_client_options", "gearshape.2") {
                        ConfigEditorView(client: client, resource: "config/downloadclient", titleKey: "manage.download_client_options", options: ServarrOptions.config)
                    }
                    link("manage.import_lists", ProviderKind.importLists.icon) { ProviderListView(client: client, kind: .importLists) }
                }
                link("manage.connections", ProviderKind.notifications.icon) { ProviderListView(client: client, kind: .notifications) }
                if client.service.hasLibrary {
                    link("manage.metadata", ProviderKind.metadata.icon) { ProviderListView(client: client, kind: .metadata) }
                }
                link("manage.tags", "tag") { TagsView(client: client) }
            } header: { Text("manage.section.integrations") }
            Section {
                link("manage.general", "gearshape") {
                    ConfigEditorView(client: client, resource: "config/host", titleKey: "manage.general",
                                     sections: ServarrOptions.hostSections, options: ServarrOptions.config)
                }
                link("manage.ui", "paintbrush") {
                    ConfigEditorView(client: client, resource: "config/ui", titleKey: "manage.ui", options: ServarrOptions.config)
                }
            } header: { Text("manage.section.general") }
        }
        .manageBackground()
        .navigationTitle(Text("manage.settings"))
        .navigationBarTitleDisplayMode(.inline)
    }

    private func link<D: View>(_ title: LocalizedStringKey, _ icon: String, @ViewBuilder _ destination: @escaping () -> D) -> some View {
        NavigationLink { destination() } label: { MenuRow(title: title, icon: icon) }
    }
}

// MARK: Quality profiles

struct QualityProfilesView: View {
    let client: ArrClient
    @State private var profiles: [JSONValue]?
    @State private var error: Error?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            ForEach(profiles ?? [], id: \.rowID) { p in
                NavigationLink {
                    QualityProfileEditor(client: client, profile: p, isNew: false)
                } label: {
                    let model = QualityProfileModel(json: p)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: model.name).font(.body.weight(.semibold))
                        Text(verbatim: model.allowedItems.map(\.name).joined(separator: ", "))
                            .font(.caption).foregroundStyle(Theme.textMuted).lineLimit(2)
                    }
                }
            }
            if profiles == nil && error == nil { LoadingView() }
        }
        .manageBackground()
        .navigationTitle(Text("manage.quality_profiles"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                NavigationLink {
                    QualityProfileTemplateLoader(client: client)
                } label: { Image(systemName: "plus") }
                .accessibilityLabel(Text("manage.add"))
            }
        }
        .onAppear { Task { await load() } }
    }

    private func load() async {
        do {
            profiles = try await client.get("qualityprofile").list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// A new profile starts from `qualityprofile/schema`.
private struct QualityProfileTemplateLoader: View {
    let client: ArrClient
    @State private var template: JSONValue?
    @State private var error: Error?

    var body: some View {
        Group {
            if let template {
                QualityProfileEditor(client: client, profile: template, isNew: true)
            } else if let error {
                ArrErrorState(error: error) { await load() }
            } else {
                LoadingView()
            }
        }
        .task { await load() }
    }

    private func load() async {
        do { template = try await client.get("qualityprofile/schema") } catch is CancellationError {} catch { self.error = error }
    }
}

/// Name, upgrades, cutoff, the order and choice of qualities, custom format scores.
struct QualityProfileEditor: View {
    let client: ArrClient
    let isNew: Bool
    @State private var model: QualityProfileModel
    @State private var error: Error?
    @State private var busy = false
    @State private var editMode: EditMode = .inactive
    @Environment(\.dismiss) private var dismiss

    init(client: ArrClient, profile: JSONValue, isNew: Bool) {
        self.client = client
        self.isNew = isNew
        _model = State(initialValue: QualityProfileModel(json: profile))
    }

    var body: some View {
        List {
            Section {
                TextField(text: $model.name) { Text("manage.name") }
                Toggle("manage.upgrades_allowed", isOn: $model.upgradeAllowed)
                Picker(selection: $model.cutoff) {
                    ForEach(model.allowedItems) { item in Text(verbatim: item.name).tag(item.id) }
                } label: { Text("manage.upgrade_until") }
                .disabled(!model.upgradeAllowed)
            }
            Section {
                ForEach(model.items) { item in
                    HStack {
                        Button {
                            model.setAllowed(item.id, !item.allowed)
                        } label: {
                            Image(systemName: item.allowed ? "checkmark.square.fill" : "square")
                                .foregroundStyle(item.allowed ? Theme.accent : Theme.textFaint)
                        }
                        .buttonStyle(.borderless)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: item.name).foregroundStyle(item.allowed ? Theme.text : Theme.textMuted)
                            if item.isGroup { Text(verbatim: item.members.joined(separator: ", ")).font(.caption2).foregroundStyle(Theme.textFaint) }
                        }
                        Spacer()
                        if item.id == model.cutoff && model.upgradeAllowed { TagBadge(text: "manage.cutoff", tone: .accent) }
                    }
                }
                .onMove { model.move(fromOffsets: $0, toOffset: $1) }
            } header: {
                HStack {
                    Text("manage.qualities")
                    Spacer()
                    Button(editMode == .active ? LocalizedStringKey("manage.done") : LocalizedStringKey("manage.reorder")) {
                        withAnimation { editMode = editMode == .active ? .inactive : .active }
                    }
                    .font(.caption.weight(.semibold))
                }
            } footer: { Text("manage.qualities_hint") }
            if !model.formatScores.isEmpty {
                Section {
                    LabeledContent { scoreField(Binding(get: { model.minFormatScore }, set: { model.minFormatScore = $0 })) } label: { Text("manage.min_format_score") }
                    LabeledContent { scoreField(Binding(get: { model.cutoffFormatScore }, set: { model.cutoffFormatScore = $0 })) } label: { Text("manage.cutoff_format_score") }
                    if model.minUpgradeFormatScore != nil {
                        LabeledContent { scoreField(Binding(get: { model.minUpgradeFormatScore ?? 0 }, set: { model.minUpgradeFormatScore = $0 })) } label: { Text("manage.min_upgrade_format_score") }
                    }
                    ForEach(model.formatScores) { f in
                        LabeledContent {
                            scoreField(Binding(get: { f.score }, set: { model.setScore(format: f.id, $0) }))
                        } label: { Text(verbatim: f.name) }
                    }
                } header: { Text("manage.custom_format_scores") }
            }
            Section {
                ArrErrorLine(error: error)
                Button { Task { await save() } } label: { HStack { Text("save"); Spacer(); if busy { ProgressView() } } }
                    .disabled(busy || model.name.isEmpty)
                if !isNew {
                    ConfirmDeleteButton { Task { await delete() } }
                }
            }
        }
        .environment(\.editMode, $editMode)
        .manageBackground()
        .navigationTitle(Text(verbatim: model.name))
        .navigationBarTitleDisplayMode(.inline)
    }

    private func scoreField(_ value: Binding<Int>) -> some View {
        JSONNumberField(value: Binding(get: { .number(value.wrappedValue) }, set: { value.wrappedValue = $0.int ?? 0 }))
    }

    private func save() async {
        busy = true
        defer { busy = false }
        do {
            if isNew {
                try await client.send("POST", "qualityprofile", json: model.json())
            } else {
                try await client.send("PUT", "qualityprofile/\(model.raw["id"].text)", json: model.json())
            }
            dismiss()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func delete() async {
        do {
            try await client.send("DELETE", "qualityprofile/\(model.raw["id"].text)")
            dismiss()
        } catch is CancellationError {} catch { self.error = error }
    }
}

// MARK: Quality definitions

/// File size limits per quality (MB per minute).
struct QualityDefinitionsView: View {
    let client: ArrClient
    @State private var items: [JSONValue]?
    @State private var original: [JSONValue] = []
    @State private var error: Error?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let items {
                Section {
                    Text("manage.quality_definitions_hint").font(.caption).foregroundStyle(Theme.textMuted)
                }
                ForEach(items.indices, id: \.self) { i in
                    Section {
                        sizeRow("manage.size_min", i, "minSize")
                        if !items[i]["preferredSize"].isNull { sizeRow("manage.size_preferred", i, "preferredSize") }
                        sizeRow("manage.size_max", i, "maxSize")
                    } header: { Text(verbatim: items[i]["title"].string ?? items[i]["quality"]["name"].string ?? "") }
                }
                Section {
                    Button { Task { await save() } } label: { Text("save") }.disabled(items == original)
                }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.quality_definitions"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .manageNotice($notice)
    }

    private func sizeRow(_ title: LocalizedStringKey, _ i: Int, _ key: String) -> some View {
        LabeledContent {
            JSONNumberField(value: Binding(get: { items?[i][key] ?? .null }, set: { v in items?[i][key] = v }), isFloat: true, allowsNull: true)
        } label: { Text(title) }
    }

    private func load() async {
        do {
            let list = try await client.get("qualitydefinition").list
            items = list
            original = list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func save() async {
        guard let items else { return }
        do {
            try await client.send("PUT", "qualitydefinition/update", json: .array(items))
            original = items
            notice = ManageNotice(text: "saved")
        } catch is CancellationError {} catch { self.error = error }
    }
}

// MARK: Custom formats

struct CustomFormatsView: View {
    let client: ArrClient
    @State private var formats: [JSONValue]?
    @State private var error: Error?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            ForEach(formats ?? [], id: \.rowID) { f in
                NavigationLink {
                    CustomFormatEditor(client: client, format: f, isNew: false)
                } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: f["name"].string ?? "").font(.body.weight(.semibold))
                        Text(verbatim: f["specifications"].list.compactMap { $0["name"].string }.joined(separator: ", "))
                            .font(.caption).foregroundStyle(Theme.textMuted).lineLimit(2)
                    }
                }
            }
            if formats?.isEmpty == true { EmptyRow(text: "manage.empty") }
            if formats == nil && error == nil { LoadingView() }
        }
        .manageBackground()
        .navigationTitle(Text("manage.custom_formats"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                NavigationLink {
                    CustomFormatEditor(client: client, format: ["name": "", "includeCustomFormatWhenRenaming": false, "specifications": []], isNew: true)
                } label: { Image(systemName: "plus") }
                .accessibilityLabel(Text("manage.add"))
            }
        }
        .onAppear { Task { await load() } }
    }

    private func load() async {
        do {
            formats = try await client.get("customformat").list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// A custom format: name, renaming switch, and its conditions (specifications with schema fields).
struct CustomFormatEditor: View {
    let client: ArrClient
    let isNew: Bool
    @State private var format: JSONValue
    @State private var specs: [SpecDraft]
    @State private var templates: [JSONValue] = []
    @State private var failures: [ArrValidationFailure] = []
    @State private var error: Error?
    @State private var busy = false
    @Environment(\.dismiss) private var dismiss

    struct SpecDraft: Identifiable, Equatable {
        let id = UUID()
        var raw: JSONValue
        var form: SchemaFormState

        init(_ raw: JSONValue) {
            self.raw = raw
            form = SchemaFormState(json: raw["fields"])
        }

        var json: JSONValue {
            var out = raw
            out["fields"] = form.outgoingFields()
            return out
        }
    }

    init(client: ArrClient, format: JSONValue, isNew: Bool) {
        self.client = client
        self.isNew = isNew
        _format = State(initialValue: format)
        _specs = State(initialValue: format["specifications"].list.map(SpecDraft.init))
    }

    var body: some View {
        Form {
            Section {
                TextField(text: Binding(get: { format["name"].string ?? "" }, set: { format["name"] = .string($0) })) { Text("manage.name") }
                Toggle(isOn: Binding(get: { format["includeCustomFormatWhenRenaming"].bool ?? false },
                                     set: { format["includeCustomFormatWhenRenaming"] = .bool($0) })) {
                    Text("manage.include_when_renaming")
                }
            }
            ForEach($specs) { $spec in
                Section {
                    TextField(text: Binding(get: { spec.raw["name"].string ?? "" }, set: { spec.raw["name"] = .string($0) })) { Text("manage.name") }
                    Toggle(isOn: Binding(get: { spec.raw["negate"].bool ?? false }, set: { spec.raw["negate"] = .bool($0) })) { Text("manage.negate") }
                    Toggle(isOn: Binding(get: { spec.raw["required"].bool ?? false }, set: { spec.raw["required"] = .bool($0) })) { Text("manage.required") }
                    SchemaFieldsView(state: $spec.form, failures: failures, showAdvanced: true)
                    Button("manage.remove_condition", role: .destructive) {
                        specs.removeAll { $0.id == spec.id }
                    }
                } header: {
                    Text(verbatim: spec.raw["implementationName"].string ?? spec.raw["implementation"].string ?? "")
                }
            }
            Section {
                Menu {
                    ForEach(templates, id: \.self) { t in
                        Button {
                            var draft = t
                            draft["name"] = .string(t["implementationName"].string ?? "")
                            specs.append(SpecDraft(draft))
                        } label: { Text(verbatim: t["implementationName"].string ?? t["implementation"].string ?? "") }
                    }
                } label: { Label("manage.add_condition", systemImage: "plus.circle") }
            }
            Section {
                ForEach(failures, id: \.self) { f in Text(verbatim: f.message).font(.footnote).foregroundStyle(f.isWarning ? .orange : .red) }
                ArrErrorLine(error: error)
                Button { Task { await save() } } label: { HStack { Text("save"); Spacer(); if busy { ProgressView() } } }.disabled(busy)
                if !isNew { ConfirmDeleteButton { Task { await delete() } } }
            }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: format["name"].string?.nilIfBlank ?? Loc.string("manage.custom_formats")))
        .navigationBarTitleDisplayMode(.inline)
        .task { templates = (try? await client.get("customformat/schema").list) ?? [] }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        var body = format
        body["specifications"] = .array(specs.map(\.json))
        do {
            if isNew {
                try await client.send("POST", "customformat", json: body)
            } else {
                try await client.send("PUT", "customformat/\(format["id"].text)", json: body)
            }
            dismiss()
        } catch let e as ArrError where !e.failures.isEmpty {
            failures = e.failures
        } catch is CancellationError {} catch { self.error = error }
    }

    private func delete() async {
        do {
            try await client.send("DELETE", "customformat/\(format["id"].text)")
            dismiss()
        } catch is CancellationError {} catch { self.error = error }
    }
}

// MARK: Root folders, tags, metadata profiles

struct RootFoldersView: View {
    @State private var pendingDelete: JSONValue?
    let client: ArrClient
    @State private var folders: [JSONValue]?
    @State private var error: Error?
    @State private var newPath = ""

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            Section {
                HStack {
                    TextField(text: $newPath, prompt: Text(verbatim: "/data/media")) { Text("manage.path") }
                        .font(.body.monospaced()).textInputAutocapitalization(.never).autocorrectionDisabled()
                    Button { Task { await add() } } label: { Image(systemName: "plus.circle.fill") }
                        .disabled(newPath.trimmingCharacters(in: .whitespaces).isEmpty)
                        .accessibilityLabel(Text("manage.add"))
                }
            } header: { Text("manage.add_root_folder") }
            Section {
                ForEach(folders ?? [], id: \.rowID) { f in
                    VStack(alignment: .leading, spacing: 3) {
                        HStack {
                            Text(verbatim: f["path"].string ?? "").font(.footnote.monospaced())
                            if f["accessible"].bool == false { TagBadge(text: "manage.unreachable", tone: .danger) }
                        }
                        Text(verbatim: [byteText(f["freeSpace"]), f["unmappedFolders"].array.map { "\($0.count)" }].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.caption2).foregroundStyle(Theme.textFaint)
                    }
                    .swipeActions {
                        Button(role: .destructive) { pendingDelete = f } label: { Label("manage.delete", systemImage: "trash") }
                    }
                }
                if folders == nil && error == nil { LoadingView() }
            }
        }
        .manageBackground()
        .confirmDelete($pendingDelete, detail: { $0["path"].text }) { f in Task { await delete(f) } }
        .navigationTitle(Text("manage.root_folders"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        do {
            folders = try await client.get("rootfolder").list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func add() async {
        let path = newPath.trimmingCharacters(in: .whitespaces)
        var body: JSONValue = ["path": .string(path)]
        if client.service == .lidarr {
            // Lidarr root folders carry defaults for new artists.
            let profiles = (try? await client.get("qualityprofile").list) ?? []
            let metadata = (try? await client.get("metadataprofile").list) ?? []
            body["name"] = .string((path as NSString).lastPathComponent)
            body["defaultQualityProfileId"] = profiles.first?["id"] ?? .int(1)
            body["defaultMetadataProfileId"] = metadata.first?["id"] ?? .int(1)
            body["defaultMonitorOption"] = "all"
            body["defaultNewItemMonitorOption"] = "all"
            body["defaultTags"] = []
        }
        do {
            try await client.send("POST", "rootfolder", json: body)
            newPath = ""
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func delete(_ f: JSONValue) async {
        do {
            try await client.send("DELETE", "rootfolder/\(f["id"].text)")
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }
}

struct TagsView: View {
    @State private var pendingDelete: JSONValue?
    let client: ArrClient
    @State private var tags: [JSONValue]?
    @State private var usage: [Int: Int] = [:]
    @State private var error: Error?
    @State private var newTag = ""
    @State private var renaming: JSONValue?
    @State private var renameText = ""

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            Section {
                HStack {
                    TextField(text: $newTag) { Text("manage.new_tag") }
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                    Button { Task { await add() } } label: { Image(systemName: "plus.circle.fill") }
                        .disabled(newTag.trimmingCharacters(in: .whitespaces).isEmpty)
                        .accessibilityLabel(Text("manage.add"))
                }
            }
            Section {
                ForEach(tags ?? [], id: \.rowID) { t in
                    HStack {
                        Text(verbatim: t["label"].string ?? "")
                        Spacer()
                        if let n = usage[t["id"].int ?? -1], n > 0 { Text(verbatim: "\(n)").font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted) }
                    }
                    .swipeActions {
                        Button(role: .destructive) { pendingDelete = t } label: { Label("manage.delete", systemImage: "trash") }
                        Button {
                            renameText = t["label"].string ?? ""
                            renaming = t
                        } label: { Label("manage.rename", systemImage: "pencil") }
                    }
                }
                if tags == nil && error == nil { LoadingView() }
            }
        }
        .manageBackground()
        .confirmDelete($pendingDelete, detail: { $0["label"].text }) { t in Task { await delete(t) } }
        .navigationTitle(Text("manage.tags"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .alert("manage.rename", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
            TextField(text: $renameText) { Text("manage.name") }
            Button("save") { if let t = renaming { Task { await rename(t) } } }
            Button("common.cancel", role: .cancel) {}
        }
    }

    private func load() async {
        do {
            tags = try await client.get("tag").list
            let details = (try? await client.get("tag/detail").list) ?? []
            var counts: [Int: Int] = [:]
            for d in details {
                let n = d.object?.values.reduce(0) { $0 + ($1.array?.count ?? 0) } ?? 0
                counts[d["id"].int ?? -1] = n
            }
            usage = counts
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func add() async {
        do {
            try await client.send("POST", "tag", json: ["label": .string(newTag.trimmingCharacters(in: .whitespaces).lowercased())])
            newTag = ""
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func rename(_ t: JSONValue) async {
        var body = t
        body["label"] = .string(renameText.trimmingCharacters(in: .whitespaces).lowercased())
        do {
            try await client.send("PUT", "tag/\(t["id"].text)", json: body)
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func delete(_ t: JSONValue) async {
        do {
            try await client.send("DELETE", "tag/\(t["id"].text)")
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// Lidarr metadata profiles: which album types and release statuses count.
struct MetadataProfilesView: View {
    let client: ArrClient
    @State private var profiles: [JSONValue]?
    @State private var error: Error?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            ForEach(profiles ?? [], id: \.rowID) { p in
                NavigationLink {
                    MetadataProfileEditor(client: client, profile: p, isNew: false)
                } label: { Text(verbatim: p["name"].string ?? "") }
            }
            if profiles == nil && error == nil { LoadingView() }
        }
        .manageBackground()
        .navigationTitle(Text("manage.metadata_profiles"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                NavigationLink {
                    MetadataProfileTemplateLoader(client: client)
                } label: { Image(systemName: "plus") }
                .accessibilityLabel(Text("manage.add"))
            }
        }
        .onAppear { Task { await load() } }
    }

    private func load() async {
        do {
            profiles = try await client.get("metadataprofile").list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}

private struct MetadataProfileTemplateLoader: View {
    let client: ArrClient
    @State private var template: JSONValue?
    @State private var error: Error?

    var body: some View {
        Group {
            if let template {
                MetadataProfileEditor(client: client, profile: template, isNew: true)
            } else if let error {
                ArrErrorState(error: error) { await load() }
            } else {
                LoadingView()
            }
        }
        .task { await load() }
    }

    private func load() async {
        do { template = try await client.get("metadataprofile/schema") } catch is CancellationError {} catch { self.error = error }
    }
}

struct MetadataProfileEditor: View {
    let client: ArrClient
    let isNew: Bool
    @State private var profile: JSONValue
    @State private var error: Error?
    @Environment(\.dismiss) private var dismiss

    init(client: ArrClient, profile: JSONValue, isNew: Bool) {
        self.client = client
        self.isNew = isNew
        _profile = State(initialValue: profile)
    }

    var body: some View {
        Form {
            Section {
                TextField(text: Binding(get: { profile["name"].string ?? "" }, set: { profile["name"] = .string($0) })) { Text("manage.name") }
            }
            group("primaryAlbumTypes", "albumType", "manage.primary_types")
            group("secondaryAlbumTypes", "albumType", "manage.secondary_types")
            group("releaseStatuses", "releaseStatus", "manage.release_statuses")
            Section {
                ArrErrorLine(error: error)
                Button { Task { await save() } } label: { Text("save") }
                if !isNew { ConfirmDeleteButton { Task { await delete() } } }
            }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: profile["name"].string ?? ""))
        .navigationBarTitleDisplayMode(.inline)
    }

    private func group(_ key: String, _ inner: String, _ title: LocalizedStringKey) -> some View {
        Section {
            ForEach(profile[key].list.indices, id: \.self) { i in
                Toggle(isOn: Binding(get: { profile[key][i]["allowed"].bool ?? false }, set: { on in
                    var list = profile[key].list
                    list[i]["allowed"] = .bool(on)
                    profile[key] = .array(list)
                })) {
                    Text(verbatim: profile[key][i][inner]["name"].string ?? "")
                }
            }
        } header: { Text(title) }
    }

    private func save() async {
        do {
            if isNew {
                try await client.send("POST", "metadataprofile", json: profile)
            } else {
                try await client.send("PUT", "metadataprofile/\(profile["id"].text)", json: profile)
            }
            dismiss()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func delete() async {
        do {
            try await client.send("DELETE", "metadataprofile/\(profile["id"].text)")
            dismiss()
        } catch is CancellationError {} catch { self.error = error }
    }
}
