import SwiftUI

/// A Servarr provider resource with a `/schema` endpoint: indexers, download clients,
/// import lists, notifications (connections), metadata, Prowlarr applications and proxies.
struct ProviderKind: Hashable, Sendable {
    let resource: String
    /// Localization key of the screen title.
    let titleKey: String
    let icon: String
    var canTestAll = false

    var title: LocalizedStringKey { LocalizedStringKey(titleKey) }

    static let indexers = ProviderKind(resource: "indexer", titleKey: "manage.indexers", icon: "magnifyingglass.circle", canTestAll: true)
    static let downloadClients = ProviderKind(resource: "downloadclient", titleKey: "manage.download_clients", icon: "arrow.down.circle", canTestAll: true)
    static let importLists = ProviderKind(resource: "importlist", titleKey: "manage.import_lists", icon: "list.bullet.rectangle", canTestAll: true)
    static let notifications = ProviderKind(resource: "notification", titleKey: "manage.connections", icon: "bell.badge")
    static let metadata = ProviderKind(resource: "metadata", titleKey: "manage.metadata", icon: "doc.text")
    static let applications = ProviderKind(resource: "applications", titleKey: "manage.apps", icon: "app.connected.to.app.below.fill", canTestAll: true)
    static let indexerProxies = ProviderKind(resource: "indexerproxy", titleKey: "manage.indexer_proxies", icon: "network")
}

/// Every configured provider of one kind.
struct ProviderListView: View {
    let client: ArrClient
    let kind: ProviderKind
    @State private var items: [JSONValue]?
    @State private var error: Error?
    @State private var pendingDelete: JSONValue?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { Section { ArrErrorLine(error: error) } }
            if let items {
                Section {
                    ForEach(items, id: \.rowID) { item in
                        NavigationLink {
                            ProviderEditorView(client: client, kind: kind, item: item, isNew: false)
                        } label: {
                            ProviderRow(item: item)
                        }
                        .swipeActions {
                            Button(role: .destructive) { pendingDelete = item } label: { Label("manage.delete", systemImage: "trash") }
                        }
                    }
                    if items.isEmpty { EmptyRow(text: "manage.empty") }
                }
                if kind.canTestAll, !items.isEmpty {
                    Section {
                        Button { Task { await testAll() } } label: { Label("manage.test_all", systemImage: "checkmark.seal") }
                    }
                }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text(kind.title))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                NavigationLink {
                    ProviderTemplatePicker(client: client, kind: kind)
                } label: {
                    Image(systemName: "plus")
                }
                .accessibilityLabel(Text("manage.add"))
            }
        }
        .onAppear { Task { await load() } }
        .refreshable { await load() }
        .confirmationDialog("manage.delete_confirm", isPresented: Binding(get: { pendingDelete != nil }, set: { if !$0 { pendingDelete = nil } }), titleVisibility: .visible) {
            Button("manage.delete", role: .destructive) {
                if let item = pendingDelete { Task { await delete(item) } }
            }
        } message: {
            Text(verbatim: pendingDelete?["name"].string ?? "")
        }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            items = try await client.get(kind.resource).list.sorted { ($0["name"].string ?? "").localizedCaseInsensitiveCompare($1["name"].string ?? "") == .orderedAscending }
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func delete(_ item: JSONValue) async {
        do {
            try await client.send("DELETE", "\(kind.resource)/\(item["id"].text)")
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func testAll() async {
        do {
            let result = try await client.send("POST", "\(kind.resource)/testall")
            let failed = result.list.filter { $0["isValid"].bool == false }
            notice = failed.isEmpty ? ManageNotice(text: "manage.test_ok") : ManageNotice(text: "manage.test_some_failed", failed: true)
        } catch {
            notice = ManageNotice(text: "manage.test_failed", failed: true)
            self.error = error
        }
    }
}

struct ProviderRow: View {
    let item: JSONValue

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: item["name"].string ?? "—").font(.body.weight(.semibold))
                Text(verbatim: item["implementationName"].string ?? item["implementation"].string ?? "")
                    .font(.caption).foregroundStyle(Theme.textMuted)
            }
            Spacer()
            if Self.enabled(item) {
                TagBadge(text: "manage.enabled", tone: .success)
            } else {
                TagBadge(text: "manage.disabled")
            }
        }
    }

    /// Any of the `enable*` switches on means the provider is in use.
    static func enabled(_ item: JSONValue) -> Bool {
        guard let o = item.object else { return false }
        let flags = o.filter { $0.key.hasPrefix("enable") || $0.key.hasPrefix("on") }.compactMap { $0.value.bool }
        return flags.isEmpty ? true : flags.contains(true)
    }
}

