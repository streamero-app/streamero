import SwiftUI

/// The library of Radarr (movies), Sonarr (series) or Lidarr (artists): posters with
/// search, sort and filter.
struct ArrLibraryView: View {
    let client: ArrClient
    @State private var items: [JSONValue]?
    @State private var profiles: [Int: String] = [:]
    @State private var error: Error?
    @State private var query = ""
    @State private var sort: Sort = .title
    @State private var filter: Filter = .all

    enum Sort: String, CaseIterable, Identifiable {
        case title, added, year, size
        var id: String { rawValue }
        var label: LocalizedStringKey {
            switch self {
            case .title: "manage.sort.title"
            case .added: "manage.sort.added"
            case .year: "manage.sort.year"
            case .size: "manage.sort.size"
            }
        }
    }

    enum Filter: String, CaseIterable, Identifiable {
        case all, monitored, unmonitored, missing, complete
        var id: String { rawValue }
        var label: LocalizedStringKey {
            switch self {
            case .all: "manage.filter.all"
            case .monitored: "manage.filter.monitored"
            case .unmonitored: "manage.filter.unmonitored"
            case .missing: "manage.filter.missing"
            case .complete: "manage.filter.complete"
            }
        }
    }

    private var flavor: ArrFlavor { ArrFlavor(client.service) }

    var body: some View {
        ScrollView {
            if let error { ArrErrorState(error: error) { await load() } }
            if let items {
                let shown = visible(items)
                Text("manage.count_items \(shown.count)")
                    .font(.caption).foregroundStyle(Theme.textFaint)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 16)
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 104), spacing: 12)], spacing: 16) {
                    ForEach(shown, id: \.rowID) { item in
                        NavigationLink {
                            ArrItemDetailView(client: client, id: item["id"].int ?? 0)
                        } label: {
                            LibraryCell(client: client, item: item, service: client.service)
                        }
                        .buttonStyle(CardPressStyle())
                    }
                }
                .padding(.horizontal, 16).padding(.bottom, 24)
                if shown.isEmpty { EmptyRow(text: "manage.empty") }
            } else if error == nil {
                LoadingView()
            }
        }
        .background(Theme.background)
        .searchable(text: $query)
        .navigationTitle(Text("manage.library"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Picker(selection: $sort) {
                        ForEach(Sort.allCases) { s in Text(s.label).tag(s) }
                    } label: { Text("manage.sort") }
                    Picker(selection: $filter) {
                        ForEach(Filter.allCases) { f in Text(f.label).tag(f) }
                    } label: { Text("manage.filter") }
                } label: {
                    Image(systemName: "line.3.horizontal.decrease.circle")
                }
                .accessibilityLabel(Text("manage.filter"))
            }
            ToolbarItem(placement: .primaryAction) {
                NavigationLink { ArrAddView(client: client) } label: { Image(systemName: "plus") }
                    .accessibilityLabel(Text("manage.add_new"))
            }
        }
        .task { await load() }
        .refreshable { await load() }
    }

    private func visible(_ all: [JSONValue]) -> [JSONValue] {
        let key = flavor.titleKey
        var out = all.filter { item in
            switch filter {
            case .all: return true
            case .monitored: return item["monitored"].bool == true
            case .unmonitored: return item["monitored"].bool != true
            case .missing: return item["monitored"].bool == true && !ArrFlavor.isComplete(item, service: client.service)
            case .complete: return ArrFlavor.isComplete(item, service: client.service)
            }
        }
        if !query.isEmpty {
            out = out.filter { ($0[key].string ?? "").localizedCaseInsensitiveContains(query) }
        }
        switch sort {
        case .title:
            out.sort { ($0["sortTitle"].string ?? $0[key].string ?? "") < ($1["sortTitle"].string ?? $1[key].string ?? "") }
        case .added:
            out.sort { ($0["added"].string ?? "") > ($1["added"].string ?? "") }
        case .year:
            out.sort { ($0["year"].int ?? 0) > ($1["year"].int ?? 0) }
        case .size:
            out.sort { (ArrFlavor.sizeOnDisk($0).int64 ?? 0) > (ArrFlavor.sizeOnDisk($1).int64 ?? 0) }
        }
        return out
    }

    private func load() async {
        do {
            items = try await client.get(flavor.items, timeout: 120).list
            error = nil
        } catch { self.error = error }
    }
}

