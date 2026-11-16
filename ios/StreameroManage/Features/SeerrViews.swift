import SwiftUI

/// Seerr artwork comes from TMDB's public image host.
func tmdbImage(_ path: String?, size: String = "w342") -> URL? {
    guard let path, !path.isEmpty else { return nil }
    return URL(string: "https://image.tmdb.org/t/p/\(size)\(path)")
}

/// Requests by state; approve, decline or delete.
struct SeerrRequestsView: View {
    let client: ArrClient
    @State private var filter = "pending"
    @State private var requests: [JSONValue]?
    @State private var error: Error?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            Section {
                Picker(selection: $filter) {
                    Text("manage.req.pending").tag("pending")
                    Text("manage.req.approved").tag("approved")
                    Text("manage.req.processing").tag("processing")
                    Text("manage.req.available").tag("available")
                    Text("manage.filter.all").tag("all")
                } label: { EmptyView() }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
            }
            if let error { ArrErrorLine(error: error) }
            if let requests {
                ForEach(requests, id: \.rowID) { r in
                    SeerrRequestRow(client: client, request: r)
                        .swipeActions(edge: .leading) {
                            if r["status"].int == 1 {
                                Button { Task { await act(r, "approve") } } label: { Label("manage.approve", systemImage: "checkmark") }.tint(.green)
                            }
                        }
                        .swipeActions(edge: .trailing) {
                            Button(role: .destructive) { Task { await delete(r) } } label: { Label("manage.delete", systemImage: "trash") }
                            if r["status"].int == 1 {
                                Button { Task { await act(r, "decline") } } label: { Label("manage.decline", systemImage: "xmark") }.tint(.orange)
                            }
                        }
                }
                if requests.isEmpty { EmptyRow(text: "manage.no_requests") }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.requests"))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: filter) { await load() }
        .refreshable { await load() }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            requests = try await client.get("request", query: [.q("take", 50), .q("skip", 0), .q("filter", filter), .q("sort", "added")])["results"].list
            error = nil
        } catch { self.error = error }
    }

    private func act(_ r: JSONValue, _ action: String) async {
        do {
            try await client.send("POST", "request/\(r["id"].text)/\(action)")
            notice = ManageNotice(text: action == "approve" ? "manage.approved" : "manage.declined")
            await load()
        } catch { self.error = error }
    }

    private func delete(_ r: JSONValue) async {
        do {
            try await client.send("DELETE", "request/\(r["id"].text)")
            await load()
        } catch { self.error = error }
    }
}

/// A request row; the title and poster are looked up from the media's TMDB entry.
struct SeerrRequestRow: View {
    let client: ArrClient
    let request: JSONValue
    @State private var details: JSONValue = .null

    private var mediaType: String { request["type"].string ?? request["media"]["mediaType"].string ?? "movie" }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            RemotePoster(url: tmdbImage(details["posterPath"].string, size: "w185"), title: title)
                .frame(width: 50, height: 75)
                .clipShape(RoundedRectangle(cornerRadius: 4, style: .continuous))
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: title).font(.subheadline.weight(.semibold)).lineLimit(2)
                HStack(spacing: 6) {
                    statusBadge
                    PlainBadge(text: mediaType.uppercased())
                    if request["is4k"].bool == true { PlainBadge(text: "4K", tone: .accent) }
                }
                let seasons = request["seasons"].list.compactMap { $0["seasonNumber"].int }
                if !seasons.isEmpty {
                    Text("manage.seasons_list \(seasons.map { String($0) }.joined(separator: ", "))")
                        .font(.caption).foregroundStyle(Theme.textMuted)
                }
                Text(verbatim: [request["requestedBy"]["displayName"].string, ArrDate.short(request["createdAt"].string)]
                    .compactMap { $0 }.joined(separator: " · "))
                    .font(.caption2).foregroundStyle(Theme.textFaint)
            }
        }
        .task(id: request["id"]) {
            guard let tmdb = request["media"]["tmdbId"].int else { return }
            details = (try? await client.get("\(mediaType == "tv" ? "tv" : "movie")/\(tmdb)")) ?? .null
        }
    }

    private var title: String {
        details.firstString("title", "name") ?? "#" + request["media"]["tmdbId"].text
    }

    @ViewBuilder private var statusBadge: some View {
        switch request["status"].int {
        case 1: TagBadge(text: "manage.req.pending", tone: .accent)
        case 2: TagBadge(text: "manage.req.approved", tone: .success)
        case 3: TagBadge(text: "manage.req.declined", tone: .danger)
        case 4: TagBadge(text: "manage.req.failed", tone: .danger)
        default: TagBadge(text: "manage.req.completed", tone: .success)
        }
    }
}

