import SwiftUI

/// Status, health, disk space, and the way to tasks, backups, logs and updates (Radarr,
/// Sonarr, Lidarr, Prowlarr share these endpoints).
struct ArrSystemView: View {
    let client: ArrClient
    @State private var status: JSONValue?
    @State private var health: [JSONValue] = []
    @State private var disks: [JSONValue] = []
    @State private var error: Error?
    @State private var confirmRestart = false
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorState(error: error) { await load() } }
            if let status {
                Section {
                    InfoRow(title: "manage.version", value: status["version"].text)
                    if let os = status["osName"].string { InfoRow(title: "manage.os", value: [os, status["osVersion"].string].compactMap { $0 }.joined(separator: " ")) }
                    if let b = status["branch"].string { InfoRow(title: "manage.branch", value: b) }
                    if let rt = status["runtimeName"].string { InfoRow(title: "manage.runtime", value: rt + " " + (status["runtimeVersion"].string ?? "")) }
                    if status["isDocker"].bool == true { InfoRow(title: "manage.docker", value: "✓") }
                    if let start = status["startTime"].string { InfoRow(title: "manage.started", value: ArrDate.short(start)) }
                    if let dir = status["appData"].string { InfoRow(title: "manage.app_data", value: dir) }
                } header: { Text("manage.status") }
                Section {
                    if health.isEmpty {
                        Label("manage.healthy", systemImage: "checkmark.seal.fill").foregroundStyle(.green)
                    }
                    ForEach(health, id: \.self) { h in
                        HStack(alignment: .top, spacing: 10) {
                            Image(systemName: h["type"].string?.lowercased() == "error" ? "xmark.octagon.fill" : "exclamationmark.triangle.fill")
                                .foregroundStyle(h["type"].string?.lowercased() == "error" ? .red : .orange)
                            VStack(alignment: .leading, spacing: 3) {
                                Text(verbatim: h["message"].string ?? "").font(.footnote)
                                if let wiki = h["wikiUrl"].string.flatMap(URL.init(string:)) {
                                    Link(destination: wiki) { Text("manage.more_info").font(.caption) }
                                }
                            }
                        }
                    }
                } header: { Text("manage.health") }
                if !disks.isEmpty {
                    Section {
                        ForEach(disks, id: \.self) { d in
                            let total = d["totalSpace"].double ?? 0
                            let free = d["freeSpace"].double ?? 0
                            VStack(alignment: .leading, spacing: 5) {
                                HStack {
                                    Text(verbatim: d["path"].string ?? "").font(.footnote.monospaced()).lineLimit(1)
                                    Spacer()
                                    Text(verbatim: byteText(d["freeSpace"]) + " / " + byteText(d["totalSpace"]))
                                        .font(.caption2.monospacedDigit()).foregroundStyle(Theme.textMuted)
                                }
                                ThinProgress(value: total > 0 ? (total - free) / total : 0)
                            }
                        }
                    } header: { Text("manage.disk_space") }
                }
            } else if error == nil {
                LoadingView()
            }
            Section {
                NavigationLink { ArrTasksView(client: client) } label: { MenuRow(title: "manage.tasks", icon: "clock.badge.checkmark") }
                NavigationLink { ArrBackupsView(client: client) } label: { MenuRow(title: "manage.backups", icon: "externaldrive") }
                NavigationLink { ArrLogsView(client: client) } label: { MenuRow(title: "manage.logs", icon: "doc.plaintext") }
                NavigationLink { ArrUpdatesView(client: client) } label: { MenuRow(title: "manage.updates", icon: "arrow.down.app") }
            }
            Section {
                Button("manage.restart", role: .destructive) { confirmRestart = true }
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.system"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .confirmationDialog("manage.restart_confirm", isPresented: $confirmRestart, titleVisibility: .visible) {
            Button("manage.restart", role: .destructive) { Task { await restart() } }
        }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            status = try await client.get("system/status")
            error = nil
        } catch {
            self.error = error
            return
        }
        health = (try? await client.get("health").list) ?? []
        disks = client.service == .prowlarr ? [] : ((try? await client.get("diskspace").list) ?? [])
    }

    private func restart() async {
        do {
            try await client.send("POST", "system/restart")
            notice = ManageNotice(text: "manage.restarting")
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// Scheduled tasks; run one now.
struct ArrTasksView: View {
    let client: ArrClient
    @State private var tasks: [JSONValue]?
    @State private var error: Error?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            ForEach(tasks ?? [], id: \.rowID) { t in
                HStack {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: t["name"].string ?? "").font(.subheadline.weight(.semibold))
                        Text(verbatim: [t["interval"].int.map { "\($0) min" }, ArrDate.short(t["lastExecution"].string)]
                            .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.caption2).foregroundStyle(Theme.textFaint)
                    }
                    Spacer()
                    Button { Task { await run(t) } } label: { Image(systemName: "play.circle.fill").font(.title3) }
                        .buttonStyle(.borderless)
                        .accessibilityLabel(Text("run_now"))
                }
            }
            if tasks == nil && error == nil { LoadingView() }
        }
        .manageBackground()
        .navigationTitle(Text("manage.tasks"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            tasks = try await client.get("system/task").list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func run(_ t: JSONValue) async {
        do {
            try await client.command(t["taskName"].string ?? t["name"].string ?? "")
            notice = ManageNotice(text: "manage.command_started")
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// Backups: list, create, download (share sheet), restore, delete.
struct ArrBackupsView: View {
    @State private var pendingDelete: JSONValue?
    let client: ArrClient
    @State private var backups: [JSONValue]?
    @State private var error: Error?
    @State private var shared: SharedFile?
    @State private var pendingRestore: JSONValue?
    @State private var notice: ManageNotice?
    @State private var busy = false

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            Section {
                Button { Task { await create() } } label: { Label("manage.backup_now", systemImage: "plus.circle") }
            }
            Section {
                ForEach(backups ?? [], id: \.rowID) { b in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: b["name"].string ?? "").font(.footnote.monospaced()).lineLimit(1)
                        Text(verbatim: [b["type"].string?.humanizedKey, byteText(b["size"]), ArrDate.short(b["time"].string)]
                            .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.caption2).foregroundStyle(Theme.textFaint)
                    }
                    .swipeActions(edge: .trailing) {
                        Button(role: .destructive) { pendingDelete = b } label: { Label("manage.delete", systemImage: "trash") }
                        Button { pendingRestore = b } label: { Label("manage.restore", systemImage: "arrow.counterclockwise") }.tint(.orange)
                    }
                    .swipeActions(edge: .leading) {
                        Button { Task { await download(b) } } label: { Label("manage.download", systemImage: "square.and.arrow.up") }.tint(Theme.accent)
                    }
                }
                if backups?.isEmpty == true { EmptyRow(text: "manage.empty") }
                if backups == nil && error == nil { LoadingView() }
            } footer: { Text("manage.backups_hint") }
        }
        .manageBackground()
        .confirmDelete($pendingDelete, detail: { $0["name"].text }) { b in Task { await delete(b) } }
        .navigationTitle(Text("manage.backups"))
        .navigationBarTitleDisplayMode(.inline)
        .overlay { if busy { ProgressView().padding(20).background(Theme.surface2, in: RoundedRectangle(cornerRadius: 12)) } }
        .task { await load() }
        .refreshable { await load() }
        .sheet(item: $shared, onDismiss: { ArrClient.removeDownloads() }) { file in ShareSheet(items: [file.url]) }
        .confirmationDialog("manage.restore_confirm", isPresented: Binding(get: { pendingRestore != nil }, set: { if !$0 { pendingRestore = nil } }), titleVisibility: .visible) {
            Button("manage.restore", role: .destructive) { if let b = pendingRestore { Task { await restore(b) } } }
        } message: { Text("manage.restore_explain") }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            backups = try await client.get("system/backup").list.sorted { ($0["time"].string ?? "") > ($1["time"].string ?? "") }
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func create() async {
        do {
            try await client.command("Backup")
            notice = ManageNotice(text: "manage.command_started")
            try? await Task.sleep(for: .seconds(3))
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func download(_ b: JSONValue) async {
        guard let path = b["path"].string else { return }
        busy = true
        defer { busy = false }
        do {
            // "/backup/manual/x.zip" (the service may add its URL base in front).
            let relative = path.range(of: "backup/").map { String(path[$0.lowerBound...]) } ?? path
            let file = try await client.download(client.rawURL(relative), fileName: b["name"].string ?? "backup.zip")
            shared = SharedFile(url: file)
        } catch is CancellationError {} catch { self.error = error }
    }

    private func restore(_ b: JSONValue) async {
        do {
            try await client.send("POST", "system/backup/restore/\(b["id"].text)")
            notice = ManageNotice(text: "manage.restarting")
        } catch is CancellationError {} catch { self.error = error }
    }

    private func delete(_ b: JSONValue) async {
        do {
            try await client.send("DELETE", "system/backup/\(b["id"].text)")
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// The newest log entries.
struct ArrLogsView: View {
    let client: ArrClient
    @State private var records: [JSONValue]?
    @State private var error: Error?
    @State private var level = "all"

    var body: some View {
        List {
            Section {
                Picker(selection: $level) {
                    Text("manage.filter.all").tag("all")
                    Text(verbatim: "Info").tag("info")
                    Text(verbatim: "Warn").tag("warn")
                    Text(verbatim: "Error").tag("error")
                } label: { Text("manage.level") }
            }
            if let error { ArrErrorLine(error: error) }
            ForEach(records ?? [], id: \.rowID) { r in
                VStack(alignment: .leading, spacing: 3) {
                    HStack {
                        PlainBadge(text: r["level"].text.uppercased(), tone: tone(r["level"].text))
                        Text(verbatim: r["logger"].string ?? "").font(.caption2).foregroundStyle(Theme.textFaint)
                        Spacer()
                        Text(verbatim: ArrDate.short(r["time"].string)).font(.caption2).foregroundStyle(Theme.textFaint)
                    }
                    Text(verbatim: r["message"].string ?? "").font(.caption.monospaced()).textSelection(.enabled)
                    if let ex = r["exception"].string?.nilIfBlank {
                        Text(verbatim: ex).font(.caption2.monospaced()).foregroundStyle(.red.opacity(0.8)).lineLimit(8)
                    }
                }
            }
            if records == nil && error == nil { LoadingView() }
        }
        .manageBackground()
        .navigationTitle(Text("manage.logs"))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: level) { await load() }
        .refreshable { await load() }
    }

    private func tone(_ level: String) -> TagBadge.Tone {
        switch level.lowercased() {
        case "error", "fatal": .danger
        case "warn": .accent
        default: .neutral
        }
    }

    private func load() async {
        do {
            var query: [URLQueryItem] = [.q("page", 1), .q("pageSize", 100), .q("sortKey", "time"), .q("sortDirection", "descending")]
            if level != "all" { query.append(.q("level", level)) }
            records = try await client.get("log", query: query)["records"].list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// Available updates and their changes; install the newest.
struct ArrUpdatesView: View {
    @State private var confirmInstall = false
    let client: ArrClient
    @State private var updates: [JSONValue]?
    @State private var error: Error?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let latest = updates?.first(where: { $0["installable"].bool == true && $0["installed"].bool != true }) {
                Section {
                    Button { confirmInstall = true } label: {
                        Label { Text("manage.install_update") + Text(verbatim: " " + latest["version"].text) } icon: { Image(systemName: "arrow.down.app.fill") }
                    }
                }
            }
            ForEach(updates ?? [], id: \.self) { u in
                Section {
                    ForEach(u["changes"]["new"].list.map(\.text), id: \.self) { c in Label { Text(verbatim: c).font(.footnote) } icon: { Image(systemName: "plus").foregroundStyle(.green) } }
                    ForEach(u["changes"]["fixed"].list.map(\.text), id: \.self) { c in Label { Text(verbatim: c).font(.footnote) } icon: { Image(systemName: "wrench").foregroundStyle(Theme.accent) } }
                } header: {
                    HStack {
                        Text(verbatim: u["version"].text)
                        if u["installed"].bool == true { TagBadge(text: "manage.installed", tone: .success) }
                        Spacer()
                        Text(verbatim: String(u["releaseDate"].string?.prefix(10) ?? ""))
                    }
                }
            }
            if updates?.isEmpty == true { EmptyRow(text: "manage.up_to_date") }
            if updates == nil && error == nil { LoadingView() }
        }
        .manageBackground()
        .confirmationDialog("manage.install_confirm", isPresented: $confirmInstall, titleVisibility: .visible) {
            Button("manage.install_update") { Task { await install() } }
        }
        .navigationTitle(Text("manage.updates"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            updates = try await client.get("update").list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func install() async {
        do {
            try await client.command("ApplicationUpdate")
            notice = ManageNotice(text: "manage.command_started")
        } catch is CancellationError {} catch { self.error = error }
    }
}
