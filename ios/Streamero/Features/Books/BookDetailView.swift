import SwiftUI

/// What a reader opens: one book, by id.
struct ReaderTarget: Identifiable, Hashable {
    let id: String
    let title: String
}

/// A book, comic or audiobook page: cover, authors, series, description, Read or Listen, chapters.
struct BookDetailView: View {
    let detail: ItemDetail
    let client: MediaClient
    @Environment(AppState.self) private var app
    @Environment(\.locale) private var locale
    @State private var reader: ReaderTarget?
    @State private var manifest: AudiobookManifest?
    @State private var playerOpen = false
    @State private var favorite: Bool?

    private var it: ItemSummary { detail.item }
    private var isAudiobook: Bool { it.kind == "audiobook" }
    private var format: String? { detail.book?.format ?? it.bookFormat }
    private var authors: [String] { detail.book?.authors ?? it.authors ?? [] }
    private var seriesName: String? { detail.book?.seriesName ?? it.seriesName }
    private var seriesIndex: Double? { detail.book?.seriesIndex ?? it.seriesIndex }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                header
                actions
                if let text = detail.book?.description ?? detail.overview, !text.isEmpty {
                    Text(verbatim: text).font(.callout).foregroundStyle(Theme.text.opacity(0.85)).lineSpacing(3)
                }
                facts
                if isAudiobook { chapters }
            }
            .padding(.horizontal, 20).padding(.top, 12).padding(.bottom, 32)
        }
        .background(AmbientBackground(colors: it.colors))
        .task(id: it.id) {
            if isAudiobook { manifest = try? await client.openAudiobook(it.id) }
        }
        .fullScreenCover(item: $reader, onDismiss: { app.reloadTick += 1 }) { target in
            ReaderScreen(target: target, client: client)
                .environment(app)
                .environment(\.locale, locale)
        }
        .sheet(isPresented: $playerOpen) {
            AudiobookPlayerView(client: client).environment(app)
        }
    }

    private var header: some View {
        VStack(spacing: 14) {
            BookCover(item: it, client: client)
                .frame(maxWidth: isAudiobook ? 240 : 200)
                .shadow(color: .black.opacity(0.6), radius: 24, y: 14)
                .frame(maxWidth: .infinity)
            VStack(alignment: .leading, spacing: 8) {
                if let seriesName {
                    NavigationLink(value: Route.books(BookListRoute(libraryId: detail.libraryId, kind: it.kind, title: seriesName, series: seriesName))) {
                        Text(verbatim: seriesLabel(seriesName, seriesIndex)).font(Theme.head(14)).textCase(.uppercase).foregroundStyle(Theme.textMuted)
                    }
                }
                Text(verbatim: it.title).font(Theme.display(30)).foregroundStyle(Theme.text).lineLimit(4).minimumScaleFactor(0.6)
                if !authors.isEmpty {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 6) {
                            ForEach(authors, id: \.self) { a in
                                NavigationLink(value: Route.books(BookListRoute(libraryId: detail.libraryId, kind: it.kind, title: a, author: a))) {
                                    Text(verbatim: a).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.accent)
                                }
                            }
                        }
                    }
                }
                if let narrators = detail.book?.narrators, !narrators.isEmpty {
                    Text("books.narratedBy \(narrators.joined(separator: ", "))").font(.subheadline).foregroundStyle(Theme.textMuted)
                }
                Text(verbatim: metaLine).font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var metaLine: String {
        var parts: [String] = []
        if let format { parts.append(format.uppercased()) }
        if isAudiobook {
            let d = manifest?.durationMs ?? it.durationMs ?? 0
            if d > 0 { parts.append(clock(d)) }
        } else if let pages = detail.book?.pageCount, pages > 0 {
            parts.append(String(localized: "books.pages \(pages)"))
        }
        if let published = detail.book?.published { parts.append(String(published.prefix(4))) } else if let y = it.year { parts.append(String(y)) }
        return parts.joined(separator: " · ")
    }

    private var progressValue: Double? {
        if isAudiobook, let m = manifest, m.durationMs > 0, m.positionMs > 0 { return min(1, Double(m.positionMs) / Double(m.durationMs)) }
        return ReadingMath.coverProgress(it)
    }

    @ViewBuilder private var actions: some View {
        VStack(alignment: .leading, spacing: 12) {
            if isAudiobook {
                let started = (manifest?.positionMs ?? it.positionMs ?? 0) > 0
                Button {
                    app.audiobook.open(it)
                    playerOpen = true
                } label: {
                    Group {
                        if started, let pos = manifest?.positionMs ?? it.positionMs {
                            Label { Text("books.continueAt \(clock(pos))") } icon: { Image(systemName: "headphones") }
                        } else {
                            Label("books.listen", systemImage: "headphones")
                        }
                    }
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(PrimaryButtonStyle(hex: it.colors?.first))
            } else if BookFormat.readsInApp(format) {
                let started = (it.progression ?? 0) > 0 && it.played != true
                Button { reader = ReaderTarget(id: it.id, title: it.title) } label: {
                    Label(started ? "books.continue" : "books.read", systemImage: "book.fill").frame(maxWidth: .infinity)
                }
                .buttonStyle(PrimaryButtonStyle(hex: it.colors?.first))
            } else {
                Label("books.readInWeb", systemImage: "globe")
                    .font(.subheadline).foregroundStyle(Theme.textMuted)
                    .padding(14).frame(maxWidth: .infinity, alignment: .leading)
                    .background(Theme.surface, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            }
            HStack(spacing: 12) {
                let fav = favorite ?? it.favorite ?? false
                Button { Task { await toggleFavorite(fav) } } label: { Image(systemName: fav ? "checkmark" : "plus") }
                    .buttonStyle(CircleButtonStyle(active: fav))
                    .accessibilityLabel(Text("library.addToList"))
                if let p = progressValue {
                    VStack(alignment: .leading, spacing: 4) {
                        ProgressStrip(value: p).frame(maxWidth: 200).clipShape(Capsule())
                        Text(verbatim: p.formatted(.percent.precision(.fractionLength(0)))).font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
                    }
                }
            }
        }
    }

    private func toggleFavorite(_ current: Bool) async {
        do {
            try await client.setUserData(item: it.id, favorite: !current)
            favorite = !current
        } catch {}
    }

    @ViewBuilder private var facts: some View {
        let b = detail.book
        VStack(alignment: .leading, spacing: 10) {
            fact("books.publisher", b?.publisher)
            fact("released", b?.published ?? detail.premiereDate)
            fact("language", b?.language.map { $0.languageName })
            fact("books.isbn", b?.isbn)
            if !detail.genres.isEmpty { fact("books.genres", detail.genres.joined(separator: ", ")) }
        }
    }

    @ViewBuilder private func fact(_ label: LocalizedStringKey, _ value: String?) -> some View {
        if let value, !value.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                Text(label).font(.caption2.bold()).textCase(.uppercase).foregroundStyle(Theme.textFaint)
                Text(verbatim: value).font(.subheadline).foregroundStyle(Theme.text)
            }
        }
    }

    @ViewBuilder private var chapters: some View {
        if let m = manifest, m.chapters.count > 1 {
            let timeline = AudiobookTimeline(m)
            let current = app.audiobook.item?.id == it.id ? app.audiobook.chapterIndex : timeline.chapterIndex(at: m.positionMs)
            VStack(alignment: .leading, spacing: 4) {
                SectionHead(title: "chapters").padding(.bottom, 6)
                ForEach(Array(timeline.chapters.enumerated()), id: \.offset) { i, c in
                    Button {
                        app.audiobook.open(it, startAt: c.startMs)
                        playerOpen = true
                    } label: {
                        ChapterRow(index: i, chapter: c, end: timeline.chapterEnd(i), current: i == current)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }
}

/// A chapter in a list: number, title, length.
struct ChapterRow: View {
    let index: Int
    let chapter: AudiobookChapter
    let end: Int64
    let current: Bool

    var body: some View {
        HStack(spacing: 12) {
            Text(verbatim: String(index + 1)).font(.subheadline.monospacedDigit())
                .foregroundStyle(current ? Theme.accent : Theme.textFaint).frame(width: 28, alignment: .leading)
            Text(verbatim: chapter.title).foregroundStyle(current ? Theme.accent : Theme.text)
                .fontWeight(current ? .semibold : .regular).lineLimit(2)
            Spacer(minLength: 0)
            Text(verbatim: clock(max(0, end - chapter.startMs))).font(.footnote.monospacedDigit()).foregroundStyle(Theme.textFaint)
        }
        .padding(.vertical, 10)
        .contentShape(Rectangle())
    }
}