// MARK: Discover

/// Trending and search; request a movie, or a series with the seasons to get.
struct SeerrDiscoverView: View {
    let client: ArrClient
    @State private var query = ""
    @State private var results: [JSONValue]?
    @State private var error: Error?

    var body: some View {
        ScrollView {
            if let error { ArrErrorLine(error: error).padding() }
            if let results {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 104), spacing: 12)], spacing: 16) {
                    ForEach(results.filter { $0["mediaType"].string != "person" }, id: \.self) { item in
                        NavigationLink {
                            SeerrRequestSheet(client: client, item: item)
                        } label: {
                            VStack(alignment: .leading, spacing: 5) {
                                RemotePoster(url: tmdbImage(item["posterPath"].string), title: item.firstString("title", "name") ?? "")
                                    .aspectRatio(2 / 3, contentMode: .fit)
                                    .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                                    .overlay(alignment: .topTrailing) { availabilityDot(item) }
                                Text(verbatim: item.firstString("title", "name") ?? "").font(.caption.weight(.semibold))
                                    .foregroundStyle(Theme.text).lineLimit(1)
                                Text(verbatim: String((item.firstString("releaseDate", "firstAirDate") ?? "").prefix(4)))
                                    .font(.caption2).foregroundStyle(Theme.textFaint)
                            }
                        }
                        .buttonStyle(CardPressStyle())
                    }
                }
                .padding(16)
            } else if error == nil {
                LoadingView()
            }
        }
        .background(Theme.background)
        .searchable(text: $query)
        .onSubmit(of: .search) { Task { await load() } }
        .onChange(of: query) { _, now in if now.isEmpty { Task { await load() } } }
        .navigationTitle(Text("manage.discover"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    @ViewBuilder private func availabilityDot(_ item: JSONValue) -> some View {
        switch item["mediaInfo"]["status"].int {
        case 5: Image(systemName: "checkmark.circle.fill").foregroundStyle(.green).padding(5)
        case 2, 3, 4: Image(systemName: "clock.fill").foregroundStyle(Theme.accent).padding(5)
        default: EmptyView()
        }
    }

    private func load() async {
        let term = query.trimmingCharacters(in: .whitespaces)
        do {
            if term.isEmpty {
                results = try await client.get("discover/trending", query: [.q("page", 1)])["results"].list
            } else {
                results = try await client.get("search", query: [.q("query", term), .q("page", 1)])["results"].list
            }
            error = nil
        } catch { self.error = error }
    }
}

/// Request one title (seasons for series).
struct SeerrRequestSheet: View {
    let client: ArrClient
    let item: JSONValue
    @State private var details: JSONValue = .null
    @State private var seasons: Set<Int> = []
    @State private var error: Error?
    @State private var busy = false
    @State private var done = false

    private var isTV: Bool { item["mediaType"].string == "tv" }
    private var tmdbId: Int { item["id"].int ?? 0 }

    var body: some View {
        Form {
            Section {
                HStack(alignment: .top, spacing: 12) {
                    RemotePoster(url: tmdbImage(item["posterPath"].string), title: item.firstString("title", "name") ?? "")
                        .frame(width: 80, height: 120)
                        .clipShape(RoundedRectangle(cornerRadius: 4, style: .continuous))
                    VStack(alignment: .leading, spacing: 4) {
                        Text(verbatim: item.firstString("title", "name") ?? "").headFont(16)
                        Text(verbatim: item["overview"].string ?? "").font(.caption).foregroundStyle(Theme.textMuted).lineLimit(8)
                    }
                }
            }
            .listRowBackground(Color.clear)
            if isTV {
                Section {
                    let all = details["seasons"].list.compactMap { $0["seasonNumber"].int }.filter { $0 > 0 }
                    Button(seasons.count == all.count ? LocalizedStringKey("manage.select_none") : LocalizedStringKey("manage.select_all")) {
                        seasons = seasons.count == all.count ? [] : Set(all)
                    }
                    ForEach(details["seasons"].list.filter { ($0["seasonNumber"].int ?? 0) > 0 }, id: \.self) { s in
                        let n = s["seasonNumber"].int ?? 0
                        Toggle(isOn: Binding(get: { seasons.contains(n) }, set: { if $0 { seasons.insert(n) } else { seasons.remove(n) } })) {
                            VStack(alignment: .leading) {
                                Text("manage.season \(n)")
                                Text("manage.episode_count \(s["episodeCount"].int ?? 0)").font(.caption).foregroundStyle(Theme.textFaint)
                            }
                        }
                    }
                } header: { Text("manage.seasons") }
            }
            Section {
                ArrErrorLine(error: error)
                if done {
                    Label("manage.requested", systemImage: "checkmark.circle.fill").foregroundStyle(.green)
                } else {
                    Button { Task { await request() } } label: { HStack { Text("manage.request"); Spacer(); if busy { ProgressView() } } }
                        .disabled(busy || (isTV && seasons.isEmpty))
                }
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.request"))
        .navigationBarTitleDisplayMode(.inline)
        .task {
            if isTV {
                details = (try? await client.get("tv/\(tmdbId)")) ?? .null
                seasons = Set(details["seasons"].list.compactMap { $0["seasonNumber"].int }.filter { $0 > 0 })
            }
        }
    }

    private func request() async {
        busy = true
        defer { busy = false }
        var body: JSONValue = ["mediaType": .string(isTV ? "tv" : "movie"), "mediaId": .number(tmdbId)]
        if isTV { body["seasons"] = .ints(seasons.sorted()) }
        do {
            try await client.send("POST", "request", json: body)
            done = true
            error = nil
        } catch { self.error = error }
    }
}

// MARK: Issues, users

struct SeerrIssuesView: View {
    let client: ArrClient
    @State private var filter = "open"
    @State private var issues: [JSONValue]?
    @State private var error: Error?

    var body: some View {
        List {
            Section {
                Picker(selection: $filter) {
                    Text("manage.issue.open").tag("open")
                    Text("manage.issue.resolved").tag("resolved")
                    Text("manage.filter.all").tag("all")
                } label: { EmptyView() }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
            }
            if let error { ArrErrorLine(error: error) }
            ForEach(issues ?? [], id: \.rowID) { issue in
                NavigationLink {
                    SeerrIssueDetail(client: client, issueId: issue["id"].int ?? 0)
                } label: {
                    VStack(alignment: .leading, spacing: 4) {
                        HStack(spacing: 6) {
                            PlainBadge(text: issueType(issue["issueType"].int), tone: .accent)
                            if issue["status"].int == 2 { TagBadge(text: "manage.issue.resolved", tone: .success) }
                        }
                        Text(verbatim: issue["comments"].list.first?["message"].string ?? "").font(.subheadline).lineLimit(2)
                        Text(verbatim: [issue["createdBy"]["displayName"].string, ArrDate.short(issue["createdAt"].string)]
                            .compactMap { $0 }.joined(separator: " · ")).font(.caption2).foregroundStyle(Theme.textFaint)
                    }
                }
            }
            if issues?.isEmpty == true { EmptyRow(text: "manage.no_issues") }
            if issues == nil && error == nil { LoadingView() }
        }
        .manageBackground()
        .navigationTitle(Text("manage.issues"))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: filter) { await load() }
        .refreshable { await load() }
    }

    private func issueType(_ t: Int?) -> String {
        switch t {
        case 1: Loc.string("manage.issue.video")
        case 2: Loc.string("manage.issue.audio")
        case 3: Loc.string("manage.issue.subtitles")
        default: Loc.string("manage.issue.other")
        }
    }

    private func load() async {
        do {
            issues = try await client.get("issue", query: [.q("take", 50), .q("skip", 0), .q("filter", filter), .q("sort", "added")])["results"].list
            error = nil
        } catch { self.error = error }
    }
}

struct SeerrIssueDetail: View {
    let client: ArrClient
    let issueId: Int
    @State private var issue: JSONValue?
    @State private var reply = ""
    @State private var error: Error?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let issue {
                Section {
                    ForEach(issue["comments"].list, id: \.rowID) { c in
                        VStack(alignment: .leading, spacing: 3) {
                            Text(verbatim: c["user"]["displayName"].string ?? "").font(.caption.weight(.semibold)).foregroundStyle(Theme.accent)
                            Text(verbatim: c["message"].string ?? "").font(.subheadline)
                            Text(verbatim: ArrDate.short(c["createdAt"].string)).font(.caption2).foregroundStyle(Theme.textFaint)
                        }
                    }
                }
                Section {
                    TextField(text: $reply, axis: .vertical) { Text("manage.comment") }.lineLimit(2...6)
                    Button { Task { await comment() } } label: { Text("manage.send") }
                        .disabled(reply.trimmingCharacters(in: .whitespaces).isEmpty)
                }
                Section {
                    if issue["status"].int == 2 {
                        Button { Task { await setStatus("open") } } label: { Text("manage.reopen") }
                    } else {
                        Button { Task { await setStatus("resolved") } } label: { Text("manage.resolve") }
                    }
                    Button("manage.delete", role: .destructive) { Task { await delete() } }
                }
            } else if error == nil {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.issues"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        do {
            issue = try await client.get("issue/\(issueId)")
            error = nil
        } catch { self.error = error }
    }

    private func comment() async {
        do {
            try await client.send("POST", "issue/\(issueId)/comment", json: ["message": .string(reply)])
            reply = ""
            await load()
        } catch { self.error = error }
    }

    private func setStatus(_ status: String) async {
        do {
            try await client.send("POST", "issue/\(issueId)/\(status)")
            await load()
        } catch { self.error = error }
    }

    private func delete() async {
        do {
            try await client.send("DELETE", "issue/\(issueId)")
            dismiss()
        } catch { self.error = error }
    }
}

struct SeerrUsersView: View {
    let client: ArrClient
    @State private var users: [JSONValue]?
    @State private var error: Error?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            ForEach(users ?? [], id: \.rowID) { u in
                HStack(spacing: 12) {
                    RemotePoster(url: (u["avatar"].string).flatMap { $0.hasPrefix("http") ? URL(string: $0) : nil }, title: u["displayName"].string ?? "")
                        .frame(width: 36, height: 36)
                        .clipShape(Circle())
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: u["displayName"].string ?? u["email"].string ?? "").font(.subheadline.weight(.semibold))
                        Text(verbatim: u["email"].string ?? "").font(.caption).foregroundStyle(Theme.textMuted)
                    }
                    Spacer()
                    Text("manage.request_count \(u["requestCount"].int ?? 0)").font(.caption).foregroundStyle(Theme.textMuted)
                }
            }
            if users == nil && error == nil { LoadingView() }
        }
        .manageBackground()
        .navigationTitle(Text("manage.users"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        do {
            users = try await client.get("user", query: [.q("take", 100), .q("skip", 0), .q("sort", "displayname")])["results"].list
            error = nil
        } catch { self.error = error }
    }
}

