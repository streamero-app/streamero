import SwiftUI

/// Search with debounced queries; titles the server does not have come from Discover.
struct SearchTab: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var query = ""
    @State private var results: [ItemSummary]?
    @State private var elsewhere: DiscoverPage?

    private let columns = [GridItem(.adaptive(minimum: 108), spacing: 14)]

    private var missing: [DiscoverTitle] {
        guard let page = elsewhere, page.configured else { return [] }
        return Array(page.titles.filter { $0.itemId == nil }.prefix(12))
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                if let results {
                    if results.isEmpty { Text("nothing_found").foregroundStyle(Theme.textMuted).frame(maxWidth: .infinity, alignment: .center).padding(.top, 32) }
                    LazyVGrid(columns: columns, spacing: 22) {
                        ForEach(results) { item in
                            NavigationLink(value: Route.item(item.id)) { PosterCard(item: item, client: client) }.buttonStyle(CardPressStyle())
                        }
                    }
                }
                if !missing.isEmpty {
                    SectionHead(title: "not_here_yet").padding(.top, 8)
                    LazyVGrid(columns: columns, spacing: 22) {
                        ForEach(missing) { DiscoverCard(title: $0, canRequest: elsewhere?.canRequest == true, client: client) }
                    }
                }
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .background(Theme.background)
        .navigationTitle(Text("nav_search"))
        .searchable(text: $query, prompt: Text("search_hint"))
        .task(id: query) { await search() }
    }

    private func search() async {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { results = nil; elsewhere = nil; return }
        try? await Task.sleep(for: .milliseconds(300))
        guard !Task.isCancelled else { return }
        results = (try? await client.search(q)) ?? []
        if app.enhancements.discoverInSearch {
            elsewhere = try? await client.discover(q)
        } else {
            elsewhere = nil
        }
    }
}

func statusLabel(_ status: String) -> LocalizedStringKey {
    switch status {
    case "pending": "status_requested"
    case "processing": "status_downloading"
    case "partial": "status_partial"
    case "available": "status_available"
    default: "status_not_requested"
    }
}

/// A title from Discover with its request button.
struct DiscoverCard: View {
    let title: DiscoverTitle
    let canRequest: Bool
    let client: MediaClient
    @State private var status: String?
    @State private var busy = false
    @State private var error: Error?

    private var currentStatus: String { status ?? title.status }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ZStack(alignment: .topLeading) {
                Theme.surface2
                // TMDb poster URLs are public CDN images, loaded without the session.
                if let poster = title.posterUrl.flatMap(URL.init(string:)) {
                    AsyncImage(url: poster) { $0.resizable().scaledToFill() } placeholder: { Color.clear }
                }
                if currentStatus != "none" {
                    Text(statusLabel(currentStatus)).font(.system(size: 10, weight: .bold)).textCase(.uppercase).foregroundStyle(Theme.text)
                        .padding(.horizontal, 5).padding(.vertical, 1)
                        .background(.black.opacity(0.75), in: RoundedRectangle(cornerRadius: 3)).padding(6)
                }
            }
            .aspectRatio(2 / 3, contentMode: .fit)
            .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
            Text(verbatim: title.title).font(.footnote.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
            HStack(spacing: 4) {
                if let year = title.year { Text(verbatim: "\(year) ·") }
                Text(title.mediaType == "tv" ? "series" : "movie")
            }
            .font(.caption).foregroundStyle(Theme.textFaint)
            if let itemID = title.itemId {
                NavigationLink(value: Route.item(itemID)) { Text("watch").font(.footnote.weight(.semibold)).frame(maxWidth: .infinity, minHeight: 34) }
                    .buttonStyle(.plain).background(.white.opacity(0.12), in: RoundedRectangle(cornerRadius: 6, style: .continuous)).foregroundStyle(Theme.text)
            } else if currentStatus == "none" && canRequest {
                Button { Task { await request() } } label: {
                    Text("request").font(.footnote.weight(.semibold)).frame(maxWidth: .infinity, minHeight: 34)
                }
                .buttonStyle(.plain).foregroundStyle(Theme.background).background(Theme.text, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                .disabled(busy)
            }
            ErrorLine(error: error)
        }
    }

    private func request() async {
        busy = true
        defer { busy = false }
        do {
            status = try await client.request(title).status
        } catch { self.error = error }
    }
}

/// Discover: trending titles, search, requests and the upcoming calendar.
struct DiscoverTab: View {
    let client: MediaClient
    @State private var query = ""
    @State private var page: DiscoverPage?
    @State private var upcoming: [UpcomingRelease] = []
    @State private var error: Error?

    private let columns = [GridItem(.adaptive(minimum: 108), spacing: 14)]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                PageTitle(title: "discover")
                Text("discover_intro").font(.subheadline).foregroundStyle(Theme.textMuted)
                searchField
                ErrorLine(error: error)
                if page?.configured == false { Text("discover_not_configured").foregroundStyle(Theme.textMuted) }
                if query.isEmpty && !upcoming.isEmpty {
                    VStack(alignment: .leading, spacing: 10) {
                        SectionHead(title: "coming_soon")
                        ScrollView(.horizontal, showsIndicators: false) {
                            LazyHStack(spacing: 10) {
                                ForEach(Array(upcoming.prefix(30).enumerated()), id: \.offset) { _, release in
                                    upcomingCard(release)
                                }
                            }
                        }
                    }
                }
                if let page, page.configured {
                    SectionHead(title: query.isEmpty ? "trending" : "results")
                    LazyVGrid(columns: columns, spacing: 22) {
                        ForEach(page.titles) { DiscoverCard(title: $0, canRequest: page.canRequest, client: client) }
                    }
                }
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .background(Theme.background)
        .task { upcoming = (try? await client.upcoming()) ?? [] }
        .task(id: query) {
            if !query.isEmpty { try? await Task.sleep(for: .milliseconds(350)) }
            guard !Task.isCancelled else { return }
            do {
                page = try await client.discover(query)
                error = nil
            } catch is CancellationError { return } catch { self.error = error }
        }
    }

    private var searchField: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass").foregroundStyle(Theme.textMuted)
            TextField("discover_hint", text: $query).autocorrectionDisabled()
        }
        .padding(.horizontal, 12).frame(height: 44)
        .background(Theme.surface2, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
    }

    private func upcomingCard(_ u: UpcomingRelease) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: String(u.date.prefix(10))).font(.caption.bold()).foregroundStyle(Theme.accent)
            Text(verbatim: u.seriesTitle ?? u.title).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
            Group {
                if u.kind == "episode" {
                    Text(verbatim: "S\(u.season ?? 0):E\(u.episode ?? 0) · \(u.title)")
                } else {
                    Text("movie")
                }
            }
            .font(.footnote).foregroundStyle(Theme.textMuted).lineLimit(1)
        }
        .padding(12)
        .frame(width: 240, alignment: .leading)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
    }
}
