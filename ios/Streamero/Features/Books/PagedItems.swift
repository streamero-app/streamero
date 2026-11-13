import Foundation
import Observation

/// A library listing loaded page by page; grids and the photo viewer share one so the viewer
/// can keep paging past what the grid has shown.
@MainActor
@Observable
final class PagedItems {
    private(set) var items: [ItemSummary] = []
    private(set) var total = -1
    private(set) var loading = false
    private(set) var error: Error?

    private let client: MediaClient
    private let libraryId: String
    private var query: [String: String?]
    private var generation = 0
    private let pageSize = 60

    init(client: MediaClient, libraryId: String, query: [String: String?]) {
        self.client = client
        self.libraryId = libraryId
        self.query = query
    }

    var hasMore: Bool { total > items.count }

    func reload(query newQuery: [String: String?]? = nil) async {
        if let newQuery { query = newQuery }
        generation += 1
        let gen = generation
        loading = true
        defer { if gen == generation { loading = false } }
        do {
            let page = try await client.items(library: libraryId, query: pageQuery(offset: 0))
            guard gen == generation else { return }
            items = page.items
            total = page.total
            error = nil
        } catch is CancellationError {
            return
        } catch {
            if gen == generation { self.error = error }
        }
    }

    func loadMore() async {
        guard !loading, hasMore else { return }
        let gen = generation
        loading = true
        defer { if gen == generation { loading = false } }
        do {
            let page = try await client.items(library: libraryId, query: pageQuery(offset: items.count))
            guard gen == generation else { return }
            let known = Set(items.map(\.id))
            items += page.items.filter { !known.contains($0.id) }
            total = page.total
        } catch {
            return
        }
    }

    /// Loads the next page when `item` is one of the last few.
    func loadMoreIfNeeded(after item: ItemSummary) async {
        guard let i = items.firstIndex(where: { $0.id == item.id }), i >= items.count - 8 else { return }
        await loadMore()
    }

    private func pageQuery(offset: Int) -> [String: String?] {
        var q = query
        q["limit"] = String(pageSize)
        q["offset"] = String(offset)
        return q
    }
}
