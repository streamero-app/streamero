import SwiftUI

/// Download button and state for a movie or an episode on its page.
struct DownloadItemControl: View {
    let detail: ItemDetail
    let fileId: String?
    let audioIndex: Int?
    /// Container of the chosen version (original files that iOS cannot play get a warning).
    let container: String?
    @Environment(AppState.self) private var app
    @State private var choosing = false
    @State private var busy = false
    @State private var error: Error?
    @State private var playing: OfflineEntry?

    private var downloads: DownloadManager { .shared }

    var body: some View {
        let entry = downloads.entry(detail.item.id)
        VStack(alignment: .leading, spacing: 8) {
            if let entry {
                DownloadStatusCard(entry: entry, play: {
                    playing = entry
                })
            } else {
                Button { choosing = true } label: {
                    Group {
                        if busy { ProgressView() } else { Label("downloads.download", systemImage: "arrow.down.circle") }
                    }
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(MutedButtonStyle())
                .disabled(busy)
            }
            ErrorLine(error: error)
        }
        .sheet(isPresented: $choosing) {
            DownloadOptionsSheet(title: detail.item.title, allowTranscode: Access.canDownloadTranscoded(app.me),
                                 originalPlayable: originalPlayable, scopes: []) { quality, _ in
                Task { await start(quality) }
            }
            .environment(\.locale, app.locale ?? Locale.current)
        }
        .fullScreenCover(item: $playing) { entry in OfflinePlayerView(entry: entry, music: app.music) }
    }

    private var originalPlayable: Bool {
        guard let container else { return true }
        return MediaClient.device.containers.contains(container.lowercased())
    }

    private func start(_ quality: DownloadQuality) async {
        busy = true
        defer { busy = false }
        do {
            try await downloads.download(itemId: detail.item.id, quality: quality, fileId: fileId, audioIndex: audioIndex)
            error = nil
        } catch {
            self.error = error
        }
    }
}

/// Download choices for a series or a season: the season, or the next N episodes from now on.
struct DownloadSeriesControl: View {
    let detail: ItemDetail
    /// The season shown on a series page (the season itself on a season page).
    let seasonId: String?
    let seasonLabel: String?
    @Environment(AppState.self) private var app
    @State private var choosing = false
    @State private var busy = false
    @State private var error: Error?
    @State private var done = false

    private var downloads: DownloadManager { .shared }

    private var seriesId: String {
        detail.item.kind == "series" ? detail.item.id : (detail.item.seriesId ?? detail.parentId ?? detail.item.id)
    }

    private var seriesTitle: String {
        detail.item.kind == "series" ? detail.item.title : (detail.item.seriesTitle ?? detail.item.title)
    }

    var body: some View {
        let local = downloads.library.entries.filter { $0.seriesId == seriesId }
        let sub = downloads.subscription(seriesId)
        VStack(alignment: .leading, spacing: 8) {
            Button { choosing = true } label: {
                Group {
                    if busy { ProgressView() } else { Label("downloads.downloadEpisodes", systemImage: "arrow.down.circle") }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(MutedButtonStyle())
            .disabled(busy)
            if let sub {
                HStack(spacing: 10) {
                    Image(systemName: "arrow.triangle.2.circlepath").foregroundStyle(Theme.accent)
                    Text("downloads.autoLine \(sub.count) \(qualityName(sub.quality))").font(.footnote).foregroundStyle(Theme.text)
                    Spacer()
                    Button { downloads.unsubscribe(seriesId) } label: { Text("downloads.stopAuto").font(.footnote.weight(.semibold)) }
                        .tint(Theme.textMuted)
                }
                .padding(12)
                .background(Theme.surface, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            }
            if !local.isEmpty {
                let ready = local.filter { $0.state == .done }.count
                let pending = local.filter { $0.state.isPending }.count
                HStack(spacing: 6) {
                    Image(systemName: "arrow.down.circle.fill").foregroundStyle(Theme.accent)
                    Text("downloads.seriesLine \(ready) \(OfflineStorage.format(OfflineStorage.totalBytes(local)))")
                    if pending > 0 { Text("downloads.pendingCount \(pending)").foregroundStyle(Theme.textMuted) }
                }
                .font(.footnote.monospacedDigit()).foregroundStyle(Theme.text)
            } else if done {
                Text("downloads.requested").font(.footnote).foregroundStyle(Theme.textMuted)
            }
            ErrorLine(error: error)
        }
        .sheet(isPresented: $choosing) {
            DownloadOptionsSheet(title: seriesTitle, allowTranscode: Access.canDownloadTranscoded(app.me),
                                 originalPlayable: true, seasonLabel: seasonLabel,
                                 scopes: seasonId == nil ? [.next(3)] : [.season, .next(3)]) { quality, scope in
                Task { await start(quality, scope) }
            }
            .environment(\.locale, app.locale ?? Locale.current)
        }
    }

    private func start(_ quality: DownloadQuality, _ scope: DownloadScope) async {
        busy = true
        defer { busy = false }
        do {
            switch scope {
            case .season, .item:
                guard let seasonId else { return }
                try await downloads.downloadSeason(seasonId, seriesId: seriesId, quality: quality)
            case .next(let n):
                try await downloads.subscribe(seriesId: seriesId, title: seriesTitle, count: n, quality: quality)
            }
            error = nil
            done = true
        } catch {
            self.error = error
        }
    }
}

/// The state of a download on the item page, with play, retry and delete.
struct DownloadStatusCard: View {
    let entry: OfflineEntry
    let play: () -> Void
    @State private var confirmDelete = false
    private var downloads: DownloadManager { .shared }

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: icon).font(.title3).foregroundStyle(entry.state == .failed ? .red : Theme.accent)
                .frame(width: 28)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text)
                DownloadStateLine(entry: entry)
            }
            Spacer(minLength: 0)
            if entry.state == .done {
                Button(action: play) { Image(systemName: "play.fill") }
                    .buttonStyle(CircleButtonStyle())
                    .accessibilityLabel(Text("downloads.playOffline"))
            } else if entry.state == .failed {
                Button { downloads.retry(entry.itemId) } label: { Image(systemName: "arrow.clockwise") }
                    .buttonStyle(CircleButtonStyle())
                    .accessibilityLabel(Text("downloads.retry"))
            }
            Button { confirmDelete = true } label: { Image(systemName: entry.state == .done ? "trash" : "xmark") }
                .buttonStyle(CircleButtonStyle())
                .accessibilityLabel(Text(entry.state == .done ? LocalizedStringKey("downloads.delete") : LocalizedStringKey("downloads.cancel")))
        }
        .padding(12)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.cornerRadius, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.cornerRadius, style: .continuous).strokeBorder(Theme.border))
        .confirmationDialog(Text("downloads.deleteConfirm"), isPresented: $confirmDelete, titleVisibility: .visible) {
            Button(role: .destructive) { downloads.delete(entry.itemId) } label: {
                Text(entry.state == .done ? LocalizedStringKey("downloads.delete") : LocalizedStringKey("downloads.cancel"))
            }
        }
    }

    private var icon: String {
        switch entry.state {
        case .done: "checkmark.circle.fill"
        case .failed: "exclamationmark.circle.fill"
        default: "arrow.down.circle"
        }
    }

    private var title: LocalizedStringKey {
        switch entry.state {
        case .done: "downloads.downloaded"
        case .failed: "downloads.failedTitle"
        default: "downloads.inProgress"
        }
    }
}

