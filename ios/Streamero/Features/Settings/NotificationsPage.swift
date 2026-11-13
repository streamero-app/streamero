import SwiftUI

/// Discord, ntfy, Gotify and webhook targets for server events (admin). Each save sends the
/// whole list, as the API replaces it; tokens are write-only and an empty one keeps the stored token.
struct NotificationsPage: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var targets: [NotificationTarget]?
    @State private var error: Error?
    @State private var editing: NotificationTarget?
    @State private var deleting: NotificationTarget?

    var body: some View {
        AdminGuarded {
            List {
                Section { IntroText(text: "notifications.intro") }
                if let error { Section { ErrorLine(error: error) } }
                if targets == nil && error == nil { LoadingView().listRowBackground(Color.clear) }
                if let targets {
                    Section {
                        if targets.isEmpty { Text("notifications.none").foregroundStyle(Theme.textMuted) }
                        ForEach(targets) { t in
                            Button { editing = t } label: { row(t) }
                                .swipeActions { Button("delete", role: .destructive) { deleting = t } }
                                .contextMenu {
                                    Button("common.edit", systemImage: "pencil") { editing = t }
                                    Button("delete", systemImage: "trash", role: .destructive) { deleting = t }
                                }
                        }
                    }
                    if targets.count < NotificationTarget.maxTargets {
                        Section {
                            Button { editing = .new() } label: { Label("notifications.add", systemImage: "plus") }
                        }
                    }
                }
            }
            .sheet(item: $editing) { target in
                NotificationEditor(client: client, target: target, isNew: !(targets ?? []).contains { $0.id == target.id }) { saved in
                    try await save(replacing: saved)
                }
                .environment(\.locale, app.locale ?? Locale.current)
            }
            .confirmationDialog(Text(verbatim: deleting.map { String(format: Loc.string("notifications.confirmDelete %@"), $0.name) } ?? ""),
                                isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                                titleVisibility: .visible, presenting: deleting) { t in
                Button("delete", role: .destructive) {
                    Task {
                        do {
                            try await store((targets ?? []).filter { $0.id != t.id })
                        } catch { self.error = error }
                    }
                }
            }
            .refreshable { await load() }
            .task { await load() }
        }
    }

    private func row(_ t: NotificationTarget) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Text(verbatim: t.name).font(.body.weight(.semibold)).foregroundStyle(Theme.text)
                TagBadge(text: kindLabel(t.kind))
                if !t.enabled { TagBadge(text: "sso.disabled") }
            }
            Text(verbatim: t.url).font(.footnote).foregroundStyle(Theme.textMuted).lineLimit(1).truncationMode(.middle)
            Text(verbatim: t.events.filter { $0 != "test" }.map { Loc.string(eventKey($0)) }.joined(separator: " · "))
                .font(.caption).foregroundStyle(Theme.textFaint)
        }
        .padding(.vertical, 2)
    }

    private func load() async {
        do {
            targets = try await client.notificationSettings().targets
            error = nil
        } catch is CancellationError { return } catch { self.error = error }
    }

    private func save(replacing target: NotificationTarget) async throws {
        var list = targets ?? []
        if let i = list.firstIndex(where: { $0.id == target.id }) { list[i] = target } else { list.append(target) }
        try await store(list)
    }

    private func store(_ list: [NotificationTarget]) async throws {
        let saved = try await client.saveNotificationSettings(NotificationSettings(targets: list.map(\.forSaving)))
        targets = saved.targets
        error = nil
    }
}

func kindLabel(_ kind: String) -> LocalizedStringKey {
    switch kind {
    case "discord": "notifications.kind_discord"
    case "ntfy": "notifications.kind_ntfy"
    case "gotify": "notifications.kind_gotify"
    default: "notifications.kind_generic"
    }
}

/// The localization key of a notification event (built in code, so a String).
func eventKey(_ event: String) -> String {
    switch event {
    case "item.added": "notifications.event_item.added"
    case "playback.started": "notifications.event_playback.started"
    case "login.succeeded": "notifications.event_login.succeeded"
    case "login.failed": "notifications.event_login.failed"
    default: "notifications.event_scan.failed"
    }
}

private struct NotificationEditor: View {
    let client: MediaClient
    let isNew: Bool
    let onSave: @MainActor (NotificationTarget) async throws -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var target: NotificationTarget
    @State private var busy = false
    @State private var testing = false
    @State private var tested = false
    @State private var error: Error?

    init(client: MediaClient, target: NotificationTarget, isNew: Bool, onSave: @escaping @MainActor (NotificationTarget) async throws -> Void) {
        self.client = client
        self.isNew = isNew
        self.onSave = onSave
        _target = State(initialValue: target)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField(text: $target.name, prompt: Text("notifications.namePlaceholder")) { Text("notifications.name") }
                    Picker(selection: $target.kind) {
                        ForEach(NotificationTarget.kinds, id: \.self) { k in Text(kindLabel(k)).tag(k) }
                    } label: { Text("notifications.kind") }
                }
                Section {
                    TextField(text: $target.url, prompt: Text(urlHint)) { Text("notifications.url") }
                        .keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
                    if target.kind != "discord" {
                        SecureField(text: Binding(get: { target.token ?? "" }, set: { target.token = $0 }),
                                    prompt: target.hasToken == true ? Text("notifications.tokenStored") : nil) {
                            Text(target.kind == "gotify" ? LocalizedStringKey("notifications.appToken") : LocalizedStringKey("notifications.token"))
                        }
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                    }
                } footer: {
                    Text(help)
                }
                Section {
                    ForEach(NotificationTarget.eventKinds, id: \.self) { ev in
                        Toggle(LocalizedStringKey(eventKey(ev)), isOn: Binding(
                            get: { target.events.contains(ev) },
                            set: { on in target.events = on ? target.events + [ev] : target.events.filter { $0 != ev } }))
                    }
                } header: { Text("notifications.events") }
                Section {
                    Toggle("notifications.enabled", isOn: $target.enabled)
                }
                Section {
                    Button { Task { await test() } } label: {
                        HStack {
                            Label("notifications.test", systemImage: "paperplane")
                            if testing { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(testing || !target.isValid)
                    if tested { Text("notifications.testSent").foregroundStyle(.green) }
                    ErrorLine(error: error)
                }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background)
            .navigationTitle(Text(isNew ? LocalizedStringKey("notifications.add") : LocalizedStringKey("common.edit")))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if busy { ProgressView() } else {
                        Button("save") { Task { await save() } }.disabled(!target.isValid)
                    }
                }
            }
        }
        .presentationBackground(Theme.background)
    }

    private var urlHint: LocalizedStringKey {
        switch target.kind {
        case "discord": "notifications.urlHint_discord"
        case "ntfy": "notifications.urlHint_ntfy"
        case "gotify": "notifications.urlHint_gotify"
        default: "notifications.urlHint_generic"
        }
    }

    private var help: LocalizedStringKey {
        switch target.kind {
        case "discord": "notifications.help_discord"
        case "ntfy": "notifications.help_ntfy"
        case "gotify": "notifications.help_gotify"
        default: "notifications.help_generic"
        }
    }

    private func test() async {
        testing = true
        tested = false
        defer { testing = false }
        do {
            try await client.testNotification(target.forSaving)
            tested = true
            error = nil
        } catch { self.error = error }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        do {
            try await onSave(target)
            dismiss()
        } catch { self.error = error }
    }
}
