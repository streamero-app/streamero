import SwiftUI

/// Movies or episodes with missing subtitles; search one language or pick a subtitle by hand.
struct BazarrWantedView: View {
    let client: ArrClient
    let movies: Bool
    @State private var rows: [JSONValue]?
    @State private var total = 0
    @State private var error: Error?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let rows {
                Section {
                    ForEach(rows, id: \.self) { row in
                        VStack(alignment: .leading, spacing: 6) {
                            Text(verbatim: title(row)).font(.subheadline.weight(.semibold))
                            if !movies {
                                Text(verbatim: [row["episode_number"].string, row["episodeTitle"].string].compactMap { $0 }.joined(separator: " · "))
                                    .font(.caption).foregroundStyle(Theme.textMuted)
                            }
                            ScrollView(.horizontal, showsIndicators: false) {
                                HStack(spacing: 6) {
                                    ForEach(row["missing_subtitles"].list, id: \.self) { lang in
                                        Button { Task { await search(row, lang) } } label: {
                                            Label { Text(verbatim: languageLabel(lang)) } icon: { Image(systemName: "magnifyingglass") }
                                                .font(.caption.weight(.semibold))
                                        }
                                        .buttonStyle(.bordered)
                                        .tint(Theme.accent)
                                    }
                                }
                            }
                            NavigationLink {
                                BazarrManualSearchView(client: client, movies: movies, row: row)
                            } label: {
                                Text("manage.search_interactive").font(.caption)
                            }
                        }
                        .padding(.vertical, 3)
                    }
                    if rows.isEmpty { EmptyRow(text: "manage.nothing_wanted") }
                } header: { Text("manage.count_items \(total)") }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text(movies ? LocalizedStringKey("manage.movies_missing") : LocalizedStringKey("manage.episodes_missing")))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { Task { await searchAll() } } label: { Label("manage.search_all", systemImage: "magnifyingglass") }
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .manageNotice($notice)
    }

    private func title(_ row: JSONValue) -> String {
        movies ? (row["title"].string ?? "") : (row["seriesTitle"].string ?? "")
    }

    private func languageLabel(_ lang: JSONValue) -> String {
        var label = lang["name"].string ?? lang["code2"].string ?? ""
        if lang["forced"].bool == true { label += " (" + Loc.string("manage.forced") + ")" }
        if lang["hi"].bool == true { label += " (HI)" }
        return label
    }

    private func load() async {
        do {
            let result = try await client.get(movies ? "movies/wanted" : "episodes/wanted", query: [.q("start", 0), .q("length", 200)])
            rows = result["data"].list
            total = result["total"].int ?? rows?.count ?? 0
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func search(_ row: JSONValue, _ lang: JSONValue) async {
        var query: [URLQueryItem] = [.q("language", lang["code2"].text), .q("forced", lang["forced"].bool ?? false), .q("hi", lang["hi"].bool ?? false)]
        if movies {
            query.append(.q("radarrid", row["radarrId"].text))
        } else {
            query.append(.q("seriesid", row["sonarrSeriesId"].text))
            query.append(.q("episodeid", row["sonarrEpisodeId"].text))
        }
        do {
            try await client.send("PATCH", movies ? "movies/subtitles" : "episodes/subtitles", query: query)
            notice = ManageNotice(text: "manage.command_started")
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func searchAll() async {
        let task = movies ? "wanted_search_missing_subtitles_movies" : "wanted_search_missing_subtitles_series"
        do {
            try await client.send("POST", "system/tasks", query: [.q("taskid", task)])
            notice = ManageNotice(text: "manage.command_started")
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// Subtitles offered by the providers for one movie or episode.
struct BazarrManualSearchView: View {
    let client: ArrClient
    let movies: Bool
    let row: JSONValue
    @State private var offers: [JSONValue]?
    @State private var error: Error?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let offers {
                ForEach(offers, id: \.self) { o in
                    Button { Task { await download(o) } } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            HStack(spacing: 6) {
                                PlainBadge(text: o["language"].text, tone: .accent)
                                PlainBadge(text: o["provider"].text)
                                if let score = o["score"].double { PlainBadge(text: "\(Int(score))%") }
                                if o["hearing_impaired"].text == "True" || o["hearing_impaired"].bool == true { PlainBadge(text: "HI") }
                                if o["forced"].text == "True" || o["forced"].bool == true { PlainBadge(text: Loc.string("manage.forced")) }
                            }
                            Text(verbatim: o["release_info"].list.map(\.text).joined(separator: "\n")).font(.caption.monospaced()).foregroundStyle(Theme.text).lineLimit(3)
                            if let u = o["uploader"].string?.nilIfBlank { Text(verbatim: u).font(.caption2).foregroundStyle(Theme.textFaint) }
                        }
                    }
                    .buttonStyle(.plain)
                }
                if offers.isEmpty { EmptyRow(text: "manage.no_results") }
            } else if error == nil {
                VStack(spacing: 8) {
                    LoadingView()
                    Text("manage.searching_providers").font(.footnote).foregroundStyle(Theme.textFaint)
                }
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.search_interactive"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .manageNotice($notice)
    }

    private var idQuery: [URLQueryItem] {
        movies ? [.q("radarrid", row["radarrId"].text)] : [.q("seriesid", row["sonarrSeriesId"].text), .q("episodeid", row["sonarrEpisodeId"].text)]
    }

    private func load() async {
        do {
            let query: [URLQueryItem] = movies ? [.q("radarrid", row["radarrId"].text)] : [.q("episodeid", row["sonarrEpisodeId"].text)]
            offers = try await client.get(movies ? "providers/movies" : "providers/episodes", query: query, timeout: 180)["data"].list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func download(_ o: JSONValue) async {
        let flag: (JSONValue) -> String = { v in (v.bool == true || v.text == "True") ? "true" : "false" }
        let query = idQuery + [
            .q("hi", flag(o["hearing_impaired"])), .q("forced", flag(o["forced"])), .q("original_format", flag(o["original_format"])),
            .q("provider", o["provider"].text), .q("subtitle", o["subtitle"].text),
        ]
        do {
            try await client.send("POST", movies ? "providers/movies" : "providers/episodes", query: query)
            notice = ManageNotice(text: "manage.downloaded")
        } catch is CancellationError {} catch { self.error = error }
    }
}

struct BazarrHistoryView: View {
    let client: ArrClient
    @State private var movies = true
    @State private var rows: [JSONValue]?
    @State private var error: Error?

    var body: some View {
        List {
            Section {
                Picker(selection: $movies) {
                    Text("manage.movies").tag(true)
                    Text("manage.series").tag(false)
                } label: { EmptyView() }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
            }
            if let error { ArrErrorLine(error: error) }
            ForEach(rows ?? [], id: \.self) { r in
                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: r["title"].string ?? r["seriesTitle"].string ?? "").font(.subheadline.weight(.semibold))
                    if let ep = r["episode_number"].string { Text(verbatim: ep + " · " + (r["episodeTitle"].string ?? "")).font(.caption).foregroundStyle(Theme.textMuted) }
                    Text(verbatim: r["description"].string ?? "").font(.caption).foregroundStyle(Theme.textMuted).lineLimit(3)
                    Text(verbatim: [r["language"]["name"].string, r["provider"].string, r["score"].string, r["timestamp"].string]
                        .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(.caption2).foregroundStyle(Theme.textFaint)
                }
            }
            if rows == nil && error == nil { LoadingView() }
        }
        .manageBackground()
        .navigationTitle(Text("manage.history"))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: movies) { await load() }
    }

    private func load() async {
        rows = nil
        do {
            rows = try await client.get(movies ? "history/movies" : "history/episodes", query: [.q("start", 0), .q("length", 100)])["data"].list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}

struct BazarrProvidersView: View {
    let client: ArrClient
    @State private var providers: [JSONValue]?
    @State private var error: Error?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            ForEach(providers ?? [], id: \.self) { p in
                HStack {
                    Text(verbatim: p["name"].string ?? "")
                    Spacer()
                    let status = p["status"].text
                    PlainBadge(text: status, tone: status.lowercased() == "good" || status.isEmpty ? .success : .danger)
                }
                if let retry = p["retry"].string?.nilIfBlank, retry != "-" {
                    Text(verbatim: retry).font(.caption2).foregroundStyle(Theme.textFaint)
                }
            }
            if providers?.isEmpty == true { EmptyRow(text: "manage.providers_all_good") }
            if providers == nil && error == nil { LoadingView() }
            Section {
                Button { Task { await reset() } } label: { Label("manage.reset_providers", systemImage: "arrow.counterclockwise") }
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.providers"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            providers = try await client.get("providers")["data"].list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func reset() async {
        do {
            try await client.send("POST", "providers", query: [.q("action", "reset")])
            notice = ManageNotice(text: "manage.done")
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// Status, health, tasks, backups and logs of Bazarr.
struct BazarrSystemView: View {
    @State private var pendingDelete: JSONValue?
    @State private var pendingRestore: JSONValue?
    let client: ArrClient
    @State private var status: JSONValue?
    @State private var health: [JSONValue] = []
    @State private var tasks: [JSONValue] = []
    @State private var backups: [JSONValue] = []
    @State private var logs: [JSONValue] = []
    @State private var error: Error?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let status {
                Section {
                    ForEach(status.flattened(), id: \.self) { item in
                        InfoRowVerbatim(title: item.path.humanizedKey, value: item.value.text)
                    }
                } header: { Text("manage.status") }
            }
            Section {
                if health.isEmpty { Label("manage.healthy", systemImage: "checkmark.seal.fill").foregroundStyle(.green) }
                ForEach(health, id: \.self) { h in
                    Text(verbatim: [h["object"].string, h["issue"].string].compactMap { $0 }.joined(separator: ": ")).font(.footnote)
                }
            } header: { Text("manage.health") }
            Section {
                ForEach(tasks, id: \.self) { t in
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: t["name"].string ?? "").font(.subheadline)
                            Text(verbatim: [t["interval"].string, t["next_run_in"].string].compactMap { $0 }.joined(separator: " · "))
                                .font(.caption2).foregroundStyle(Theme.textFaint)
                        }
                        Spacer()
                        if t["job_running"].bool == true {
                            ProgressView()
                        } else {
                            Button { Task { await run(t) } } label: { Image(systemName: "play.circle.fill").font(.title3) }
                                .buttonStyle(.borderless).accessibilityLabel(Text("run_now"))
                        }
                    }
                }
            } header: { Text("manage.tasks") }
            Section {
                Button { Task { await createBackup() } } label: { Label("manage.backup_now", systemImage: "plus.circle") }
                ForEach(backups, id: \.self) { b in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: b["filename"].string ?? "").font(.footnote.monospaced())
                        Text(verbatim: [b["date"].string, b["size"].string].compactMap { $0 }.joined(separator: " · ")).font(.caption2).foregroundStyle(Theme.textFaint)
                    }
                    .swipeActions {
                        Button(role: .destructive) { pendingDelete = b } label: { Label("manage.delete", systemImage: "trash") }
                        Button { pendingRestore = b } label: { Label("manage.restore", systemImage: "arrow.counterclockwise") }.tint(.orange)
                    }
                }
            } header: { Text("manage.backups") }
            Section {
                ForEach(logs.prefix(100), id: \.self) { l in
                    VStack(alignment: .leading, spacing: 2) {
                        HStack {
                            PlainBadge(text: l["type"].text.uppercased(), tone: l["type"].text.uppercased() == "ERROR" ? .danger : .neutral)
                            Spacer()
                            Text(verbatim: l["timestamp"].text).font(.caption2).foregroundStyle(Theme.textFaint)
                        }
                        Text(verbatim: l["message"].text).font(.caption.monospaced()).lineLimit(6)
                    }
                }
            } header: { Text("manage.logs") }
        }
        .manageBackground()
        .confirmDelete($pendingDelete, detail: { $0["filename"].text }) { b in Task { await deleteBackup(b) } }
        .confirmItemAction("manage.restore_confirm", button: "manage.restore", item: $pendingRestore,
                           detail: { _ in Loc.string("manage.restore_explain") }) { b in Task { await restore(b) } }
        .navigationTitle(Text("manage.system"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            status = try await client.get("system/status")["data"]
            error = nil
        } catch {
            self.error = error
            return
        }
        health = (try? await client.get("system/health"))?["data"].list ?? []
        tasks = (try? await client.get("system/tasks"))?["data"].list ?? []
        backups = (try? await client.get("system/backups"))?["data"].list ?? []
        logs = ((try? await client.get("system/logs"))?["data"].list ?? []).reversed()
    }

    private func run(_ t: JSONValue) async {
        do {
            try await client.send("POST", "system/tasks", query: [.q("taskid", t["job_id"].text)])
            notice = ManageNotice(text: "manage.command_started")
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func createBackup() async {
        do {
            try await client.send("POST", "system/backups")
            notice = ManageNotice(text: "manage.command_started")
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func restore(_ b: JSONValue) async {
        do {
            try await client.send("PATCH", "system/backups", query: [.q("filename", b["filename"].text)])
            notice = ManageNotice(text: "manage.restarting")
        } catch is CancellationError {} catch { self.error = error }
    }

    private func deleteBackup(_ b: JSONValue) async {
        do {
            try await client.send("DELETE", "system/backups", query: [.q("filename", b["filename"].text)])
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// A labelled value where the label comes from the API.
struct InfoRowVerbatim: View {
    let title: String
    let value: String

    var body: some View {
        LabeledContent {
            Text(verbatim: value).foregroundStyle(Theme.textMuted).multilineTextAlignment(.trailing).textSelection(.enabled)
        } label: { Text(verbatim: title) }
    }
}

/// Bazarr's settings: one generic form per section of its settings JSON.
struct BazarrSettingsView: View {
    let client: ArrClient
    @State private var sections: [String]?
    @State private var error: Error?

    var body: some View {
        List {
            if let error { ArrErrorState(error: error) { await load() } }
            Section {
                NavigationLink {
                    BazarrLanguagesView(client: client)
                } label: { MenuRow(title: "manage.languages", icon: "character.bubble") }
            }
            ForEach(sections ?? [], id: \.self) { section in
                NavigationLink {
                    BazarrSectionEditor(client: client, section: section)
                } label: {
                    Label { Text(verbatim: section.humanizedKey) } icon: { Image(systemName: "gearshape").foregroundStyle(Theme.accent) }
                }
            }
            if sections == nil && error == nil { LoadingView() }
        }
        .manageBackground()
        .navigationTitle(Text("manage.settings"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        do {
            let settings = try await client.get("system/settings")
            sections = (settings.object ?? [:]).filter { $0.value.object != nil }.keys.sorted()
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// One section of Bazarr's settings; saves only what changed.
struct BazarrSectionEditor: View {
    let client: ArrClient
    let section: String
    @State private var state: ObjectFormState?
    @State private var error: Error?
    @State private var busy = false
    @State private var notice: ManageNotice?

    var body: some View {
        Form {
            if let binding = Binding($state) {
                Section {
                    ObjectFieldsView(state: binding, paths: binding.wrappedValue.original.flattened().map(\.path).filter { $0.hasPrefix(section + ".") })
                }
                Section {
                    ArrErrorLine(error: error)
                    Button { Task { await save() } } label: { HStack { Text("save"); Spacer(); if busy { ProgressView() } } }
                        .disabled(busy || !binding.wrappedValue.hasChanges)
                }
            } else if let error {
                ArrErrorState(error: error) { await load() }
            } else {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: section.humanizedKey))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            let all = try await client.get("system/settings")
            // Keep only this section so unchanged secrets elsewhere are never touched.
            state = ObjectFormState([section: all[section]])
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func save() async {
        guard let state else { return }
        busy = true
        defer { busy = false }
        do {
            try await client.sendForm("POST", "system/settings", fields: BazarrSettings.formFields(state.changes()))
            notice = ManageNotice(text: "saved")
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }
}