/// Choose what to add: the implementations (and their presets) from `/{resource}/schema`.
struct ProviderTemplatePicker: View {
    let client: ArrClient
    let kind: ProviderKind
    @State private var templates: [JSONValue]?
    @State private var error: Error?
    @State private var query = ""

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let templates {
                ForEach(filtered(templates), id: \.self) { template in
                    let presets = template["presets"].list
                    Section {
                        NavigationLink {
                            ProviderEditorView(client: client, kind: kind, item: Self.prepared(template), isNew: true)
                        } label: {
                            VStack(alignment: .leading, spacing: 3) {
                                Text(verbatim: template["implementationName"].string ?? template["implementation"].string ?? "")
                                    .font(.body.weight(.semibold))
                                if let p = template["protocol"].string { Text(verbatim: p).font(.caption).foregroundStyle(Theme.textFaint) }
                                if let d = template["description"].string?.nilIfBlank {
                                    Text(verbatim: d).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(2)
                                }
                            }
                        }
                        ForEach(presets, id: \.self) { preset in
                            NavigationLink {
                                ProviderEditorView(client: client, kind: kind, item: Self.prepared(preset, base: template), isNew: true)
                            } label: {
                                Label { Text(verbatim: preset["name"].string ?? "") } icon: { Image(systemName: "arrow.turn.down.right") }
                                    .font(.subheadline)
                            }
                        }
                    }
                }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .searchable(text: $query)
        .navigationTitle(Text("manage.add"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func filtered(_ all: [JSONValue]) -> [JSONValue] {
        let sorted = all.sorted {
            ($0["implementationName"].string ?? "").localizedCaseInsensitiveCompare($1["implementationName"].string ?? "") == .orderedAscending
        }
        guard !query.isEmpty else { return sorted }
        return sorted.filter {
            ($0["implementationName"].string ?? "").localizedCaseInsensitiveContains(query)
                || ($0["name"].string ?? "").localizedCaseInsensitiveContains(query)
                || $0["presets"].list.contains { ($0["name"].string ?? "").localizedCaseInsensitiveContains(query) }
        }
    }

    /// A new provider from a template: named after the implementation, without the presets list.
    static func prepared(_ template: JSONValue, base: JSONValue? = nil) -> JSONValue {
        var item = template
        if let base {
            // Presets carry only what differs; the rest comes from the implementation.
            for key in base.object?.keys.sorted() ?? [] where item[key].isNull && key != "presets" { item[key] = base[key] }
        }
        item["presets"] = .null
        if (item["name"].string ?? "").isEmpty {
            item["name"] = .string(item["implementationName"].string ?? item["implementation"].string ?? "")
        }
        if var o = item.object {
            o.removeValue(forKey: "presets")
            o.removeValue(forKey: "id")
            item = .object(o)
        }
        return item
    }

    private func load() async {
        do {
            templates = try await client.get("\(kind.resource)/schema").list
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// Enum choices for top-level provider keys (import lists).
private let providerOptions: [String: [ArrSelectOption]] = [
    "minimumAvailability": apiOptions(["tba", "announced", "inCinemas", "released"]),
    "seriesType": apiOptions(["standard", "daily", "anime"]),
    "monitorNewItems": apiOptions(["all", "none", "new"]),
]

/// Edits one provider: name, switches, the schema fields, tags; test, save and delete.
struct ProviderEditorView: View {
    let client: ArrClient
    let kind: ProviderKind
    let isNew: Bool
    @State private var resource: JSONValue
    @State private var form: SchemaFormState
    @State private var tags: [JSONValue] = []
    @State private var lookups: [String: [ArrSelectOption]] = [:]
    @State private var failures: [ArrValidationFailure] = []
    @State private var error: Error?
    @State private var busy = false
    @State private var showAdvanced = false
    @State private var notice: ManageNotice?
    @State private var confirmDelete = false
    @Environment(\.dismiss) private var dismiss

    init(client: ArrClient, kind: ProviderKind, item: JSONValue, isNew: Bool) {
        self.client = client
        self.kind = kind
        self.isNew = isNew
        _resource = State(initialValue: item)
        _form = State(initialValue: SchemaFormState(json: item["fields"]))
    }

    /// Top-level switches: `enable…`, `on…` (only those the provider supports) and the rest.
    private var flagKeys: [String] {
        guard let o = resource.object else { return [] }
        return o.keys.filter { key in
            guard o[key]?.bool != nil, !key.hasPrefix("supports"), key != "isNew" else { return false }
            if key.hasPrefix("on") {
                let supports = "supports" + key.prefix(1).uppercased() + key.dropFirst()
                if let s = o[supports]?.bool { return s }
            }
            return true
        }.sorted { a, b in
            // "enable" first, then alphabetical.
            (a == "enable" ? 0 : 1, a) < (b == "enable" ? 0 : 1, b)
        }
    }

    /// Top-level choices that reference other resources or enums.
    private var choiceKeys: [String] {
        ["qualityProfileId", "metadataProfileId", "rootFolderPath", "appProfileId", "minimumAvailability", "seriesType",
         "monitorNewItems", "downloadClientId"].filter { !resource[$0].isNull }
    }

    var body: some View {
        Form {
            Section {
                VStack(alignment: .leading, spacing: 4) {
                    Text("manage.name").font(.caption).foregroundStyle(Theme.textMuted)
                    TextField(text: Binding(get: { resource["name"].string ?? "" }, set: { resource["name"] = .string($0) })) { Text("manage.name") }
                        .autocorrectionDisabled()
                }
                if let impl = resource["implementationName"].string {
                    InfoRow(title: "manage.type", value: impl)
                }
                if let link = resource["infoLink"].string.flatMap(URL.init(string:)) {
                    Link(destination: link) { Label("manage.more_info", systemImage: "info.circle") }
                }
                ForEach(failures.filter { $0.matches("name") }, id: \.self) { f in
                    Text(verbatim: f.message).font(.caption).foregroundStyle(.red)
                }
            }
            if !flagKeys.isEmpty {
                Section {
                    ForEach(flagKeys, id: \.self) { key in
                        Toggle(isOn: Binding(get: { resource[key].bool ?? false }, set: { resource[key] = .bool($0) })) {
                            Text(verbatim: key.humanizedKey)
                        }
                    }
                }
            }
            if !resource["priority"].isNull || !choiceKeys.isEmpty {
                Section {
                    if !resource["priority"].isNull {
                        LabeledContent {
                            JSONNumberField(value: Binding(get: { resource["priority"] }, set: { resource["priority"] = $0 }))
                        } label: { Text("manage.priority") }
                    }
                    ForEach(choiceKeys, id: \.self) { key in
                        let options = lookups[key] ?? providerOptions[key] ?? []
                        Picker(selection: Binding(get: { resource[key] }, set: { resource[key] = $0 })) {
                            if !options.contains(where: { $0.value == resource[key] }) {
                                Text(verbatim: resource[key].text.isEmpty ? "—" : resource[key].text).tag(resource[key])
                            }
                            ForEach(options) { o in Text(verbatim: o.name).tag(o.value) }
                        } label: { Text(verbatim: key.humanizedKey) }
                    }
                }
            }
            Section {
                SchemaFieldsView(state: $form, tags: tags, failures: failures, showAdvanced: showAdvanced,
                                 loadOptions: { field in await loadOptions(field) })
                if form.fields.contains(where: \.advanced) {
                    Toggle("manage.show_advanced", isOn: $showAdvanced.animation())
                }
            } header: { Text("manage.settings") }
            if !resource["tags"].isNull {
                Section {
                    MultiSelectRow(title: Loc.string("manage.tags"), options: tagOptions(tags),
                                   selection: Binding(get: { resource["tags"].list }, set: { resource["tags"] = .array($0) })) {
                        Text("manage.tags")
                    }
                }
            }
            Section {
                // A failure on a field that is not on screen (advanced while hidden, or hidden by
                // the schema) is listed here, so Save never fails without a word.
                let shown = form.fields.filter { $0.isVisible && (showAdvanced || !$0.advanced) }
                let general = failures.filter { f in
                    f.propertyName.isEmpty || !(shown.contains { f.matches($0.name) } || f.matches("name"))
                }
                ForEach(general, id: \.self) { f in
                    Text(verbatim: f.message).font(.footnote).foregroundStyle(f.isWarning ? .orange : .red)
                }
                ArrErrorLine(error: error)
                Button { Task { await test() } } label: { Label("manage.test", systemImage: "checkmark.seal") }
                    .disabled(busy)
                Button { Task { await save(force: false) } } label: {
                    HStack { Text("save"); Spacer(); if busy { ProgressView() } }
                }
                .disabled(busy)
                if !failures.isEmpty, failures.allSatisfy(\.isWarning) {
                    Button { Task { await save(force: true) } } label: { Text("manage.save_anyway") }
                }
                if !isNew {
                    Button("manage.delete", role: .destructive) { confirmDelete = true }
                }
            }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: resource["name"].string?.nilIfBlank ?? resource["implementationName"].string ?? ""))
        .navigationBarTitleDisplayMode(.inline)
        .task { await loadLookups() }
        .confirmationDialog("manage.delete_confirm", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("manage.delete", role: .destructive) { Task { await delete() } }
        }
        .manageNotice($notice)
    }

    private var body_: JSONValue {
        var body = resource
        body["fields"] = form.outgoingFields()
        return body
    }

    private func loadLookups() async {
        if !resource["tags"].isNull { tags = (try? await client.get("tag").list) ?? [] }
        if !resource["qualityProfileId"].isNull {
            lookups["qualityProfileId"] = idOptions((try? await client.get("qualityprofile").list) ?? [])
        }
        if !resource["metadataProfileId"].isNull {
            lookups["metadataProfileId"] = idOptions((try? await client.get("metadataprofile").list) ?? [])
        }
        if !resource["appProfileId"].isNull {
            lookups["appProfileId"] = idOptions((try? await client.get("appprofile").list) ?? [])
        }
        if !resource["downloadClientId"].isNull {
            lookups["downloadClientId"] = [ArrSelectOption(value: .int(0), name: "—", hint: nil)]
                + idOptions((try? await client.get("downloadclient").list) ?? [])
        }
        if !resource["rootFolderPath"].isNull {
            let folders = (try? await client.get("rootfolder").list) ?? []
            lookups["rootFolderPath"] = folders.compactMap { f in
                f["path"].string.map { ArrSelectOption(value: .string($0), name: $0, hint: nil) }
            }
        }
    }

    private func loadOptions(_ field: ArrField) async -> [ArrSelectOption] {
        guard let action = field.optionsAction else { return [] }
        let result = try? await client.send("POST", "\(kind.resource)/action/\(action)", json: body_)
        return (result?["options"].list ?? []).map {
            ArrSelectOption(value: $0["value"], name: $0["name"].string ?? $0["value"].text, hint: $0["hint"].string)
        }
    }

    private func test() async {
        busy = true
        defer { busy = false }
        do {
            try await client.send("POST", "\(kind.resource)/test", json: body_)
            failures = []
            error = nil
            notice = ManageNotice(text: "manage.test_ok")
        } catch let e as ArrError where !e.failures.isEmpty {
            failures = e.failures
            notice = ManageNotice(text: "manage.test_failed", failed: true)
        } catch {
            self.error = error
            notice = ManageNotice(text: "manage.test_failed", failed: true)
        }
    }

    private func save(force: Bool) async {
        busy = true
        defer { busy = false }
        let query = force ? [URLQueryItem.q("forceSave", "true")] : []
        do {
            if isNew {
                try await client.send("POST", kind.resource, query: query, json: body_)
            } else {
                try await client.send("PUT", "\(kind.resource)/\(resource["id"].text)", query: query, json: body_)
            }
            dismiss()
        } catch let e as ArrError where !e.failures.isEmpty {
            failures = e.failures
            error = nil
        } catch {
            self.error = error
        }
    }

    private func delete() async {
        do {
            try await client.send("DELETE", "\(kind.resource)/\(resource["id"].text)")
            dismiss()
        } catch is CancellationError {} catch { self.error = error }
    }
}
