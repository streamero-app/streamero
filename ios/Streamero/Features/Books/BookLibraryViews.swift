import SwiftUI

private let bookColumns = [GridItem(.adaptive(minimum: 108), spacing: 14)]

/// A cover: tall for books, square for audiobooks, with the reading progress along the bottom.
struct BookCover: View {
    let item: ItemSummary
    let client: MediaClient

    private var aspect: CGFloat { item.kind == "audiobook" ? 1 : 2 / 3 }

    var body: some View {
        Color.clear
            .aspectRatio(aspect, contentMode: .fit)
            .overlay {
                AuthImage(url: item.hasPoster ? client.imageURL(item: item.id, type: "poster") : nil,
                          token: client.token, title: item.title)
            }
            .overlay(alignment: .topTrailing) {
                if item.kind == "audiobook" {
                    Image(systemName: "headphones").font(.caption2.bold()).foregroundStyle(Theme.text)
                        .frame(width: 22, height: 22).background(.black.opacity(0.55), in: Circle()).padding(6)
                } else if item.played == true {
                    Image(systemName: "checkmark").font(.caption.bold()).foregroundStyle(Theme.accentContrast)
                        .frame(width: 22, height: 22).background(Theme.accent, in: Circle()).padding(6)
                }
            }
            .overlay(alignment: .bottom) {
                if let p = ReadingMath.coverProgress(item), p < 1 { ProgressStrip(value: p).padding(8) }
            }
            .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous).strokeBorder(Theme.border))
    }
}

/// Cover, title and author.
struct BookCard: View {
    let item: ItemSummary
    let client: MediaClient

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            BookCover(item: item, client: client)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: item.title).font(.footnote.weight(.medium)).foregroundStyle(Theme.text).lineLimit(2)
                Text(verbatim: bookByline(item)).font(.caption2).foregroundStyle(Theme.textMuted).lineLimit(1)
            }
        }
    }
}

/// "Author" or "Series #2" under a cover.
func bookByline(_ item: ItemSummary) -> String {
    if let authors = item.authors, !authors.isEmpty { return authors.joined(separator: ", ") }
    if let series = item.seriesName { return seriesLabel(series, item.seriesIndex) }
    return " "
}

/// "Series · #2" (fractional positions such as 2.5 keep their fraction).
func seriesLabel(_ name: String, _ index: Double?) -> String {
    guard let index else { return name }
    let number = index == index.rounded() ? String(Int(index)) : String(index)
    return "\(name) · #\(number)"
}

/// The cover grid of a paged listing with infinite scroll.
struct PagedBookGrid: View {
    let paged: PagedItems
    let client: MediaClient

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ErrorLine(error: paged.error)
            LazyVGrid(columns: bookColumns, spacing: 22) {
                ForEach(paged.items) { item in
                    NavigationLink(value: Route.item(item.id)) { BookCard(item: item, client: client) }
                        .buttonStyle(CardPressStyle())
                        .task(id: item.id) { await paged.loadMoreIfNeeded(after: item) }
                }
            }
            if paged.loading && paged.items.isEmpty { LoadingView() }
            if paged.total == 0 { Text("nothing_here").foregroundStyle(Theme.textMuted).padding(.top, 24) }
        }
    }
}

/// A book or audiobook library: everything, by author or by series.
struct BookLibraryView: View {
    let library: LibraryRoute
    let client: MediaClient
    @State private var tab = Tab.all
    @State private var sort = "title"
    @State private var paged: PagedItems
    @State private var authors: [NameCount]?
    @State private var series: [NameCount]?
    @State private var error: Error?

    enum Tab: Hashable { case all, authors, series }

    init(library: LibraryRoute, client: MediaClient) {
        self.library = library
        self.client = client
        _paged = State(initialValue: PagedItems(client: client, libraryId: library.id, query: [:]))
    }

    /// The item kind listed in this library.
    private var kind: String { library.kind == "audiobooks" ? "audiobook" : "book" }

