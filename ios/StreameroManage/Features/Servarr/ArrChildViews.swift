import SwiftUI

/// The episodes of one Sonarr season: monitoring, automatic and interactive search.
struct SonarrSeasonView: View {
    let client: ArrClient
    let seriesId: Int
    let seasonNumber: Int
    @State private var series: JSONValue?
    @State private var episodes: [JSONValue]?
    @State private var error: Error?
    @State private var notice: ManageNotice?
    @State private var interactiveEpisode: Int?

    private var seasonMonitored: Bool {
        series?["seasons"].list.first { $0["seasonNumber"].int == seasonNumber }?["monitored"].bool ?? false
    }

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            Section {
                Toggle(isOn: Binding(get: { seasonMonitored }, set: { on in Task { await monitorSeason(on) } })) {
                    Label("manage.monitor_season", systemImage: "bookmark")
                }
                Button {
                    Task { await run("SeasonSearch", ["seriesId": .number(seriesId), "seasonNumber": .number(seasonNumber)]) }
                } label: { Label("manage.search_season", systemImage: "magnifyingglass") }
                NavigationLink {
                    ReleaseSearchView(client: client, query: [.q("seriesId", seriesId), .q("seasonNumber", seasonNumber)])
                } label: { Label("manage.search_interactive", systemImage: "person.fill.questionmark") }
            }
            if let episodes {
                Section {
                    ForEach(episodes, id: \.rowID) { ep in
                        ArrEpisodeRow(episode: ep) { on in Task { await monitor(ep, on) } }
                            .swipeActions(edge: .trailing) {
                                Button {
                                    Task { await run("EpisodeSearch", ["episodeIds": .ints([ep["id"].int ?? 0])]) }
                                } label: { Label("manage.search_auto", systemImage: "magnifyingglass") }
                                .tint(Theme.accent)
                            }
                            .swipeActions(edge: .leading) {
                                Button {
                                    interactiveEpisode = ep["id"].int
                                } label: { Label("manage.search_interactive", systemImage: "person.fill.questionmark") }
                                .tint(.indigo)
                            }
                    }
                    if episodes.isEmpty { EmptyRow(text: "manage.empty") }
                } header: { Text("manage.episodes") }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(seasonNumber == 0 ? Text("manage.specials") : Text("manage.season \(seasonNumber)"))
        .navigationDestination(item: $interactiveEpisode) { episodeId in
            ReleaseSearchView(client: client, query: [.q("episodeId", episodeId)])
        }
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            series = try await client.get("series/\(seriesId)")
            episodes = try await client.get("episode", query: [.q("seriesId", seriesId), .q("seasonNumber", seasonNumber)]).list
                .sorted { ($0["episodeNumber"].int ?? 0) < ($1["episodeNumber"].int ?? 0) }
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func run(_ name: String, _ params: [String: JSONValue]) async {
        do {
            try await client.command(name, params)
            notice = ManageNotice(text: "manage.command_started")
        } catch is CancellationError {} catch { self.error = error }
    }

    private func monitor(_ ep: JSONValue, _ on: Bool) async {
        do {
            try await client.send("PUT", "episode/monitor", json: ["episodeIds": .ints([ep["id"].int ?? 0]), "monitored": .bool(on)])
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }

    private func monitorSeason(_ on: Bool) async {
        guard var body = series else { return }
        body["seasons"] = .array(body["seasons"].list.map { s in
            var s = s
            if s["seasonNumber"].int == seasonNumber { s["monitored"] = .bool(on) }
            return s
        })
        do {
            try await client.send("PUT", "series/\(seriesId)", json: body)
            await load()
        } catch is CancellationError {} catch { self.error = error }
    }
}

private struct ArrEpisodeRow: View {
    let episode: JSONValue
    let setMonitored: (Bool) -> Void

    var body: some View {
        HStack(spacing: 12) {
            Button {
                setMonitored(!(episode["monitored"].bool ?? false))
            } label: {
                Image(systemName: episode["monitored"].bool == true ? "bookmark.fill" : "bookmark")
                    .foregroundStyle(episode["monitored"].bool == true ? Theme.accent : Theme.textFaint)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(Text("manage.filter.monitored"))
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: "\(episode["episodeNumber"].int ?? 0). \(episode["title"].string ?? "")").font(.subheadline).lineLimit(2)
                Text(verbatim: ArrDate.short(episode["airDateUtc"].string ?? episode["airDate"].string))
                    .font(.caption2).foregroundStyle(Theme.textFaint)
            }
            Spacer()
            Image(systemName: episode["hasFile"].bool == true ? "checkmark.circle.fill" : "circle.dashed")
                .foregroundStyle(episode["hasFile"].bool == true ? .green : Theme.textFaint)
        }
    }
}

/// One Lidarr album: tracks, monitoring and searches.
struct LidarrAlbumView: View {
    let client: ArrClient
    @State var album: JSONValue
    @State private var tracks: [JSONValue] = []
    @State private var error: Error?
    @State private var notice: ManageNotice?

