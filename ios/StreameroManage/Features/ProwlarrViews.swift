import SwiftUI

/// Search every Prowlarr indexer and grab a release.
struct ProwlarrSearchView: View {
    let client: ArrClient
    @State private var query = ""
    @State private var type = "search"
    @State private var results: [JSONValue]?
    @State private var error: Error?
    @State private var searching = false
    @State private var sort: ReleaseSort = .seeders
    @State private var pending: JSONValue?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            Section {
                Picker(selection: $type) {
                    Text("manage.search_type.all").tag("search")
                    Text("manage.search_type.movie").tag("movie")
                    Text("manage.search_type.tv").tag("tvsearch")
                    Text("manage.search_type.music").tag("music")
                    Text("manage.search_type.book").tag("book")
                } label: { Text("manage.search_type") }
                Picker(selection: $sort) {
                    ForEach(ReleaseSort.allCases) { s in Text(s.label).tag(s) }
                } label: { Text("manage.sort") }
            }
            if let error { ArrErrorLine(error: error) }
            if searching { LoadingView() }
            if let results {
                Section {
                    ForEach(ReleaseSort.sorted(results, by: sort), id: \.self) { r in
                        Button { pending = r } label: { ReleaseRow(release: r) }.buttonStyle(.plain)
                    }
                    if results.isEmpty { EmptyRow(text: "manage.no_releases") }
                } header: { Text("manage.count_items \(results.count)") }
            }
        }
        .manageBackground()
        .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always))
        .onSubmit(of: .search) { Task { await search() } }
        .navigationTitle(Text("manage.search"))
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog("manage.grab_confirm", isPresented: Binding(get: { pending != nil }, set: { if !$0 { pending = nil } }), titleVisibility: .visible) {
            Button("manage.grab") { if let r = pending { Task { await grab(r) } } }
        } message: { Text(verbatim: pending?["title"].string ?? "") }
        .manageNotice($notice)
    }

    private func search() async {
        let term = query.trimmingCharacters(in: .whitespaces)
        guard !term.isEmpty else { return }
        searching = true
        defer { searching = false }
        do {
            results = try await client.get("search", query: [.q("query", term), .q("type", type), .q("limit", 100)], timeout: 180).list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func grab(_ r: JSONValue) async {
        do {
            try await client.send("POST", "search", json: ["guid": r["guid"], "indexerId": r["indexerId"]])
            notice = ManageNotice(text: "manage.grabbed")
        } catch is CancellationError {} catch { self.error = error }
    }
}

/// Queries, grabs and failures per indexer.
struct ProwlarrStatsView: View {
    let client: ArrClient
    @State private var stats: JSONValue?
    @State private var error: Error?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let stats {
                let indexers = stats["indexers"].list.sorted { ($0["numberOfGrabs"].int ?? 0) > ($1["numberOfGrabs"].int ?? 0) }
                let maxQueries = max(1, indexers.map { $0["numberOfQueries"].int ?? 0 }.max() ?? 1)
                ForEach(indexers, id: \.self) { s in
                    VStack(alignment: .leading, spacing: 6) {
                        HStack {
                            Text(verbatim: s["indexerName"].string ?? "").font(.subheadline.weight(.semibold))
                            Spacer()
                            if let ms = s["averageResponseTime"].int { Text(verbatim: "\(ms) ms").font(.caption2.monospacedDigit()).foregroundStyle(Theme.textFaint) }
                        }
                        ThinProgress(value: Double(s["numberOfQueries"].int ?? 0) / Double(maxQueries))
                        HStack(spacing: 14) {
                            stat("manage.stat.queries", s["numberOfQueries"].int ?? 0)
                            stat("manage.stat.grabs", s["numberOfGrabs"].int ?? 0)
                            stat("manage.stat.failed", (s["numberOfFailedQueries"].int ?? 0) + (s["numberOfFailedGrabs"].int ?? 0))
                        }
                    }
                    .padding(.vertical, 3)
                }
                if indexers.isEmpty { EmptyRow(text: "manage.empty") }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.stats"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    private func stat(_ title: LocalizedStringKey, _ value: Int) -> some View {
        HStack(spacing: 4) {
            Text(title).font(.caption2).foregroundStyle(Theme.textMuted)
            Text(verbatim: "\(value)").font(.caption.weight(.semibold).monospacedDigit())
        }
    }

    private func load() async {
        do {
            stats = try await client.get("indexerstats")
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }
}