    private var sortOptions: [Opt<String>] {
        [Opt("title", "sort_title"), Opt("added", "sort_added"), Opt("series", "books.sortSeries")]
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text(verbatim: library.name).font(Theme.display(34)).foregroundStyle(Theme.text)
                HStack(spacing: 8) {
                    tabButton(.all, "books.all")
                    tabButton(.authors, "books.authors")
                    tabButton(.series, "books.series")
                }
                switch tab {
                case .all:
                    HStack(spacing: 8) {
                        if paged.total >= 0 { Text("items_count \(paged.total)").font(.subheadline).foregroundStyle(Theme.textMuted) }
                        Spacer(minLength: 0)
                        Picker(selection: $sort) {
                            ForEach(sortOptions) { $0.label.tag($0.value) }
                        } label: { EmptyView() }
                        .pickerStyle(.menu)
                        .tint(Theme.text)
                    }
                    PagedBookGrid(paged: paged, client: client)
                case .authors:
                    nameList(authors) { BookListRoute(libraryId: library.id, kind: kind, title: $0.name, author: $0.name) }
                case .series:
                    seriesGrid
                }
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: sort) { await paged.reload(query: ["kind": kind, "sort": sort]) }
        .task(id: tab) { await loadNames() }
        .refreshable {
            await paged.reload()
            await loadNames(force: true)
        }
    }

    private func tabButton(_ value: Tab, _ title: LocalizedStringKey) -> some View {
        Button { tab = value } label: { Text(title) }.buttonStyle(ToggleCapsuleStyle(active: tab == value))
    }

    @ViewBuilder private func nameList(_ names: [NameCount]?, route: @escaping (NameCount) -> BookListRoute) -> some View {
        ErrorLine(error: error)
        if names == nil && error == nil { LoadingView() }
        if names?.isEmpty == true { Text("nothing_here").foregroundStyle(Theme.textMuted).padding(.top, 24) }
        LazyVStack(spacing: 0) {
            ForEach(names ?? []) { n in
                NavigationLink(value: Route.books(route(n))) {
                    HStack(spacing: 14) {
                        Text(verbatim: initials(n.name)).font(Theme.head(16)).foregroundStyle(Theme.text.opacity(0.75))
                            .frame(width: 44, height: 44)
                            .background(Color(hue: Theme.hue(for: n.name), saturation: 0.25, brightness: 0.2), in: Circle())
                        Text(verbatim: n.name).foregroundStyle(Theme.text).lineLimit(1)
                        Spacer(minLength: 0)
                        Text(verbatim: String(n.count)).font(.subheadline.monospacedDigit()).foregroundStyle(Theme.textFaint)
                        Image(systemName: "chevron.right").font(.caption).foregroundStyle(Theme.textFaint)
                    }
                    .padding(.vertical, 8)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
    }

    @ViewBuilder private var seriesGrid: some View {
        ErrorLine(error: error)
        if series == nil && error == nil { LoadingView() }
        if series?.isEmpty == true { Text("nothing_here").foregroundStyle(Theme.textMuted).padding(.top, 24) }
        LazyVGrid(columns: bookColumns, spacing: 22) {
            ForEach(series ?? []) { s in
                NavigationLink(value: Route.books(BookListRoute(libraryId: library.id, kind: kind, title: s.name, series: s.name))) {
                    VStack(alignment: .leading, spacing: 8) {
                        Color.clear
                            .aspectRatio(kind == "audiobook" ? 1 : 2 / 3, contentMode: .fit)
                            .overlay { AuthImage(url: s.itemId.flatMap { client.imageURL(item: $0, type: "poster") }, token: client.token, title: s.name) }
                            .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                            .overlay(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous).strokeBorder(Theme.border))
                        Text(verbatim: s.name).font(.footnote.weight(.medium)).foregroundStyle(Theme.text).lineLimit(2)
                        Text("items_count \(s.count)").font(.caption2).foregroundStyle(Theme.textMuted)
                    }
                }
                .buttonStyle(CardPressStyle())
            }
        }
    }

    private func initials(_ name: String) -> String {
        name.split(separator: " ").prefix(2).compactMap(\.first).map { String($0).uppercased() }.joined()
    }

    private func loadNames(force: Bool = false) async {
        do {
            switch tab {
            case .all: return
            case .authors: if authors == nil || force { authors = try await client.authors(library: library.id) }
            case .series: if series == nil || force { series = try await client.series(library: library.id) }
            }
            error = nil
        } catch is CancellationError {
            return
        } catch {
            self.error = error
        }
    }
}

/// The books of one author or series.
struct BookListView: View {
    let route: BookListRoute
    let client: MediaClient
    @State private var paged: PagedItems

    init(route: BookListRoute, client: MediaClient) {
        self.route = route
        self.client = client
        var query: [String: String?] = ["kind": route.kind, "sort": route.series != nil ? "series" : "title"]
        query["author"] = route.author
        query["series"] = route.series
        _paged = State(initialValue: PagedItems(client: client, libraryId: route.libraryId, query: query))
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text(verbatim: route.title).font(Theme.display(30)).foregroundStyle(Theme.text)
                Text(route.series != nil ? "books.series" : "books.author")
                    .font(.caption.bold()).textCase(.uppercase).foregroundStyle(Theme.textFaint)
                PagedBookGrid(paged: paged, client: client)
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .task { await paged.reload() }
        .refreshable { await paged.reload() }
    }
}
