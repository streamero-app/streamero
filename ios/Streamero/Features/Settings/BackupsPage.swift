import SwiftUI

/// Database backups (admin): create now, download (share sheet), restore with the name typed
/// as confirmation, delete, and the daily schedule. Uploading an archive stays on the web.
struct BackupsPage: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var overview: BackupOverview?
    @State private var error: Error?
    @State private var queuedDuringScan = false
    @State private var creating = false
    @State private var downloading: String?
    @State private var shared: SharedFile?
    @State private var restoring: Backup?
    @State private var deleting: Backup?
    @State private var schedule: BackupSettings?
    @State private var scheduleSaved = false

    var body: some View {
        AdminGuarded {
            List {
                Section {
                    IntroText(text: "backups.intro")
                    if let overview {
                        Text("backups.folder \(overview.dir)").font(.caption).foregroundStyle(Theme.textFaint).listRowBackground(Color.clear)
                    }
                }
                Section {
                    Button { Task { await create() } } label: {
                        HStack {
                            Label(overview?.running == true ? LocalizedStringKey("backups.running") : LocalizedStringKey("backups.now"),
                                  systemImage: "externaldrive.badge.plus")
                            if creating || overview?.running == true { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(overview == nil || overview?.isBusy == true || creating)
                    if overview?.scanRunning == true || queuedDuringScan {
                        Label("backups.scanWarning", systemImage: "exclamationmark.triangle")
                            .font(.footnote).foregroundStyle(Theme.accent)
                    }
                }
                if let restore = overview?.lastRestore { restoreStatus(restore) }
                if let error { Section { ErrorLine(error: error) } }
                if overview == nil && error == nil { LoadingView().listRowBackground(Color.clear) }
                if let overview {
                    Section {
                        if overview.backups.isEmpty { Text("backups.none").foregroundStyle(Theme.textMuted) }
                        ForEach(overview.backups) { b in row(b, busy: overview.isBusy) }
                    }
                }
                if schedule != nil { scheduleSection }
            }
            .sheet(item: $shared) { file in ShareSheet(items: [file.url]).presentationDetents([.medium, .large]) }
            .sheet(item: $restoring) { b in
                RestoreSheet(backup: b, client: client) { Task { await load() } }
                    .environment(\.locale, app.locale ?? Locale.current)
            }
            .confirmationDialog(Text("backups.deleteConfirm"),
                                isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                                titleVisibility: .visible, presenting: deleting) { b in
                Button("backups.delete", role: .destructive) {
                    Task {
                        do {
                            try await client.deleteBackup(b.name)
                            await load()
                        } catch { self.error = error }
                    }
                }
            }
            .refreshable { await load() }
            .task {
                // Poll while a backup is written or a restore runs (as the web page does).
                while !Task.isCancelled {
                    await load()
                    try? await Task.sleep(for: .seconds(overview?.isBusy == true ? 3 : 30))
                }
            }
        }
    }

    private func row(_ b: Backup, busy: Bool) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(verbatim: shortTime(b.createdAt)).font(.body.weight(.semibold))
                TagBadge(text: kindKey(b.kind))
                if b.includesImages { TagBadge(text: "backups.withArtwork") }
                if let p = b.problem { TagBadge(text: problemKey(p), tone: .danger) }
            }
            Text(verbatim: details(b)).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(2)
            Text(verbatim: b.name).font(.caption2.monospaced()).foregroundStyle(Theme.textFaint).lineLimit(1).truncationMode(.middle)
            HStack(spacing: 12) {
                Button { Task { await download(b) } } label: {
                    if downloading == b.name { ProgressView() } else { Label("backups.download", systemImage: "square.and.arrow.down") }
                }
                .disabled(downloading != nil)
                Button { restoring = b } label: { Label("backups.restore", systemImage: "clock.arrow.circlepath") }
                    .disabled(!b.restorable || busy)
                Spacer()
                Button(role: .destructive) { deleting = b } label: { Image(systemName: "trash") }
                    .accessibilityLabel(Text("backups.delete"))
            }
            .buttonStyle(.borderless)
            .font(.subheadline)
        }
        .padding(.vertical, 4)
    }

    private func details(_ b: Backup) -> String {
        var parts = [byteSize(b.sizeBytes)]
        if !b.serverVersion.isEmpty { parts.append(b.serverVersion) }
        if b.scanRunning { parts.append(Loc.string("backups.duringScan")) }
        if let t = b.skippedTables, !t.isEmpty { parts.append(String(format: Loc.string("backups.skipped %@"), t.joined(separator: ", "))) }
        return parts.joined(separator: " · ")
    }

    private func kindKey(_ kind: String) -> LocalizedStringKey {
        switch kind {
        case "scheduled": "backups.kind_scheduled"
        case "pre_restore": "backups.kind_pre_restore"
        case "uploaded": "backups.kind_uploaded"
        default: "backups.kind_manual"
        }
    }

    private func problemKey(_ problem: String) -> LocalizedStringKey {
        problem == "newer_schema" ? "backups.problem_newer_schema" : "backups.problem_unreadable"
    }

    private func restoreStatus(_ r: RestoreStatus) -> some View {
        let when = shortTime(r.finishedAt ?? r.startedAt)
        return Section {
            Group {
                switch r.state {
                case "running": Text("backups.restore_running \(r.backup)")
                case "succeeded": Text("backups.restore_succeeded \(r.backup) \(when)")
                default: Text("backups.restore_failed \(r.backup) \(when)").foregroundStyle(.red)
                }
            }
            .font(.subheadline)
            if let e = r.error { Text(verbatim: e).font(.caption).foregroundStyle(.red) }
            if let s = r.safetyBackup { Text("backups.safetyBackup \(s)").font(.caption).foregroundStyle(Theme.textMuted) }
            ForEach(r.warnings ?? [], id: \.self) { w in Text(verbatim: w).font(.caption).foregroundStyle(Theme.textMuted) }
        }
    }

    private var scheduleSection: some View {
        let binding = Binding(get: { schedule ?? BackupSettings(enabled: false, keepLast: 7, includeImages: false) },
                              set: { schedule = $0; scheduleSaved = false })
        return Section {
            Toggle(isOn: binding.enabled) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("backups.enabled")
                    FieldHint(text: "backups.enabledHint")
                }
            }
            Stepper(value: binding.keepLast, in: 1...100) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("backups.keepLast") + Text(verbatim: ": \(binding.wrappedValue.keepLast)")
                    FieldHint(text: "backups.keepLastHint")
                }
            }
            Toggle(isOn: binding.includeImages) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("backups.includeImages")
                    FieldHint(text: "backups.includeImagesHint")
                }
            }
            Button("save") { Task { await saveSchedule() } }
            if scheduleSaved { Text("saved").foregroundStyle(.green) }
        } header: {
            Text("backups.scheduleTitle")
        }
    }

    // MARK: Actions

    private func load() async {
        do {
            let o = try await client.backups()
            overview = o
            if schedule == nil { schedule = o.settings }
            error = nil
        } catch is CancellationError { return } catch { self.error = error }
    }

    private func create() async {
        creating = true
        defer { creating = false }
        do {
            queuedDuringScan = try await client.createBackup().scanRunning
            await load()
        } catch { self.error = error }
    }

    private func download(_ b: Backup) async {
        downloading = b.name
        defer { downloading = nil }
        do {
            shared = SharedFile(url: try await client.downloadBackup(b.name))
            error = nil
        } catch { self.error = error }
    }

    private func saveSchedule() async {
        guard let schedule else { return }
        do {
            self.schedule = try await client.saveBackupSettings(schedule)
            scheduleSaved = true
            error = nil
        } catch { self.error = error }
    }
}

/// Replaces everything with a backup; asks for the backup's exact name first.
private struct RestoreSheet: View {
    let backup: Backup
    let client: MediaClient
    let onStarted: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var typed = ""
    @State private var busy = false
    @State private var error: Error?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Label("backups.restoreWarning", systemImage: "exclamationmark.triangle.fill")
                        .font(.subheadline).foregroundStyle(Theme.text)
                        .symbolRenderingMode(.multicolor)
                }
                Section {
                    TextField(text: $typed) { Text(verbatim: backup.name) }
                        .font(.callout.monospaced())
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                } header: {
                    Text("backups.typeName \(backup.name)").textCase(nil)
                }
                Section {
                    Button(role: .destructive) { Task { await restore() } } label: {
                        HStack {
                            Text("backups.restoreConfirm").bold()
                            if busy { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(!backup.confirms(typed) || busy)
                    ErrorLine(error: error)
                }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background)
            .navigationTitle(Text("backups.restore"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } } }
        }
        .presentationBackground(Theme.background)
    }

    private func restore() async {
        busy = true
        defer { busy = false }
        do {
            try await client.restoreBackup(backup.name, confirm: typed)
            onStarted()
            dismiss()
        } catch { self.error = error }
    }
}
