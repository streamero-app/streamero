import SwiftUI
import UIKit

/// Artwork saved with a download, read from the device (no server needed).
struct LocalArtwork: View {
    let url: URL?
    var title: String = ""
    @State private var image: UIImage?

    var body: some View {
        Color.clear
            .overlay {
                ZStack {
                    PosterPlaceholder(title: title).opacity(image == nil ? 1 : 0)
                    if let image { Image(uiImage: image).resizable().scaledToFill().transition(.opacity) }
                }
            }
            .clipped()
            .task(id: url) {
                guard let url else { image = nil; return }
                let data = await Task.detached(priority: .utility) { try? Data(contentsOf: url) }.value
                image = data.flatMap { UIImage(data: $0) }
            }
    }
}

/// The offline library: movies and series with their episodes, storage, settings and the
/// "next episodes" subscriptions. Works without a connection.
struct DownloadsView: View {
    /// Set when the server could not be reached: shows the offline notice with a retry.
    var offlineRetry: (() async -> Void)? = nil
    @Environment(AppState.self) private var app: AppState?
    @State private var playing: OfflineEntry?
    @State private var confirmDeleteAll = false
    @State private var retrying = false

    private var downloads: DownloadManager { .shared }

    var body: some View {
        let library = downloads.library
        let movies = library.entries.filter { $0.seriesId == nil }.sorted { $0.item.title < $1.item.title }
        let groups = seriesGroups(library.entries)
        List {
            if let offlineRetry { offlineNotice(offlineRetry) }
            Section {
                summary(library)
                Toggle(isOn: Binding(get: { downloads.wifiOnly }, set: { downloads.setWifiOnly($0) })) {
                    Text("downloads.wifiOnly").foregroundStyle(Theme.text)
                }
                .tint(Theme.accent)
            } footer: {
                Text("downloads.wifiOnlyHint")
            }
            .listRowBackground(Theme.surface)

            if library.entries.isEmpty {
                Section {
                    VStack(spacing: 10) {
                        Image(systemName: "arrow.down.circle").font(.system(size: 38, weight: .light)).foregroundStyle(Theme.textFaint)
                        Text("downloads.empty").font(.headline).foregroundStyle(Theme.text)
                        Text("downloads.emptyHint").font(.subheadline).foregroundStyle(Theme.textMuted).multilineTextAlignment(.center)
                    }
                    .frame(maxWidth: .infinity).padding(.vertical, 28)
                }
                .listRowBackground(Color.clear)
            }

            if !movies.isEmpty {
                Section {
                    ForEach(movies) { row($0) }
                } header: {
                    Text("downloads.movies").font(Theme.head(14)).foregroundStyle(Theme.textMuted)
                }
                .listRowBackground(Theme.surface)
            }

            ForEach(groups, id: \.id) { group in
                Section {
                    ForEach(group.episodes) { row($0) }
                } header: {
                    seriesHeader(group)
                }
                .listRowBackground(Theme.surface)
            }

            if !library.subscriptions.isEmpty {
                Section {
                    ForEach(library.subscriptions) { sub in
                        VStack(alignment: .leading, spacing: 3) {
                            Text(verbatim: sub.title).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text)
                            Text("downloads.autoLine \(sub.count) \(qualityName(sub.quality))").font(.caption).foregroundStyle(Theme.textMuted)
                        }
                        .swipeActions(edge: .trailing) {
                            Button(role: .destructive) { downloads.unsubscribe(sub.seriesId) } label: {
                                Label("downloads.stopAuto", systemImage: "xmark")
                            }
                        }
                    }
                } header: {
                    Text("downloads.auto").font(Theme.head(14)).foregroundStyle(Theme.textMuted)
                } footer: {
                    Text("downloads.autoHint")
                }
                .listRowBackground(Theme.surface)
            }

            if !library.entries.isEmpty || !library.subscriptions.isEmpty {
                Section {
                    Button(role: .destructive) { confirmDeleteAll = true } label: { Text("downloads.deleteAll") }
                }
                .listRowBackground(Theme.surface)
            }
        }
        .listStyle(.insetGrouped)
        .scrollContentBackground(.hidden)
        .background(Theme.background.ignoresSafeArea())
        .navigationTitle(Text("downloads.title"))
        .refreshable { await downloads.sync() }
        .confirmationDialog(Text("downloads.deleteAllConfirm"), isPresented: $confirmDeleteAll, titleVisibility: .visible) {
            Button(role: .destructive) { downloads.deleteAll() } label: { Text("downloads.deleteAll") }
        }
        .fullScreenCover(item: $playing) { entry in
            OfflinePlayerView(entry: entry, music: app?.music)
        }
    }

    private struct SeriesGroup {
        let id: String
        let title: String
        let poster: String?
        let episodes: [OfflineEntry]
    }

    private func seriesGroups(_ entries: [OfflineEntry]) -> [SeriesGroup] {
        var order: [String] = []
        var byId: [String: [OfflineEntry]] = [:]
        for entry in entries {
            guard let id = entry.seriesId else { continue }
            if byId[id] == nil { order.append(id) }
            byId[id, default: []].append(entry)
        }
        return order.map { id in
            let episodes = (byId[id] ?? []).sorted {
                ($0.item.seasonNumber ?? 0, $0.item.episodeNumber ?? 0) < ($1.item.seasonNumber ?? 0, $1.item.episodeNumber ?? 0)
            }
            let first = episodes.first
            return SeriesGroup(id: id, title: first?.series?.title ?? first?.item.seriesTitle ?? "",
                               poster: episodes.compactMap(\.seriesPosterFile).first, episodes: episodes)
        }
        .sorted { $0.title < $1.title }
    }

    private func offlineNotice(_ retry: @escaping () async -> Void) -> some View {
        Section {
            VStack(alignment: .leading, spacing: 8) {
                Label("downloads.offlineTitle", systemImage: "wifi.slash").font(.headline).foregroundStyle(Theme.text)
                Text("downloads.offlineHint").font(.subheadline).foregroundStyle(Theme.textMuted)
                Button {
                    Task {
                        retrying = true
                        await retry()
                        retrying = false
                    }
                } label: {
                    if retrying { ProgressView() } else { Text("downloads.retryConnection") }
                }
                .buttonStyle(MutedButtonStyle())
                .disabled(retrying)
                .padding(.top, 4)
            }
            .padding(.vertical, 6)
        }
        .listRowBackground(Theme.surface2)
    }

    private func summary(_ library: OfflineLibrary) -> some View {
        let done = library.entries.filter { $0.state == .done }.count
        return HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text("downloads.count \(done)").font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text)
                Text("downloads.used \(OfflineStorage.format(OfflineStorage.totalBytes(library.entries)))")
                    .font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
            }
            Spacer()
            if downloads.syncing { ProgressView() }
        }
    }

    private func seriesHeader(_ group: SeriesGroup) -> some View {
        HStack(spacing: 10) {
            LocalArtwork(url: downloads.localURL(group.poster), title: group.title)
                .frame(width: 28, height: 42)
                .clipShape(RoundedRectangle(cornerRadius: 3, style: .continuous))
            Text(verbatim: group.title).font(Theme.head(14)).foregroundStyle(Theme.text).lineLimit(1)
            Spacer()
            Text(verbatim: OfflineStorage.format(OfflineStorage.totalBytes(group.episodes)))
                .font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
        }
        .textCase(nil)
    }

    private func row(_ entry: OfflineEntry) -> some View {
        Button {
            if entry.state == .done {
                playing = entry
            }
        } label: {
            DownloadRow(entry: entry)
        }
        .buttonStyle(.plain)
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) { downloads.delete(entry.itemId) } label: {
                Label("downloads.delete", systemImage: "trash")
            }
        }
        .swipeActions(edge: .leading) {
            if entry.state == .failed {
                Button { downloads.retry(entry.itemId) } label: { Label("downloads.retry", systemImage: "arrow.clockwise") }
                    .tint(Theme.accent)
            }
        }
    }
}