    private var albumId: Int { album["id"].int ?? 0 }

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            Section {
                HStack(alignment: .top, spacing: 14) {
                    ArrPoster(client: client, item: album, coverType: "cover", title: album["title"].string ?? "")
                        .frame(width: 96, height: 96)
                        .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                    VStack(alignment: .leading, spacing: 4) {
                        Text(verbatim: album["title"].string ?? "").headFont(16)
                        Text(verbatim: [album["albumType"].string, album["releaseDate"].string.map { String($0.prefix(10)) }]
                            .compactMap { $0 }.joined(separator: " · "))
                            .font(.caption).foregroundStyle(Theme.textMuted)
                    }
                }
            }
            .listRowBackground(Color.clear)
            Section {
                Toggle(isOn: Binding(get: { album["monitored"].bool ?? false }, set: { on in Task { await monitor(on) } })) {
                    Label("manage.filter.monitored", systemImage: "bookmark")
                }
                Button {
                    Task { await run() }
                } label: { Label("manage.search_auto", systemImage: "magnifyingglass") }
                NavigationLink {
                    ReleaseSearchView(client: client, query: [.q("albumId", albumId)])
                } label: { Label("manage.search_interactive", systemImage: "person.fill.questionmark") }
            }
            if !tracks.isEmpty {
                Section {
                    ForEach(tracks, id: \.rowID) { t in
                        HStack {
                            Text(verbatim: t["trackNumber"].text).font(.caption.monospacedDigit()).foregroundStyle(Theme.textFaint).frame(width: 28, alignment: .leading)
                            Text(verbatim: t["title"].string ?? "").font(.subheadline).lineLimit(1)
                            Spacer()
                            Image(systemName: t["hasFile"].bool == true ? "checkmark.circle.fill" : "circle.dashed")
                                .foregroundStyle(t["hasFile"].bool == true ? .green : Theme.textFaint)
                        }
                    }
                } header: { Text("manage.tracks") }
            }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: album["title"].string ?? ""))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .manageNotice($notice)
    }

    private func load() async {
        tracks = ((try? await client.get("track", query: [.q("albumId", albumId)]).list) ?? [])
            .sorted { ($0["absoluteTrackNumber"].int ?? 0) < ($1["absoluteTrackNumber"].int ?? 0) }
    }

    private func run() async {
        do {
            try await client.command("AlbumSearch", ["albumIds": .ints([albumId])])
            notice = ManageNotice(text: "manage.command_started")
        } catch is CancellationError {} catch { self.error = error }
    }

    private func monitor(_ on: Bool) async {
        do {
            try await client.send("PUT", "album/monitor", json: ["albumIds": .ints([albumId]), "monitored": .bool(on)])
            album["monitored"] = .bool(on)
        } catch is CancellationError {} catch { self.error = error }
    }
}

// MARK: Interactive search