private struct LibraryCell: View {
    let client: ArrClient
    let item: JSONValue
    let service: ArrService

    var body: some View {
        let title = item[ArrFlavor(service).titleKey].string ?? ""
        VStack(alignment: .leading, spacing: 6) {
            ArrPoster(client: client, item: item, title: title)
                .aspectRatio(2 / 3, contentMode: .fit)
                .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                .overlay(alignment: .bottom) {
                    Rectangle().fill(statusColor).frame(height: 3)
                }
                .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                .opacity(item["monitored"].bool == false ? 0.55 : 1)
            Text(verbatim: title).font(.caption.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
            if let p = ArrFlavor.progress(item, service: service) {
                Text(verbatim: "\(p.have)/\(p.total)").font(.caption2.monospacedDigit()).foregroundStyle(Theme.textFaint)
            } else if let year = item["year"].int, year > 0 {
                Text(verbatim: String(year)).font(.caption2).foregroundStyle(Theme.textFaint)
            }
        }
    }

    private var statusColor: Color {
        if item["monitored"].bool != true { return Theme.textFaint }
        return ArrFlavor.isComplete(item, service: service) ? .green : Theme.accent
    }
}

// MARK: Detail

/// One movie, series or artist: state, actions, files, seasons or albums, history.
struct ArrItemDetailView: View {
    let client: ArrClient
    let id: Int
    @State private var item: JSONValue?
    @State private var files: [JSONValue] = []
    @State private var children: [JSONValue] = []
    @State private var history: [JSONValue] = []
    @State private var profiles: [JSONValue] = []
    @State private var error: Error?
    @State private var notice: ManageNotice?
    @State private var editing = false
    @State private var confirmDelete = false
    @State private var deleteFiles = false
    @State private var pendingFileDelete: JSONValue?
    @Environment(\.dismiss) private var dismiss

    private var flavor: ArrFlavor { ArrFlavor(client.service) }