// MARK: System and settings

struct SeerrSystemView: View {
    let client: ArrClient
    @State private var status: JSONValue?
    @State private var jobs: [JSONValue] = []
    @State private var error: Error?
    @State private var notice: ManageNotice?

    var body: some View {
        List {
            if let error { ArrErrorLine(error: error) }
            if let status {
                Section {
                    InfoRow(title: "manage.version", value: status["version"].text)
                    if let c = status["commitTag"].string { InfoRow(title: "manage.branch", value: c) }
                    if status["updateAvailable"].bool == true { TagBadge(text: "manage.update_available", tone: .accent) }
                } header: { Text("manage.status") }
            }
            Section {
                ForEach(jobs, id: \.rowID) { j in
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: j["name"].string ?? "").font(.subheadline)
                            Text(verbatim: [j["interval"].string, ArrDate.short(j["nextExecutionTime"].string)].compactMap { $0 }.joined(separator: " · "))
                                .font(.caption2).foregroundStyle(Theme.textFaint)
                        }
                        Spacer()
                        if j["running"].bool == true {
                            ProgressView()
                        } else {
                            Button { Task { await run(j) } } label: { Image(systemName: "play.circle.fill").font(.title3) }
                                .buttonStyle(.borderless).accessibilityLabel(Text("run_now"))
                        }
                    }
                }
            } header: { Text("manage.jobs") }
        }
        .manageBackground()
        .navigationTitle(Text("manage.system"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .manageNotice($notice)
    }

    private func load() async {
        do {
            status = try await client.get("status")
            jobs = (try? await client.get("settings/jobs").list) ?? []
            error = nil
        } catch { self.error = error }
    }

    private func run(_ j: JSONValue) async {
        do {
            try await client.send("POST", "settings/jobs/\(j["id"].text)/run")
            notice = ManageNotice(text: "manage.command_started")
            await load()
        } catch { self.error = error }
    }
}

