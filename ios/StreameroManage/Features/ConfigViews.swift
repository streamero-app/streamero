import SwiftUI

/// How a settings object is saved.
enum ConfigSaveMode: Sendable {
    /// Servarr `config/*`: PUT `{resource}/{id}` with the whole object.
    case putWithID
    /// PUT `{resource}` with the whole object.
    case put
    /// Seerr settings: POST `{resource}` with the whole object.
    case post
    /// Bazarr: POST `system/settings` with only the changed keys as form fields.
    case bazarrForm
}

/// A group of keys shown under one heading.
struct ConfigSection: Identifiable, Sendable {
    let titleKey: String
    let keys: [String]
    var id: String { titleKey }
}

/// Edits one settings object generically: every key by its JSON type, known enums as
/// pickers, secrets write-only, API validation errors next to the keys.
struct ConfigEditorView: View {
    let client: ArrClient
    let resource: String
    let titleKey: String
    var sections: [ConfigSection] = []
    var options: [String: [ArrSelectOption]] = [:]
    var saveMode: ConfigSaveMode = .putWithID
    /// Keys never shown (ids, read-only internals).
    var hiddenKeys: Set<String> = ["id"]
    /// Naming: live examples from `config/naming/examples`.
    var examplesResource: String? = nil

    @State private var state: ObjectFormState?
    @State private var tags: [JSONValue] = []
    @State private var failures: [ArrValidationFailure] = []
    @State private var error: Error?
    @State private var busy = false
    @State private var notice: ManageNotice?
    @State private var examples: JSONValue = .null

    var body: some View {
        Form {
            if let binding = Binding($state) {
                if examplesResource != nil, let o = examples.object, !o.isEmpty {
                    Section {
                        ForEach(o.keys.sorted(), id: \.self) { key in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: key.humanizedKey).font(.caption).foregroundStyle(Theme.textMuted)
                                Text(verbatim: o[key]?.text.nilIfBlank ?? "—").font(.footnote.monospaced()).foregroundStyle(Theme.text)
                            }
                        }
                    } header: { Text("manage.examples") }
                }
                ForEach(groups(binding.wrappedValue), id: \.title) { group in
                    Section {
                        ObjectFieldsView(state: binding, paths: group.paths, options: options, tags: tags, failures: failures)
                    } header: {
                        if let key = group.titleKey { Text(LocalizedStringKey(key)) } else { Text(verbatim: group.title) }
                    }
                }
                Section {
                    let general = failures.filter { f in f.propertyName.isEmpty || !allPaths(binding.wrappedValue).contains { f.matches(lastKey($0)) } }
                    ForEach(general, id: \.self) { f in
                        Text(verbatim: f.message).font(.footnote).foregroundStyle(f.isWarning ? .orange : .red)
                    }
                    ArrErrorLine(error: error)
                    Button { Task { await save() } } label: {
                        HStack { Text("save"); Spacer(); if busy { ProgressView() } }
                    }
                    .disabled(busy)
                }
            } else if let error {
                ArrErrorState(error: error) { await load() }
            } else {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text(LocalizedStringKey(titleKey)))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .task(id: state?.current) { await loadExamples() }
        .manageNotice($notice)
    }

    private struct KeyGroup {
        let title: String
        let titleKey: String?
        let paths: [String]
    }

    private func lastKey(_ path: String) -> String { path.split(separator: ".").last.map(String.init) ?? path }

    private func allPaths(_ s: ObjectFormState) -> [String] {
        s.original.flattened().map(\.path).filter { !hiddenKeys.contains($0) && !hiddenKeys.contains(lastKey($0)) }
    }

    /// The given sections first; other keys grouped by their parent object (or "Other").
    private func groups(_ s: ObjectFormState) -> [KeyGroup] {
        let paths = allPaths(s)
        var used = Set<String>()
        var out: [KeyGroup] = []
        for section in sections {
            let present = section.keys.filter { paths.contains($0) }
            used.formUnion(present)
            if !present.isEmpty { out.append(KeyGroup(title: section.titleKey, titleKey: section.titleKey, paths: present)) }
        }
        var byParent: [String: [String]] = [:]
        var order: [String] = []
        for path in paths where !used.contains(path) {
            let parent = path.contains(".") ? String(path[..<path.lastIndex(of: ".")!]) : ""
            if byParent[parent] == nil { order.append(parent) }
            byParent[parent, default: []].append(path)
        }
        for parent in order {
            if parent.isEmpty {
                out.append(KeyGroup(title: "", titleKey: sections.isEmpty ? "manage.settings" : "manage.other", paths: byParent[parent] ?? []))
            } else {
                out.append(KeyGroup(title: parent.split(separator: ".").map { String($0).humanizedKey }.joined(separator: " › "), titleKey: nil, paths: byParent[parent] ?? []))
            }
        }
        return out
    }

    private func load() async {
        do {
            let value = try await client.get(resource)
            state = ObjectFormState(value)
            error = nil
            if value.flattened().contains(where: { $0.path.hasSuffix("tags") }) {
                tags = (try? await client.get("tag").list) ?? []
            }
        } catch { self.error = error }
    }

    private func loadExamples() async {
        guard let examplesResource, let state else { return }
        try? await Task.sleep(for: .milliseconds(400))
        guard !Task.isCancelled else { return }
        let query = state.current.flattened().compactMap { item -> URLQueryItem? in
            switch item.value {
            case .string, .int, .double, .bool: return URLQueryItem(name: item.path, value: item.value.text)
            default: return nil
            }
        }
        if let result = try? await client.get(examplesResource, query: query) { examples = result }
    }

    private func save() async {
        guard let state else { return }
        busy = true
        defer { busy = false }
        do {
            switch saveMode {
            case .putWithID:
                let id = state.original["id"].text
                try await client.send("PUT", id.isEmpty ? resource : "\(resource)/\(id)", json: state.fullObject())
            case .put:
                try await client.send("PUT", resource, json: state.fullObject())
            case .post:
                try await client.send("POST", resource, json: state.fullObject())
            case .bazarrForm:
                try await client.sendForm("POST", resource, fields: BazarrSettings.formFields(state.changes()))
            }
            failures = []
            error = nil
            notice = ManageNotice(text: "saved")
            await load()
        } catch let e as ArrError where !e.failures.isEmpty {
            failures = e.failures
        } catch {
            self.error = error
        }
    }
}