    var body: some View {
        List {
            if let error { Section { ArrErrorLine(error: error) } }
            if let item {
                header(item)
                actions(item)
                if let overview = item["overview"].string?.nilIfBlank {
                    Section { Text(verbatim: overview).font(.subheadline).foregroundStyle(Theme.textMuted) }
                }
                info(item)
                if client.service == .sonarr { seasons(item) }
                if client.service == .lidarr { albums(item) }
                filesSection
                historySection
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: item?[flavor.titleKey].string ?? ""))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button { editing = true } label: { Label("manage.edit", systemImage: "pencil") }
                    Button(role: .destructive) { confirmDelete = true } label: { Label("manage.delete", systemImage: "trash") }
                } label: { Image(systemName: "ellipsis.circle") }
            }
        }
        .sheet(isPresented: $editing, onDismiss: { Task { await load() } }) {
            if let item {
                NavigationStack { ArrItemEditView(client: client, item: item) }
            }
        }
        .confirmationDialog("manage.delete_confirm", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("manage.delete_keep_files", role: .destructive) { Task { await delete(files: false) } }
            Button("manage.delete_with_files", role: .destructive) { Task { await delete(files: true) } }
        } message: {
            Text(verbatim: item?[flavor.titleKey].string ?? "")
        }
        .confirmationDialog("manage.delete_file_confirm", isPresented: Binding(get: { pendingFileDelete != nil }, set: { if !$0 { pendingFileDelete = nil } }), titleVisibility: .visible) {
            Button("manage.delete", role: .destructive) {
                if let f = pendingFileDelete { Task { await deleteFile(f) } }
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .manageNotice($notice)
    }

    @ViewBuilder private func header(_ item: JSONValue) -> some View {
        Section {
            HStack(alignment: .top, spacing: 14) {
                ArrPoster(client: client, item: item, title: item[flavor.titleKey].string ?? "")
                    .frame(width: 104, height: 156)
                    .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                VStack(alignment: .leading, spacing: 6) {
                    Text(verbatim: item[flavor.titleKey].string ?? "").headFont(18).foregroundStyle(Theme.text)
                    let meta = [item["year"].int.map { String($0) }, item["network"].string, item["status"].string?.humanizedKey,
                                item["artistType"].string, item["runtime"].int.map { "\($0) min" }]
                        .compactMap { $0 }.filter { !$0.isEmpty && $0 != "0" }
                    Text(verbatim: meta.joined(separator: " · ")).font(.caption).foregroundStyle(Theme.textMuted)
                    HStack(spacing: 6) {
                        if item["monitored"].bool == true {
                            TagBadge(text: "manage.filter.monitored", tone: .accent)
                        } else {
                            TagBadge(text: "manage.filter.unmonitored")
                        }
                        if ArrFlavor.isComplete(item, service: client.service) {
                            TagBadge(text: "manage.filter.complete", tone: .success)
                        } else {
                            TagBadge(text: "manage.filter.missing", tone: .danger)
                        }
                    }
                    if let p = ArrFlavor.progress(item, service: client.service), p.total > 0 {
                        ThinProgress(value: Double(p.have) / Double(p.total))
                        Text(verbatim: "\(p.have)/\(p.total)").font(.caption2.monospacedDigit()).foregroundStyle(Theme.textFaint)
                    }
                }
            }
            .padding(.vertical, 4)
        }
        .listRowBackground(Color.clear)
    }

    @ViewBuilder private func actions(_ item: JSONValue) -> some View {
        Section {
            Toggle(isOn: Binding(get: { item["monitored"].bool ?? false }, set: { on in Task { await setMonitored(on) } })) {
                Label("manage.filter.monitored", systemImage: "bookmark")
            }
            Button { Task { await run(flavor.search(id)) } } label: { Label("manage.search_auto", systemImage: "magnifyingglass") }
            if client.service == .radarr {
                NavigationLink {
                    ReleaseSearchView(client: client, query: [.q("movieId", id)])
                } label: { Label("manage.search_interactive", systemImage: "person.fill.questionmark") }
            }
            Button { Task { await run(flavor.refresh(id)) } } label: { Label("manage.refresh", systemImage: "arrow.clockwise") }
            if let rescan = flavor.rescan(id) {
                Button { Task { await run(rescan) } } label: { Label("manage.rescan", systemImage: "folder.badge.gearshape") }
            }
        }
    }

    @ViewBuilder private func info(_ item: JSONValue) -> some View {
        Section {
            let profile = profiles.first { $0["id"].int == item["qualityProfileId"].int }?["name"].string
            if let profile { InfoRow(title: "manage.quality_profile", value: profile) }
            if let path = item["path"].string { InfoRow(title: "manage.path", value: path) }
            let size = ArrFlavor.sizeOnDisk(item)
            if (size.int64 ?? 0) > 0 { InfoRow(title: "manage.size_on_disk", value: byteText(size)) }
            if let a = item["minimumAvailability"].string { InfoRow(title: "manage.minimum_availability", value: a.humanizedKey) }
            if let t = item["seriesType"].string { InfoRow(title: "manage.series_type", value: t.humanizedKey) }
            if let added = item["added"].string { InfoRow(title: "manage.added", value: ArrDate.short(added)) }
        }
    }

    @ViewBuilder private func seasons(_ item: JSONValue) -> some View {
        Section {
            ForEach(item["seasons"].list.sorted { ($0["seasonNumber"].int ?? 0) > ($1["seasonNumber"].int ?? 0) }, id: \.self) { season in
                let number = season["seasonNumber"].int ?? 0
                NavigationLink {
                    SonarrSeasonView(client: client, seriesId: id, seasonNumber: number)
                } label: {
                    HStack {
                        Image(systemName: season["monitored"].bool == true ? "bookmark.fill" : "bookmark")
                            .foregroundStyle(season["monitored"].bool == true ? Theme.accent : Theme.textFaint)
                        if number == 0 { Text("manage.specials") } else { Text("manage.season \(number)") }
                        Spacer()
                        let s = season["statistics"]
                        Text(verbatim: "\(s["episodeFileCount"].int ?? 0)/\(s["episodeCount"].int ?? 0)")
                            .font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
                    }
                }
            }
        } header: { Text("manage.seasons") }
    }

    @ViewBuilder private func albums(_ item: JSONValue) -> some View {
        Section {
            ForEach(children, id: \.rowID) { album in
                NavigationLink {
                    LidarrAlbumView(client: client, album: album)
                } label: {
                    HStack {
                        Image(systemName: album["monitored"].bool == true ? "bookmark.fill" : "bookmark")
                            .foregroundStyle(album["monitored"].bool == true ? Theme.accent : Theme.textFaint)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: album["title"].string ?? "")
                            Text(verbatim: [String(album["releaseDate"].string?.prefix(4) ?? ""), album["albumType"].string ?? ""]
                                .filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.caption).foregroundStyle(Theme.textFaint)
                        }
                        Spacer()
                        let s = album["statistics"]
                        Text(verbatim: "\(s["trackFileCount"].int ?? 0)/\(s["totalTrackCount"].int ?? 0)")
                            .font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
                    }
                }
            }
            if children.isEmpty { EmptyRow(text: "manage.empty") }
        } header: { Text("manage.albums") }
    }

    @ViewBuilder private var filesSection: some View {
        if !files.isEmpty {
            Section {
                ForEach(files, id: \.rowID) { f in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: f["relativePath"].string ?? f["path"].string ?? "").font(.footnote.monospaced()).lineLimit(3)
                        Text(verbatim: [f["quality"]["quality"]["name"].string, byteText(f["size"]), f["mediaInfo"]["videoCodec"].string,
                                        f["mediaInfo"]["audioCodec"].string].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.caption).foregroundStyle(Theme.textMuted)
                    }
                    .swipeActions {
                        Button(role: .destructive) { pendingFileDelete = f } label: { Label("manage.delete", systemImage: "trash") }
                    }
                }
            } header: { Text("manage.files") }
        }
    }

    @ViewBuilder private var historySection: some View {
        if !history.isEmpty {
            Section {
                ForEach(history.prefix(25), id: \.rowID) { h in HistoryRow(record: h) }
            } header: { Text("manage.history") }
        }
    }

    private func load() async {
        do {
            let value = try await client.get("\(flavor.items)/\(id)")
            item = value
            error = nil
        } catch {
            self.error = error
            return
        }
        profiles = (try? await client.get("qualityprofile").list) ?? []
        let byItem = [URLQueryItem.q(flavor.idParam, id)]
        if client.service == .lidarr {
            children = ((try? await client.get("album", query: byItem).list) ?? [])
                .sorted { ($0["releaseDate"].string ?? "") > ($1["releaseDate"].string ?? "") }
        }
        if client.service == .radarr {
            files = (try? await client.get(flavor.files, query: byItem).list) ?? []
        } else if client.service == .sonarr {
            files = (try? await client.get(flavor.files, query: byItem).list) ?? []
        }
        let raw = (try? await client.get(flavor.history, query: byItem)) ?? .null
        history = (raw.array ?? raw["records"].list).sorted { ($0["date"].string ?? "") > ($1["date"].string ?? "") }
    }

    private func run(_ command: (String, [String: JSONValue])) async {
        do {
            try await client.command(command.0, command.1)
            notice = ManageNotice(text: "manage.command_started")
        } catch {
            self.error = error
        }
    }

    private func setMonitored(_ on: Bool) async {
        guard var body = item else { return }
        body["monitored"] = .bool(on)
        do {
            item = try await client.send("PUT", "\(flavor.items)/\(id)", json: body)
            if item?.isNull == true { item = body }
        } catch { self.error = error }
    }

    private func delete(files: Bool) async {
        do {
            try await client.send("DELETE", "\(flavor.items)/\(id)", query: [.q("deleteFiles", files), .q(flavor.exclusionParam, false)])
            dismiss()
        } catch { self.error = error }
    }

    private func deleteFile(_ f: JSONValue) async {
        do {
            try await client.send("DELETE", "\(flavor.files)/\(f["id"].text)")
            await load()
        } catch { self.error = error }
    }
}

