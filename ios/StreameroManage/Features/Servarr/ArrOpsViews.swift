import SwiftUI

// MARK: Add new

/// Look up a new movie, series or artist and add it.
struct ArrAddView: View {
    let client: ArrClient
    @State private var query = ""
    @State private var results: [JSONValue]?
    @State private var error: Error?
    @State private var searching = false

    private var flavor: ArrFlavor { ArrFlavor(client.service) }

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if searching { LoadingView() }
            if let results {
                ForEach(results, id: \.self) { item in
                    let inLibrary = (item["id"].int ?? 0) > 0
                    NavigationLink {
                        if inLibrary {
                            ArrItemDetailView(client: client, id: item["id"].int ?? 0)
                        } else {
                            ArrAddOptionsView(client: client, item: item)
                        }
                    } label: {
                        HStack(alignment: .top, spacing: 12) {
                            ArrPoster(client: client, item: item, title: item[flavor.titleKey].string ?? "")
                                .frame(width: 56, height: 84)
                                .clipShape(RoundedRectangle(cornerRadius: 4, style: .continuous))
                            VStack(alignment: .leading, spacing: 4) {
                                Text(verbatim: item[flavor.titleKey].string ?? "").font(.subheadline.weight(.semibold))
                                Text(verbatim: [item["year"].int.map { String($0) }, item["network"].string, item["disambiguation"].string]
                                    .compactMap { $0 }.filter { !$0.isEmpty && $0 != "0" }.joined(separator: " · "))
                                    .font(.caption).foregroundStyle(Theme.textMuted)
                                if inLibrary { TagBadge(text: "manage.in_library", tone: .success) }
                                Text(verbatim: item["overview"].string ?? "").font(.caption).foregroundStyle(Theme.textFaint).lineLimit(3)
                            }
                        }
                    }
                }
                if results.isEmpty { EmptyRow(text: "manage.no_results") }
            } else if !searching {
                EmptyRow(text: "manage.add_hint")
            }
        }
        .manageBackground()
        .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always))
        .onSubmit(of: .search) { Task { await search() } }
        .navigationTitle(Text("manage.add_new"))
        .navigationBarTitleDisplayMode(.inline)
    }

    private func search() async {
        let term = query.trimmingCharacters(in: .whitespaces)
        guard !term.isEmpty else { return }
        searching = true
        defer { searching = false }
        do {
            results = try await client.get(flavor.lookup, query: [.q("term", term)], timeout: 90).list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// Where and how to add a looked-up item.
struct ArrAddOptionsView: View {
    let client: ArrClient
    let item: JSONValue
    @State private var rootFolders: [JSONValue] = []
    @State private var profiles: [JSONValue] = []
    @State private var metadataProfiles: [JSONValue] = []
    @State private var tags: [JSONValue] = []
    @State private var rootFolder: String = ""
    @State private var profile: JSONValue = .null
    @State private var metadataProfile: JSONValue = .null
    @State private var monitor = "all"
    @State private var availability = "released"
    @State private var seriesType = "standard"
    @State private var seasonFolder = true
    @State private var searchNow = true
    @State private var selectedTags: [JSONValue] = []
    @State private var error: Error?
    @State private var busy = false
    @State private var added: Int?

    private var flavor: ArrFlavor { ArrFlavor(client.service) }

    private var monitorOptions: [String] {
        switch client.service {
        case .radarr: ["movieOnly", "movieAndCollection", "none"]
        case .sonarr: ["all", "future", "missing", "existing", "recent", "pilot", "firstSeason", "lastSeason", "monitorSpecials", "unmonitorSpecials", "none"]
        default: ["all", "future", "missing", "existing", "first", "latest", "none"]
        }
    }

    var body: some View {
        Form {
            Section {
                HStack(alignment: .top, spacing: 12) {
                    ArrPoster(client: client, item: item, title: item[flavor.titleKey].string ?? "")
                        .frame(width: 70, height: 105)
                        .clipShape(RoundedRectangle(cornerRadius: 4, style: .continuous))
                    VStack(alignment: .leading, spacing: 4) {
                        Text(verbatim: item[flavor.titleKey].string ?? "").headFont(16)
                        Text(verbatim: item["overview"].string ?? "").font(.caption).foregroundStyle(Theme.textMuted).lineLimit(6)
                    }
                }
            }
            .listRowBackground(Color.clear)
            Section {
                Picker(selection: $rootFolder) {
                    ForEach(rootFolders.compactMap { $0["path"].string }, id: \.self) { p in Text(verbatim: p).tag(p) }
                } label: { Text("manage.root_folder") }
                Picker(selection: $profile) {
                    ForEach(idOptions(profiles)) { o in Text(verbatim: o.name).tag(o.value) }
                } label: { Text("manage.quality_profile") }
                if client.service == .lidarr {
                    Picker(selection: $metadataProfile) {
                        ForEach(idOptions(metadataProfiles)) { o in Text(verbatim: o.name).tag(o.value) }
                    } label: { Text("manage.metadata_profile") }
                }
                Picker(selection: $monitor) {
                    ForEach(monitorOptions, id: \.self) { m in Text(verbatim: m.humanizedKey).tag(m) }
                } label: { Text("manage.monitor") }
                if client.service == .radarr {
                    Picker(selection: $availability) {
                        ForEach(["announced", "inCinemas", "released"], id: \.self) { a in Text(verbatim: a.humanizedKey).tag(a) }
                    } label: { Text("manage.minimum_availability") }
                }
                if client.service == .sonarr {
                    Picker(selection: $seriesType) {
                        ForEach(["standard", "daily", "anime"], id: \.self) { t in Text(verbatim: t.humanizedKey).tag(t) }
                    } label: { Text("manage.series_type") }
                    Toggle("manage.season_folder", isOn: $seasonFolder)
                }
                MultiSelectRow(title: Loc.string("manage.tags"), options: tagOptions(tags), selection: $selectedTags) { Text("manage.tags") }
                Toggle("manage.search_on_add", isOn: $searchNow)
            }
            Section {
                ArrErrorLine(error: error)
                if let added {
                    NavigationLink {
                        ArrItemDetailView(client: client, id: added)
                    } label: { Label("manage.added_open", systemImage: "checkmark.circle.fill").foregroundStyle(.green) }
                } else {
                    Button { Task { await add() } } label: {
                        HStack { Text("manage.add"); Spacer(); if busy { ProgressView() } }
                    }
                    .disabled(busy || rootFolder.isEmpty || profile.isNull)
                }
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.add"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        do {
            rootFolders = try await client.get("rootfolder").list
            profiles = try await client.get("qualityprofile").list
            tags = (try? await client.get("tag").list) ?? []
            if client.service == .lidarr { metadataProfiles = (try? await client.get("metadataprofile").list) ?? [] }
            if rootFolder.isEmpty { rootFolder = rootFolders.first?["path"].string ?? "" }
            if profile.isNull { profile = profiles.first?["id"] ?? .null }
            if metadataProfile.isNull { metadataProfile = metadataProfiles.first?["id"] ?? .null }
            if client.service == .radarr { monitor = "movieOnly" }
        } catch is CancellationError {} catch { self.error = error }
    }

    /// The lookup result plus the choices, as the add endpoint takes it.
    func requestBody() -> JSONValue {
        var body = item
        body["qualityProfileId"] = profile
        body["rootFolderPath"] = .string(rootFolder)
        body["tags"] = .array(selectedTags)
        switch client.service {
        case .radarr:
            body["monitored"] = .bool(monitor != "none")
            body["minimumAvailability"] = .string(availability)
            body["addOptions"] = ["searchForMovie": .bool(searchNow), "monitor": .string(monitor)]
        case .sonarr:
            body["monitored"] = .bool(monitor != "none")
            body["seriesType"] = .string(seriesType)
            body["seasonFolder"] = .bool(seasonFolder)
            body["addOptions"] = ["monitor": .string(monitor), "searchForMissingEpisodes": .bool(searchNow),
                                  "searchForCutoffUnmetEpisodes": .bool(false)]
        default:
            body["monitored"] = .bool(monitor != "none")
            body["metadataProfileId"] = metadataProfile
            body["monitorNewItems"] = .string("all")
            body["addOptions"] = ["monitor": .string(monitor), "searchForMissingAlbums": .bool(searchNow)]
        }
        return body
    }

    private func add() async {
        busy = true
        defer { busy = false }
        do {
            let result = try await client.send("POST", flavor.items, json: requestBody())
            added = result["id"].int
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}

// MARK: Calendar

/// What comes out over the next weeks (and the last one).
struct ArrCalendarView: View {
    let client: ArrClient
    @State private var entries: [CalendarEntry]?
    @State private var error: Error?
    @State private var weekOffset = 0

    struct CalendarEntry: Identifiable, Hashable {
        let id: String
        let date: Date
        let title: String
        let subtitle: String
        let kind: String
        let hasFile: Bool
        let item: JSONValue
    }

    var body: some View {
        List {
            Section {
                HStack {
                    Button { weekOffset -= 1 } label: { Image(systemName: "chevron.left") }.buttonStyle(.borderless)
                    Spacer()
                    Text(verbatim: rangeText).font(.subheadline.weight(.semibold))
                    Spacer()
                    Button { weekOffset += 1 } label: { Image(systemName: "chevron.right") }.buttonStyle(.borderless)
                }
            }
            if let error { ArrErrorLine(error: error) }
            if let entries {
                let days = Dictionary(grouping: entries) { Calendar.current.startOfDay(for: $0.date) }
                ForEach(days.keys.sorted(), id: \.self) { day in
                    Section {
                        ForEach(days[day] ?? []) { e in
                            HStack(spacing: 12) {
                                Rectangle().fill(e.hasFile ? Color.green : Theme.accent).frame(width: 3).clipShape(Capsule())
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(verbatim: e.title).font(.subheadline.weight(.semibold))
                                    Text(verbatim: e.subtitle).font(.caption).foregroundStyle(Theme.textMuted)
                                }
                                Spacer()
                                Text(verbatim: e.date.formatted(date: .omitted, time: .shortened)).font(.caption2).foregroundStyle(Theme.textFaint)
                            }
                        }
                    } header: { Text(verbatim: ArrDate.day(day)) }
                }
                if entries.isEmpty { EmptyRow(text: "manage.nothing_scheduled") }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.calendar"))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: weekOffset) { await load() }
        .refreshable { await load() }
    }

    private var range: (Date, Date) {
        let cal = Calendar.current
        let start = cal.date(byAdding: .day, value: weekOffset * 7 - 1, to: cal.startOfDay(for: Date())) ?? Date()
        let end = cal.date(byAdding: .day, value: 14, to: start) ?? Date()
        return (start, end)
    }

    private var rangeText: String {
        let (a, b) = range
        return a.formatted(.dateTime.day().month(.abbreviated)) + " – " + b.formatted(.dateTime.day().month(.abbreviated))
    }

    private func load() async {
        let (start, end) = range
        var query: [URLQueryItem] = [.q("start", ArrDate.iso(start)), .q("end", ArrDate.iso(end)), .q("unmonitored", "true")]
        if client.service == .sonarr { query.append(.q("includeSeries", "true")) }
        if client.service == .lidarr { query.append(.q("includeArtist", "true")) }
        do {
            let list = try await client.get("calendar", query: query).list
            entries = list.flatMap { Self.entries($0, service: client.service, from: start, to: end) }.sorted { $0.date < $1.date }
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    static func entries(_ item: JSONValue, service: ArrService, from: Date, to: Date) -> [CalendarEntry] {
        switch service {
        case .sonarr:
            guard let date = ArrDate.parse(item["airDateUtc"].string) else { return [] }
            let code = String(format: "S%02dE%02d", item["seasonNumber"].int ?? 0, item["episodeNumber"].int ?? 0)
            return [CalendarEntry(id: item.rowID, date: date, title: item["series"]["title"].string ?? "",
                                  subtitle: code + " · " + (item["title"].string ?? ""), kind: "episode",
                                  hasFile: item["hasFile"].bool ?? false, item: item)]
        case .lidarr:
            guard let date = ArrDate.parse(item["releaseDate"].string) else { return [] }
            return [CalendarEntry(id: item.rowID, date: date, title: item["artist"]["artistName"].string ?? "",
                                  subtitle: item["title"].string ?? "", kind: "album",
                                  hasFile: (item["statistics"]["trackFileCount"].int ?? 0) > 0, item: item)]
        default:
            let kinds = ["inCinemas", "digitalRelease", "physicalRelease"]
            return kinds.compactMap { key -> CalendarEntry? in
                guard let date = ArrDate.parse(item[key].string), date >= from, date <= to else { return nil }
                return CalendarEntry(id: item.rowID + key, date: date, title: item["title"].string ?? "",
                                     subtitle: key.humanizedKey, kind: key, hasFile: item["hasFile"].bool ?? false, item: item)
            }
        }
    }
}

// MARK: Activity

/// Queue (live), history and blocklist.
struct ArrActivityView: View {
    let client: ArrClient
    @State private var tab = 0

    var body: some View {
        VStack(spacing: 0) {
            Picker(selection: $tab) {
                Text("manage.queue").tag(0)
                Text("manage.history").tag(1)
                Text("manage.blocklist").tag(2)
            } label: { EmptyView() }
            .pickerStyle(.segmented)
            .padding(.horizontal, 16).padding(.vertical, 8)
            switch tab {
            case 0: ArrQueueList(client: client)
            case 1: ArrHistoryList(client: client)
            default: ArrBlocklistView(client: client)
            }
        }
        .background(Theme.background)
        .navigationTitle(Text("manage.activity"))
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// The download queue, refreshed every 5 seconds while it is on screen.
struct ArrQueueList: View {
    let client: ArrClient
    @State private var records: [JSONValue]?
    @State private var error: Error?
    @State private var pendingRemove: JSONValue?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let records {
                ForEach(records, id: \.rowID) { r in
                    QueueRow(record: r)
                        .swipeActions {
                            Button(role: .destructive) { pendingRemove = r } label: { Label("manage.remove", systemImage: "xmark") }
                        }
                }
                if records.isEmpty { EmptyRow(text: "manage.queue_empty") }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .poll(every: { .seconds(error == nil ? 5 : 30) }) { await load() }
        .refreshable { await load() }
        .confirmationDialog("manage.remove_confirm", isPresented: Binding(get: { pendingRemove != nil }, set: { if !$0 { pendingRemove = nil } }), titleVisibility: .visible) {
            if let r = pendingRemove {
                Button("manage.remove_from_client", role: .destructive) { Task { await remove(r, client: true, blocklist: false) } }
                Button("manage.remove_and_blocklist", role: .destructive) { Task { await remove(r, client: true, blocklist: true) } }
                Button("manage.remove_queue_only") { Task { await remove(r, client: false, blocklist: false) } }
            }
        } message: {
            Text(verbatim: pendingRemove?["title"].string ?? "")
        }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            let base: [URLQueryItem] = [.q("page", 1), .q("pageSize", 100)]
            let page = try await client.get("queue", query: base + ArrFlavor(client.service).includeQuery)
            records = page["records"].list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func remove(_ r: JSONValue, client removeFromClient: Bool, blocklist: Bool) async {
        do {
            try await client.send("DELETE", "queue/\(r["id"].text)", query: [.q("removeFromClient", removeFromClient), .q("blocklist", blocklist)])
            notice = ManageNotice(text: "manage.removed")
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }
}

struct QueueRow: View {
    let record: JSONValue

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(verbatim: itemTitle).font(.subheadline.weight(.semibold)).lineLimit(1)
            Text(verbatim: record["title"].string ?? "").font(.caption2.monospaced()).foregroundStyle(Theme.textFaint).lineLimit(2)
            ThinProgress(value: progress)
            HStack(spacing: 6) {
                PlainBadge(text: (record["trackedDownloadState"].string ?? record["status"].string ?? "").humanizedKey,
                           tone: hasWarning ? .danger : .accent)
                if let q = record["quality"]["quality"]["name"].string { PlainBadge(text: q) }
                Spacer()
                Text(verbatim: [percentText, record["timeleft"].string].compactMap { $0 }.joined(separator: " · "))
                    .font(.caption2.monospacedDigit()).foregroundStyle(Theme.textMuted)
            }
            ForEach(record["statusMessages"].list.flatMap { $0["messages"].list.map(\.text) }, id: \.self) { m in
                Text(verbatim: m).font(.caption2).foregroundStyle(.orange)
            }
            if let m = record["errorMessage"].string?.nilIfBlank { Text(verbatim: m).font(.caption2).foregroundStyle(.red) }
        }
        .padding(.vertical, 3)
    }

    private var itemTitle: String {
        if let t = record["movie"]["title"].string { return t }
        if let t = record["series"]["title"].string {
            let ep = record["episode"]
            if let s = ep["seasonNumber"].int, let e = ep["episodeNumber"].int { return t + String(format: " S%02dE%02d", s, e) }
            return t
        }
        if let a = record["artist"]["artistName"].string { return a + " – " + (record["album"]["title"].string ?? "") }
        return record["title"].string ?? ""
    }

    private var progress: Double {
        let size = record["size"].double ?? 0
        guard size > 0 else { return 0 }
        return 1 - (record["sizeleft"].double ?? size) / size
    }

    private var percentText: String { "\(Int((progress * 100).rounded()))%" }

    private var hasWarning: Bool {
        let s = (record["trackedDownloadStatus"].string ?? "").lowercased()
        return s == "warning" || s == "error"
    }
}

/// Paged history (Radarr/Sonarr/Lidarr/Prowlarr).
struct ArrHistoryList: View {
    let client: ArrClient
    @State private var records: [JSONValue] = []
    @State private var total = 0
    @State private var page = 0
    @State private var error: Error?
    @State private var loading = false

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            ForEach(records, id: \.rowID) { HistoryRow(record: $0) }
            if records.count < total {
                Button { Task { await more() } } label: { Text("manage.load_more") }.disabled(loading)
            } else if !loading, records.isEmpty, error == nil {
                EmptyRow(text: "manage.empty")
            }
            if loading { LoadingView() }
        }
        .manageBackground()
        .navigationTitle(Text("manage.history"))
        .navigationBarTitleDisplayMode(.inline)
        .task { if records.isEmpty { await more() } }
        .refreshable {
            records = []
            page = 0
            await more()
        }
    }

    private func more() async {
        loading = true
        defer { loading = false }
        do {
            var query: [URLQueryItem] = [.q("page", page + 1), .q("pageSize", 50), .q("sortKey", "date"), .q("sortDirection", "descending")]
            if client.service.hasLibrary { query += ArrFlavor(client.service).includeQuery }
            let result = try await client.get("history", query: query)
            records += result["records"].list
            total = result["totalRecords"].int ?? records.count
            page += 1
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// Releases that will not be grabbed again.
struct ArrBlocklistView: View {
    let client: ArrClient
    @State private var records: [JSONValue]?
    @State private var error: Error?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let records {
                ForEach(records, id: \.rowID) { r in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: r["sourceTitle"].string ?? "").font(.footnote).lineLimit(3)
                        Text(verbatim: [r["quality"]["quality"]["name"].string, r["message"].string, ArrDate.short(r["date"].string)]
                            .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.caption2).foregroundStyle(Theme.textFaint)
                    }
                    .swipeActions {
                        Button(role: .destructive) { Task { await remove(r) } } label: { Label("manage.delete", systemImage: "trash") }
                    }
                }
                if records.isEmpty { EmptyRow(text: "manage.empty") }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        do {
            let result = try await client.get("blocklist", query: [.q("page", 1), .q("pageSize", 100), .q("sortKey", "date"), .q("sortDirection", "descending")])
            records = result["records"].list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func remove(_ r: JSONValue) async {
        do {
            try await client.send("DELETE", "blocklist/\(r["id"].text)")
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }
}

// MARK: Wanted

/// Missing and cutoff-unmet items with search.
struct ArrWantedView: View {
    let client: ArrClient
    @State private var cutoff = false
    @State private var records: [JSONValue]?
    @State private var total = 0
    @State private var error: Error?
    @State private var notice: ManageNotice?

    private var flavor: ArrFlavor { ArrFlavor(client.service) }

    var body: some View {
        List {
            Section {
                Picker(selection: $cutoff) {
                    Text("manage.missing").tag(false)
                    Text("manage.cutoff_unmet").tag(true)
                } label: { EmptyView() }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
            }
            if let error { ArrErrorLine(error: error) }
            if let records {
                Section {
                    ForEach(records, id: \.rowID) { r in
                        Button { Task { await search([r["id"].int ?? 0]) } } label: {
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(verbatim: title(r)).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text)
                                    Text(verbatim: subtitle(r)).font(.caption).foregroundStyle(Theme.textMuted)
                                }
                                Spacer()
                                Image(systemName: "magnifyingglass").foregroundStyle(Theme.accent)
                            }
                        }
                    }
                    if records.isEmpty { EmptyRow(text: "manage.nothing_wanted") }
                } header: {
                    Text("manage.count_items \(total)")
                }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.wanted"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { Task { await searchAll() } } label: { Label("manage.search_all", systemImage: "magnifyingglass") }
            }
        }
        .task(id: cutoff) { await load() }
        .refreshable { await load() }
        .manageNotice($notice)
    }

    private func title(_ r: JSONValue) -> String {
        if let t = r["series"]["title"].string { return t }
        if let a = r["artist"]["artistName"].string { return a }
        return r["title"].string ?? ""
    }

    private func subtitle(_ r: JSONValue) -> String {
        switch client.service {
        case .sonarr:
            return String(format: "S%02dE%02d · ", r["seasonNumber"].int ?? 0, r["episodeNumber"].int ?? 0)
                + (r["title"].string ?? "") + " · " + ArrDate.short(r["airDateUtc"].string)
        case .lidarr:
            return (r["title"].string ?? "") + " · " + String(r["releaseDate"].string?.prefix(10) ?? "")
        default:
            return [r["year"].int.map { String($0) }, r["status"].string?.humanizedKey].compactMap { $0 }.joined(separator: " · ")
        }
    }

    private func load() async {
        records = nil
        do {
            var query: [URLQueryItem] = [.q("page", 1), .q("pageSize", 100), .q("monitored", "true")]
            if client.service == .sonarr { query.append(.q("includeSeries", "true")) }
            if client.service == .lidarr { query.append(.q("includeArtist", "true")) }
            let result = try await client.get(cutoff ? "wanted/cutoff" : "wanted/missing", query: query)
            records = result["records"].list
            total = result["totalRecords"].int ?? records?.count ?? 0
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func search(_ ids: [Int]) async {
        let command = flavor.searchWanted(ids)
        do {
            try await client.command(command.0, command.1)
            notice = ManageNotice(text: "manage.command_started")
        } catch is CancellationError {} catch { self.error = error }
    }

    private func searchAll() async {
        do {
            try await client.command(flavor.searchAll(cutoff: cutoff))
            notice = ManageNotice(text: "manage.command_started")
        } catch is CancellationError {} catch { self.error = error }
    }
}
