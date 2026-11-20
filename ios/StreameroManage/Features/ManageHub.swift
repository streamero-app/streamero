import SwiftUI

/// The Manage tab: every *arr service with its state, and the way into each (D-048).
struct ManageTab: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var services: [ArrServiceStatus]?
    @State private var summaries: [ArrService: ServiceSummary] = [:]
    @State private var error: Error?

    var body: some View {
        Group {
            if !app.isAdmin {
                ForbiddenView()
            } else {
                list
            }
        }
        .navigationTitle(Text("manage.tab"))
    }

    private var list: some View {
        List {
            Section {
                PageTitle(title: "manage.tab").listRowBackground(Color.clear).listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 4, trailing: 16))
                Text("manage.intro").font(.subheadline).foregroundStyle(Theme.textMuted).listRowBackground(Color.clear)
            }
            if let error {
                Section { ArrErrorState(error: error) { await load() } }
            }
            if let services {
                Section {
                    ForEach(ArrService.allCases) { service in
                        let status = services.first { $0.service == service }
                        NavigationLink {
                            ServiceHomeView(media: client, service: service, status: status)
                        } label: {
                            ServiceCard(service: service, status: status, summary: summaries[service])
                        }
                        .listRowBackground(Theme.surface)
                    }
                }
                Section {
                    NavigationLink {
                        SettingsPageView(page: .requests, client: client)
                    } label: {
                        MenuRow(title: "manage.connections_settings", icon: "link")
                    }
                } footer: {
                    Text("manage.connections_hint")
                }
            } else if error == nil {
                LoadingView().listRowBackground(Color.clear)
            }
        }
        .manageBackground()
        .navigationBarTitleDisplayMode(.inline)
        // Refresh while the hub is visible: queue counts move. Slower after an error (429).
        .poll(every: { .seconds(error == nil ? 15 : 60) }) { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        do {
            let list = try await client.arrServices()
            services = list
            error = nil
            await withTaskGroup(of: (ArrService, ServiceSummary).self) { group in
                for status in list where status.configured && status.reachable {
                    guard let service = status.service else { continue }
                    let arr = client.arr(service, apiBase: status.apiBase)
                    group.addTask { (service, await ServiceSummary.load(arr)) }
                }
                for await (service, summary) in group { summaries[service] = summary }
            }
        } catch is CancellationError {
            return
        } catch {
            self.error = error
        }
    }
}

/// Counts shown on a service card.
struct ServiceSummary: Sendable, Equatable {
    var warnings = 0
    var errors = 0
    /// False when the health check itself failed: then nothing is claimed about the health.
    var healthKnown = true
    /// Downloads in the queue (Radarr/Sonarr/Lidarr), pending requests (Seerr), missing
    /// subtitles (Bazarr), indexers (Prowlarr).
    var count: Int?

    static func load(_ arr: ArrClient) async -> ServiceSummary {
        var s = ServiceSummary()
        switch arr.service {
        case .radarr, .sonarr, .lidarr, .prowlarr:
            if let health = try? await arr.get("health").list {
                s.errors = health.filter { $0["type"].string?.lowercased() == "error" }.count
                s.warnings = health.count - s.errors
            } else {
                s.healthKnown = false
            }
            if arr.service == .prowlarr {
                s.count = (try? await arr.get("indexer").list.count)
            } else {
                s.count = (try? await arr.get("queue/status"))?["totalCount"].int
            }
        case .bazarr:
            let badges = try? await arr.get("badges")
            s.count = (badges?["movies"].int ?? 0) + (badges?["episodes"].int ?? 0)
            if let health = try? await arr.get("system/health") {
                s.warnings = health["data"].list.count
            } else {
                s.healthKnown = false
            }
        case .seerr:
            s.count = (try? await arr.get("request/count"))?["pending"].int
        }
        return s
    }
}

struct ServiceCard: View {
    let service: ArrService
    let status: ArrServiceStatus?
    let summary: ServiceSummary?

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: service.symbol)
                .font(.system(size: 20, weight: .semibold))
                .foregroundStyle(ok ? Theme.accent : Theme.textFaint)
                .frame(width: 44, height: 44)
                .background((ok ? Theme.accent : Theme.text).opacity(0.12), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) {
                    Text(verbatim: service.name).headFont(16).foregroundStyle(Theme.text)
                    if let v = status?.version, ok { Text(verbatim: v).font(.caption2.monospaced()).foregroundStyle(Theme.textFaint) }
                }
                state
            }
            Spacer(minLength: 4)
            if let count = summary?.count, ok, count > 0 {
                Text(verbatim: "\(count)")
                    .font(.subheadline.weight(.bold).monospacedDigit())
                    .foregroundStyle(Theme.accentContrast)
                    .padding(.horizontal, 9).padding(.vertical, 3)
                    .background(Theme.accent, in: Capsule())
                    .accessibilityLabel(Text(countLabel) + Text(verbatim: " \(count)"))
            }
        }
        .padding(.vertical, 6)
    }

    private var ok: Bool { status?.configured == true && status?.reachable == true }

    private var countLabel: LocalizedStringKey {
        switch service {
        case .seerr: "manage.pending_requests"
        case .bazarr: "manage.missing_subtitles"
        case .prowlarr: "manage.indexers"
        default: "manage.queue"
        }
    }

    @ViewBuilder private var state: some View {
        if status?.configured != true {
            TagBadge(text: "manage.not_configured")
        } else if status?.reachable != true {
            HStack(spacing: 6) {
                TagBadge(text: "manage.unreachable", tone: .danger)
                if let e = status?.error { Text(verbatim: e).font(.caption2).foregroundStyle(Theme.textFaint).lineLimit(1) }
            }
        } else if let summary, summary.errors + summary.warnings > 0 {
            HStack(spacing: 6) {
                if summary.errors > 0 { PlainBadge(text: "\(summary.errors)", tone: .danger) }
                if summary.warnings > 0 { PlainBadge(text: "\(summary.warnings)", tone: .accent) }
                Text("manage.health_issues").font(.caption).foregroundStyle(Theme.textMuted)
            }
        } else if summary?.healthKnown != false {
            TagBadge(text: "manage.healthy", tone: .success)
        } else {
            TagBadge(text: "manage.health_unknown")
        }
    }
}