/// Seerr settings: main, services (Radarr/Sonarr connections), notifications.
struct SeerrSettingsMenu: View {
    let client: ArrClient

    private static let agents = ["email", "discord", "slack", "telegram", "pushover", "pushbullet", "gotify", "ntfy", "webhook", "webpush", "lunasea"]

    var body: some View {
        List {
            Section {
                NavigationLink {
                    ConfigEditorView(client: client, resource: "settings/main", titleKey: "manage.general", saveMode: .post)
                } label: { MenuRow(title: "manage.general", icon: "gearshape") }
            }
            Section {
                NavigationLink {
                    ObjectListView(client: client, resource: "settings/radarr", titleKey: "Radarr", titleKeys: ["name", "hostname"],
                                   subtitleKeys: ["hostname", "activeDirectory"], template: Self.serviceTemplate(port: 7878))
                } label: { MenuRow(title: LocalizedStringKey("manage.services_radarr"), icon: ArrService.radarr.symbol) }
                NavigationLink {
                    ObjectListView(client: client, resource: "settings/sonarr", titleKey: "Sonarr", titleKeys: ["name", "hostname"],
                                   subtitleKeys: ["hostname", "activeDirectory"], template: Self.serviceTemplate(port: 8989))
                } label: { MenuRow(title: LocalizedStringKey("manage.services_sonarr"), icon: ArrService.sonarr.symbol) }
            } header: { Text("manage.services") }
            Section {
                ForEach(Self.agents, id: \.self) { agent in
                    NavigationLink {
                        ConfigEditorView(client: client, resource: "settings/notifications/\(agent)", titleKey: "manage.notifications", saveMode: .post)
                    } label: {
                        Label { Text(verbatim: agent.humanizedKey) } icon: { Image(systemName: "bell").foregroundStyle(Theme.accent) }
                    }
                }
            } header: { Text("manage.notifications") }
        }
        .manageBackground()
        .navigationTitle(Text("manage.settings"))
        .navigationBarTitleDisplayMode(.inline)
    }

    static func serviceTemplate(port: Int) -> JSONValue {
        ["name": "", "hostname": "", "port": .number(port), "apiKey": "", "useSsl": false, "baseUrl": "",
         "activeProfileId": 0, "activeProfileName": "", "activeDirectory": "", "is4k": false, "minimumAvailability": "released",
         "isDefault": false, "externalUrl": "", "syncEnabled": false, "preventSearch": false, "tags": []]
    }
}