/// Quality (and for series: what to download) before a download starts.
struct DownloadOptionsSheet: View {
    let title: String
    let allowTranscode: Bool
    let originalPlayable: Bool
    var seasonLabel: String? = nil
    /// Empty for a single movie or episode.
    let scopes: [DownloadScope]
    let confirm: (DownloadQuality, DownloadScope) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var quality: DownloadQuality = .original
    @State private var scopeKind = "season"
    @State private var count = 3

    private static let counts = [1, 3, 5, 10]

    private var qualities: [DownloadQuality] {
        allowTranscode ? DownloadQuality.allCases : [.original]
    }

    private var hasSeason: Bool { scopes.contains(.season) }
    private var hasNext: Bool {
        scopes.contains { scope in
            if case .next = scope { return true }
            return false
        }
    }

    var body: some View {
        NavigationStack {
            Form {
                if hasSeason || hasNext {
                    Section {
                        Picker(selection: $scopeKind) {
                            if hasSeason {
                                if let seasonLabel { Text("downloads.scope.seasonNamed \(seasonLabel)").tag("season") } else { Text("downloads.scope.season").tag("season") }
                            }
                            if hasNext { Text("downloads.scope.next").tag("next") }
                        } label: { EmptyView() }
                        .pickerStyle(.inline)
                        .labelsHidden()
                        if scopeKind == "next" {
                            Picker(selection: $count) {
                                ForEach(Self.counts, id: \.self) { n in Text(verbatim: String(n)).tag(n) }
                            } label: { Text("downloads.episodeCount") }
                            .pickerStyle(.segmented)
                        }
                    } header: {
                        Text("downloads.what")
                    } footer: {
                        if scopeKind == "next" { Text("downloads.autoHint") }
                    }
                    .listRowBackground(Theme.surface)
                }
                Section {
                    Picker(selection: $quality) {
                        ForEach(qualities) { q in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: qualityName(q))
                                Text(qualityHint(q)).font(.caption).foregroundStyle(Theme.textMuted)
                            }
                            .tag(q)
                        }
                    } label: { EmptyView() }
                    .pickerStyle(.inline)
                    .labelsHidden()
                } header: {
                    Text("downloads.quality")
                } footer: {
                    if quality == .original && !originalPlayable {
                        Text("downloads.originalWarning").foregroundStyle(.orange)
                    } else if !allowTranscode {
                        Text("downloads.noTranscodeHint")
                    }
                }
                .listRowBackground(Theme.surface)
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background.ignoresSafeArea())
            .navigationTitle(Text(verbatim: title))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("player.back") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        let scope: DownloadScope = scopes.isEmpty ? .item : (scopeKind == "next" ? .next(count) : .season)
                        confirm(quality, scope)
                        dismiss()
                    } label: { Text("downloads.start").bold() }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .onAppear {
            if allowTranscode && !originalPlayable { quality = .source }
            scopeKind = hasSeason ? "season" : "next"
        }
    }

    private func qualityHint(_ q: DownloadQuality) -> LocalizedStringKey {
        switch q {
        case .original: "downloads.quality.originalHint"
        case .source: "downloads.quality.sourceHint"
        case .p1080: "downloads.quality.1080Hint"
        case .p720: "downloads.quality.720Hint"
        }
    }
}