/// One service: its sections, or why it cannot be used yet.
struct ServiceHomeView: View {
    let media: MediaClient
    let service: ArrService
    let status: ArrServiceStatus?

    private var arr: ArrClient { media.arr(service, apiBase: status?.apiBase) }

    var body: some View {
        List {
            if status?.configured != true || status?.reachable != true {
                Section {
                    VStack(alignment: .leading, spacing: 10) {
                        Image(systemName: status?.configured == true ? "wifi.exclamationmark" : "wrench.and.screwdriver")
                            .font(.system(size: 28, weight: .light)).foregroundStyle(Theme.textFaint)
                        Text(status?.configured == true ? LocalizedStringKey("manage.unreachable_explain") : LocalizedStringKey("manage.not_configured_explain"))
                            .foregroundStyle(Theme.textMuted)
                        if let e = status?.error { Text(verbatim: e).font(.caption.monospaced()).foregroundStyle(Theme.textFaint) }
                    }
                    .padding(.vertical, 8)
                    NavigationLink {
                        SettingsPageView(page: .requests, client: media)
                    } label: {
                        MenuRow(title: "manage.connections_settings", icon: "link")
                    }
                }
            } else {
                ServiceMenu(arr: arr)
            }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: service.name))
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// The sections of a reachable service.
struct ServiceMenu: View {
    let arr: ArrClient

    var body: some View {
        switch arr.service {
        case .radarr, .sonarr, .lidarr:
            Section {
                link("manage.library", "square.grid.2x2") { ArrLibraryView(client: arr) }
                link("manage.add_new", "plus.circle") { ArrAddView(client: arr) }
                link("manage.calendar", "calendar") { ArrCalendarView(client: arr) }
                link("manage.activity", "arrow.down.circle") { ArrActivityView(client: arr) }
                link("manage.wanted", "exclamationmark.magnifyingglass") { ArrWantedView(client: arr) }
            }
            Section {
                link("manage.settings", "gearshape") { ServarrSettingsMenu(client: arr) }
                link("manage.system", "cpu") { ArrSystemView(client: arr) }
            }
        case .prowlarr:
            Section {
                link("manage.indexers", "magnifyingglass.circle") { ProviderListView(client: arr, kind: .indexers) }
                link("manage.search", "magnifyingglass") { ProwlarrSearchView(client: arr) }
                link("manage.apps", "app.connected.to.app.below.fill") { ProviderListView(client: arr, kind: .applications) }
                link("manage.stats", "chart.bar") { ProwlarrStatsView(client: arr) }
                link("manage.history", "clock.arrow.circlepath") { ArrHistoryList(client: arr) }
            }
            Section {
                link("manage.settings", "gearshape") { ServarrSettingsMenu(client: arr) }
                link("manage.system", "cpu") { ArrSystemView(client: arr) }
            }
        case .bazarr:
            Section {
                link("manage.movies_missing", "film") { BazarrWantedView(client: arr, movies: true) }
                link("manage.episodes_missing", "tv") { BazarrWantedView(client: arr, movies: false) }
                link("manage.history", "clock.arrow.circlepath") { BazarrHistoryView(client: arr) }
                link("manage.providers", "server.rack") { BazarrProvidersView(client: arr) }
            }
            Section {
                link("manage.settings", "gearshape") { BazarrSettingsView(client: arr) }
                link("manage.system", "cpu") { BazarrSystemView(client: arr) }
            }
        case .seerr:
            Section {
                link("manage.requests", "tray.full") { SeerrRequestsView(client: arr) }
                link("manage.discover", "safari") { SeerrDiscoverView(client: arr) }
                link("manage.issues", "exclamationmark.bubble") { SeerrIssuesView(client: arr) }
                link("manage.users", "person.2") { SeerrUsersView(client: arr) }
            }
            Section {
                link("manage.settings", "gearshape") { SeerrSettingsMenu(client: arr) }
                link("manage.system", "cpu") { SeerrSystemView(client: arr) }
            }
        }
    }

    private func link<D: View>(_ title: LocalizedStringKey, _ icon: String, @ViewBuilder _ destination: @escaping () -> D) -> some View {
        NavigationLink { destination() } label: { MenuRow(title: title, icon: icon) }
    }
}