/// One downloaded item with its state.
struct DownloadRow: View {
    let entry: OfflineEntry
    private var downloads: DownloadManager { .shared }

    var body: some View {
        let isEpisode = entry.item.kind == "episode"
        HStack(spacing: 14) {
            LocalArtwork(url: downloads.localURL(entry.posterFile), title: isEpisode ? "" : entry.item.title)
                .frame(width: isEpisode ? 96 : 44, height: isEpisode ? 54 : 66)
                .overlay(alignment: .bottom) {
                    if entry.state == .done, entry.durationMs ?? 0 > 0, entry.resumeMs > 0 {
                        ProgressStrip(value: min(1, Double(entry.resumeMs) / Double(entry.durationMs ?? 1))).padding(4)
                    }
                }
                .clipShape(RoundedRectangle(cornerRadius: 5, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 5, style: .continuous).strokeBorder(Theme.border))
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: isEpisode ? seasonEpisode(entry.item) : entry.item.title)
                    .font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(2)
                DownloadStateLine(entry: entry)
            }
            Spacer(minLength: 0)
            if entry.state == .done {
                Image(systemName: entry.isPlayed ? "checkmark" : "play.fill")
                    .font(.footnote.bold()).foregroundStyle(entry.isPlayed ? Theme.accent : Theme.text)
            }
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())
    }

    private func seasonEpisode(_ it: ItemSummary) -> String {
        guard let s = it.seasonNumber, let e = it.episodeNumber else { return it.title }
        return "S\(String(format: "%02d", s))E\(String(format: "%02d", e)) · \(it.title)"
    }
}

/// "Preparing · 40 %", "Downloading · 12 %", the size, or the failure.
struct DownloadStateLine: View {
    let entry: OfflineEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            switch entry.state {
            case .waiting:
                Text("downloads.state.waiting").font(.caption).foregroundStyle(Theme.textMuted)
            case .transcoding:
                Text("downloads.state.preparing \(percent(entry.stageProgress))").font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
                ProgressStrip(value: entry.stageProgress).frame(maxWidth: 180).clipShape(Capsule())
            case .downloading:
                Text("downloads.state.downloading \(percent(entry.stageProgress))").font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
                ProgressStrip(value: entry.stageProgress).frame(maxWidth: 180).clipShape(Capsule())
            case .done:
                Text(verbatim: [qualityName(entry.quality), OfflineStorage.format(OfflineStorage.bytes(entry))].joined(separator: " · "))
                    .font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
            case .failed:
                Text("downloads.state.failed").font(.caption).foregroundStyle(.red)
            }
        }
    }

    private func percent(_ value: Double) -> String {
        value.formatted(.percent.precision(.fractionLength(0)))
    }
}

/// Localized name of a download quality.
func qualityName(_ q: DownloadQuality) -> String {
    switch q {
    case .original: Loc.string("downloads.quality.original")
    case .source: Loc.string("downloads.quality.source")
    case .p1080: "1080p"
    case .p720: "720p"
    }
}

/// Shown when the server cannot be reached at launch: the downloads still play.
struct OfflineRootView: View {
    let server: SavedServer
    let retry: () async -> Void
    private var downloads: DownloadManager { .shared }

    var body: some View {
        NavigationStack {
            DownloadsView(offlineRetry: retry)
        }
        .onChange(of: downloads.online) { _, up in
            if up { Task { await retry() } }
        }
    }
}