/// Bazarr saves settings from form fields named `settings-{section}-{key}`.
enum BazarrSettings {
    static func formFields(_ changes: [JSONPathValue]) -> [(String, String)] {
        var out: [(String, String)] = []
        for change in changes {
            let name = "settings-" + change.path.replacingOccurrences(of: ".", with: "-")
            switch change.value {
            case .array(let items):
                if items.isEmpty { out.append((name, "[]")) }
                for item in items { out.append((name, item.text)) }
            case .null:
                out.append((name, ""))
            case .bool(let b):
                out.append((name, b ? "true" : "false"))
            default:
                out.append((name, change.value.text))
            }
        }
        return out
    }
}

// MARK: Generic lists of objects

/// A list resource edited as plain objects (remote path mappings, delay and release
/// profiles, Prowlarr app profiles, Seerr service connections).
struct ObjectListView: View {
    let client: ArrClient
    let resource: String
    let titleKey: String
    /// Keys whose values make the row title (first non-empty).
    var titleKeys: [String] = ["name", "label", "title"]
    var subtitleKeys: [String] = []
    /// What a new entry starts as; nil when the resource cannot be added here.
    var template: JSONValue? = nil
    var options: [String: [ArrSelectOption]] = [:]
    /// Seerr updates with PUT `{resource}/{id}` too; Servarr the same.
    var canDelete = true
    @State private var items: [JSONValue]?
    @State private var error: Error?
    @State private var pendingDelete: JSONValue?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let items {
                ForEach(items, id: \.rowID) { item in
                    NavigationLink {
                        ObjectEditorView(client: client, resource: resource, item: item, isNew: false, options: options, canDelete: canDelete)
                    } label: {
                        VStack(alignment: .leading, spacing: 3) {
                            Text(verbatim: title(item)).font(.body.weight(.semibold))
                            let sub = subtitleKeys.map { item.value(at: $0).text }.filter { !$0.isEmpty }.joined(separator: " → ")
                            if !sub.isEmpty { Text(verbatim: sub).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(2) }
                        }
                    }
                    .swipeActions {
                        if canDelete {
                            Button(role: .destructive) { pendingDelete = item } label: { Label("manage.delete", systemImage: "trash") }
                        }
                    }
                }
                if items.isEmpty { EmptyRow(text: "manage.empty") }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text(LocalizedStringKey(titleKey)))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let template {
                ToolbarItem(placement: .primaryAction) {
                    NavigationLink {
                        ObjectEditorView(client: client, resource: resource, item: template, isNew: true, options: options, canDelete: false)
                    } label: { Image(systemName: "plus") }
                    .accessibilityLabel(Text("manage.add"))
                }
            }
        }
        .onAppear { Task { await load() } }
        .refreshable { await load() }
        .confirmationDialog("manage.delete_confirm", isPresented: Binding(get: { pendingDelete != nil }, set: { if !$0 { pendingDelete = nil } }), titleVisibility: .visible) {
            Button("manage.delete", role: .destructive) {
                if let item = pendingDelete { Task { await delete(item) } }
            }
        }
    }

    private func title(_ item: JSONValue) -> String {
        for key in titleKeys {
            let v = item.value(at: key).text
            if !v.isEmpty { return v }
        }
        return "#" + item["id"].text
    }

    private func load() async {
        do {
            let value = try await client.get(resource)
            items = value.array ?? value["results"].array ?? []
            error = nil
        } catch { self.error = error }
    }

    private func delete(_ item: JSONValue) async {
        do {
            try await client.send("DELETE", "\(resource)/\(item["id"].text)")
            await load()
        } catch { self.error = error }
    }
}