/// A history record (grabbed, imported, failed, deleted…).
struct HistoryRow: View {
    let record: JSONValue

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: icon).foregroundStyle(color).frame(width: 20)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: record.firstString("sourceTitle") ?? record["data"]["query"].string ?? record["eventType"].text)
                    .font(.footnote).lineLimit(2)
                Text(verbatim: [record["eventType"].string?.humanizedKey, record["quality"]["quality"]["name"].string,
                                ArrDate.short(record["date"].string)].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption2).foregroundStyle(Theme.textFaint)
            }
        }
    }

    private var event: String { (record["eventType"].string ?? "").lowercased() }

    private var icon: String {
        if event.contains("grab") { return "arrow.down.circle" }
        if event.contains("import") { return "tray.and.arrow.down" }
        if event.contains("fail") { return "exclamationmark.triangle" }
        if event.contains("delete") { return "trash" }
        if event.contains("rename") { return "pencil" }
        if event.contains("query") || event.contains("rss") { return "magnifyingglass" }
        return "clock"
    }

    private var color: Color {
        if event.contains("fail") { return .red }
        if event.contains("import") { return .green }
        return Theme.accent
    }
}

// MARK: Edit

/// Monitoring, profile, path and tags of a library item.
struct ArrItemEditView: View {
    let client: ArrClient
    @State private var item: JSONValue
    private let originalPath: String
    @State private var profiles: [JSONValue] = []
    @State private var metadataProfiles: [JSONValue] = []
    @State private var tags: [JSONValue] = []
    @State private var error: Error?
    @State private var busy = false
    @Environment(\.dismiss) private var dismiss