/// Releases found by the indexers for an item; grab one to send it to the download client.
struct ReleaseSearchView: View {
    let client: ArrClient
    let query: [URLQueryItem]
    @State private var releases: [JSONValue]?
    @State private var error: Error?
    @State private var showRejected = false
    @State private var sort: ReleaseSort = .score
    @State private var pending: JSONValue?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorState(error: error) { await load() } }
            if let releases {
                let shown = ReleaseSort.sorted(releases.filter { showRejected || $0["rejected"].bool != true }, by: sort)
                Section {
                    Toggle("manage.show_rejected", isOn: $showRejected)
                    Picker(selection: $sort) {
                        ForEach(ReleaseSort.allCases) { s in Text(s.label).tag(s) }
                    } label: { Text("manage.sort") }
                }
                Section {
                    ForEach(shown, id: \.self) { r in
                        Button { pending = r } label: { ReleaseRow(release: r) }
                            .buttonStyle(.plain)
                    }
                    if shown.isEmpty { EmptyRow(text: "manage.no_releases") }
                } header: { Text("manage.count_items \(shown.count)") }
            } else if error == nil {
                VStack(spacing: 10) {
                    LoadingView()
                    Text("manage.searching_indexers").font(.footnote).foregroundStyle(Theme.textFaint)
                }
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.search_interactive"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .confirmationDialog("manage.grab_confirm", isPresented: Binding(get: { pending != nil }, set: { if !$0 { pending = nil } }), titleVisibility: .visible) {
            Button("manage.grab") { if let r = pending { Task { await grab(r) } } }
        } message: {
            Text(verbatim: pending?["title"].string ?? "")
        }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            releases = try await client.get("release", query: query, timeout: 180).list
            error = nil
        } catch is CancellationError {} catch { self.error = error }
    }

    private func grab(_ r: JSONValue) async {
        do {
            try await client.send("POST", "release", json: ["guid": r["guid"], "indexerId": r["indexerId"]])
            notice = ManageNotice(text: "manage.grabbed")
        } catch is CancellationError {} catch { self.error = error }
    }
}

enum ReleaseSort: String, CaseIterable, Identifiable {
    case score, size, seeders, age
    var id: String { rawValue }

    var label: LocalizedStringKey {
        switch self {
        case .score: "manage.sort.score"
        case .size: "manage.sort.size"
        case .seeders: "manage.sort.seeders"
        case .age: "manage.sort.age"
        }
    }

    /// The API order is already by preference; other sorts are descending (age ascending).
    static func sorted(_ list: [JSONValue], by sort: ReleaseSort) -> [JSONValue] {
        switch sort {
        case .score: return list
        case .size: return list.sorted { ($0["size"].int64 ?? 0) > ($1["size"].int64 ?? 0) }
        case .seeders: return list.sorted { ($0["seeders"].int ?? -1) > ($1["seeders"].int ?? -1) }
        case .age: return list.sorted { ($0["ageMinutes"].double ?? $0["age"].double ?? 0) < ($1["ageMinutes"].double ?? $1["age"].double ?? 0) }
        }
    }
}

struct ReleaseRow: View {
    let release: JSONValue

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(verbatim: release["title"].string ?? "").font(.footnote.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(3)
            HStack(spacing: 6) {
                if let q = release["quality"]["quality"]["name"].string { PlainBadge(text: q, tone: .accent) }
                if let score = release["customFormatScore"].int, score != 0 { PlainBadge(text: (score > 0 ? "+" : "") + "\(score)") }
                if release["rejected"].bool == true { TagBadge(text: "manage.rejected", tone: .danger) }
                PlainBadge(text: release["protocol"].string ?? "")
            }
            Text(verbatim: details).font(.caption2).foregroundStyle(Theme.textMuted)
            if release["rejected"].bool == true {
                ForEach(release["rejections"].list.map(\.text), id: \.self) { reason in
                    Text(verbatim: "• " + reason).font(.caption2).foregroundStyle(.red.opacity(0.85))
                }
            }
        }
        .padding(.vertical, 2)
    }

    private var details: String {
        var parts: [String] = []
        if let indexer = release["indexer"].string { parts.append(indexer) }
        parts.append(byteText(release["size"]))
        if let days = release["age"].int { parts.append("\(days) d") }
        if let s = release["seeders"].int { parts.append("↑\(s) ↓\(release["leechers"].int ?? 0)") }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}