/// Edits one object of a list resource.
struct ObjectEditorView: View {
    let client: ArrClient
    let resource: String
    let isNew: Bool
    var options: [String: [ArrSelectOption]] = [:]
    var canDelete = true
    @State private var state: ObjectFormState
    @State private var tags: [JSONValue] = []
    @State private var failures: [ArrValidationFailure] = []
    @State private var error: Error?
    @State private var busy = false
    @Environment(\.dismiss) private var dismiss

    init(client: ArrClient, resource: String, item: JSONValue, isNew: Bool, options: [String: [ArrSelectOption]] = [:], canDelete: Bool = true) {
        self.client = client
        self.resource = resource
        self.isNew = isNew
        self.options = options
        self.canDelete = canDelete
        _state = State(initialValue: ObjectFormState(item))
    }

    private var paths: [String] {
        state.original.flattened().map(\.path).filter { $0 != "id" }
    }

    var body: some View {
        Form {
            Section {
                ObjectFieldsView(state: $state, paths: paths, options: options, tags: tags, failures: failures)
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
                if !isNew && canDelete {
                    Button("manage.delete", role: .destructive) { Task { await delete() } }
                }
            }
        }
        .manageBackground()
        .navigationTitle(Text(isNew ? LocalizedStringKey("manage.add") : LocalizedStringKey("manage.edit")))
        .navigationBarTitleDisplayMode(.inline)
        .task {
            if paths.contains(where: { $0.hasSuffix("tags") }), client.service.isServarr {
                tags = (try? await client.get("tag").list) ?? []
            }
        }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        do {
            if isNew {
                try await client.send("POST", resource, json: state.fullObject())
            } else {
                try await client.send("PUT", "\(resource)/\(state.original["id"].text)", json: state.fullObject())
            }
            dismiss()
        } catch let e as ArrError where !e.failures.isEmpty {
            failures = e.failures
        } catch { self.error = error }
    }

    private func delete() async {
        do {
            try await client.send("DELETE", "\(resource)/\(state.original["id"].text)")
            dismiss()
        } catch { self.error = error }
    }
}