    init(client: ArrClient, item: JSONValue) {
        self.client = client
        _item = State(initialValue: item)
        originalPath = item["path"].string ?? ""
    }

    private var flavor: ArrFlavor { ArrFlavor(client.service) }

    private func binding(_ key: String) -> Binding<JSONValue> {
        Binding(get: { item[key] }, set: { item[key] = $0 })
    }

    var body: some View {
        Form {
            Section {
                Toggle(isOn: Binding(get: { item["monitored"].bool ?? false }, set: { item["monitored"] = .bool($0) })) { Text("manage.filter.monitored") }
                Picker(selection: binding("qualityProfileId")) {
                    ForEach(idOptions(profiles)) { o in Text(verbatim: o.name).tag(o.value) }
                } label: { Text("manage.quality_profile") }
                if client.service == .lidarr {
                    Picker(selection: binding("metadataProfileId")) {
                        ForEach(idOptions(metadataProfiles)) { o in Text(verbatim: o.name).tag(o.value) }
                    } label: { Text("manage.metadata_profile") }
                }
                if client.service == .radarr {
                    Picker(selection: binding("minimumAvailability")) {
                        ForEach(apiOptions(["announced", "inCinemas", "released"])) { o in Text(verbatim: o.name).tag(o.value) }
                    } label: { Text("manage.minimum_availability") }
                }
                if client.service == .sonarr {
                    Picker(selection: binding("seriesType")) {
                        ForEach(apiOptions(["standard", "daily", "anime"])) { o in Text(verbatim: o.name).tag(o.value) }
                    } label: { Text("manage.series_type") }
                    Toggle(isOn: Binding(get: { item["seasonFolder"].bool ?? false }, set: { item["seasonFolder"] = .bool($0) })) { Text("manage.season_folder") }
                }
            }
            Section {
                VStack(alignment: .leading, spacing: 4) {
                    Text("manage.path").font(.caption).foregroundStyle(Theme.textMuted)
                    TextField(text: Binding(get: { item["path"].string ?? "" }, set: { item["path"] = .string($0) })) { Text("manage.path") }
                        .font(.body.monospaced()).textInputAutocapitalization(.never).autocorrectionDisabled()
                }
                if (item["path"].string ?? "") != originalPath {
                    Text("manage.path_moves_files").font(.caption).foregroundStyle(Theme.textFaint)
                }
                MultiSelectRow(title: Loc.string("manage.tags"), options: tagOptions(tags), selection: arrayBinding(binding("tags"))) {
                    Text("manage.tags")
                }
            }
            Section {
                ArrErrorLine(error: error)
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.edit"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
            ToolbarItem(placement: .confirmationAction) {
                Button { Task { await save() } } label: { if busy { ProgressView() } else { Text("save") } }.disabled(busy)
            }
        }
        .task {
            profiles = (try? await client.get("qualityprofile").list) ?? []
            tags = (try? await client.get("tag").list) ?? []
            if client.service == .lidarr { metadataProfiles = (try? await client.get("metadataprofile").list) ?? [] }
        }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        let moved = (item["path"].string ?? "") != originalPath
        do {
            try await client.send("PUT", "\(flavor.items)/\(item["id"].text)", query: moved ? [.q("moveFiles", true)] : [], json: item)
            dismiss()
        } catch { self.error = error }
    }
}
