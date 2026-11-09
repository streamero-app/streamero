import SwiftUI

/// Search Bazarr's providers and download one offer, or let Bazarr fetch what is missing.
/// Same permission rule as the web and Android clients.
struct SubtitleFinderButton: View {
    let itemID: String
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var open = false

    var body: some View {
        if app.canFindSubtitles {
            Button { open = true } label: {
                Label("subs_find", systemImage: "captions.bubble").frame(maxWidth: .infinity)
            }
            .buttonStyle(MutedButtonStyle())
            .sheet(isPresented: $open) { SubtitleFinderSheet(itemID: itemID, client: client) }
        }
    }
}

struct SubtitleFinderSheet: View {
    let itemID: String
    let client: MediaClient
    @Environment(\.dismiss) private var dismiss
    @State private var result: SubtitleSearch?
    @State private var error: Error?
    @State private var noticeKey: LocalizedStringKey?
    @State private var busy: String?
    @State private var language: String?

    var body: some View {
        NavigationStack {
            Group {
                if let result {
                    content(result)
                } else if error == nil {
                    VStack(spacing: 12) {
                        ProgressView().tint(Theme.text)
                        Text("subs_searching").font(.subheadline).foregroundStyle(Theme.textMuted)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    ErrorLine(error: error).padding()
                }
            }
            .background(Theme.background)
            .navigationTitle(Text("subs_find"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("close") { dismiss() } } }
            .task { await search() }
        }
    }

    private func search() async {
        do {
            result = try await client.searchSubtitles(item: itemID)
        } catch { self.error = error }
    }

    @ViewBuilder private func content(_ r: SubtitleSearch) -> some View {
        if !r.configured {
            Text("subs_not_configured").foregroundStyle(Theme.textMuted).padding(20).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        } else {
            List {
                Section {
                    Button { Task { await fetchMissing() } } label: { Label("subs_auto", systemImage: "sparkles") }
                    if let noticeKey { Text(noticeKey).foregroundStyle(.green).font(.subheadline) }
                    ErrorLine(error: error)
                }
                languageFilter(r)
                Section {
                    let shown = r.offers.filter { language == nil || $0.language == language }
                    if shown.isEmpty { Text("subs_none").foregroundStyle(Theme.textMuted) }
                    ForEach(shown) { o in row(o) }
                }
            }
            .scrollContentBackground(.hidden)
        }
    }

    @ViewBuilder private func languageFilter(_ r: SubtitleSearch) -> some View {
        let languages = Array(Set(r.offers.map(\.language))).sorted()
        if languages.count > 1 {
            Section {
                Picker(selection: $language) {
                    Text("subs_all").tag(String?.none)
                    ForEach(languages, id: \.self) { Text(verbatim: $0.languageName).tag(String?.some($0)) }
                } label: { Text("language") }
            }
        }
    }

    private func row(_ o: SubtitleOffer) -> some View {
        HStack(spacing: 12) {
            Text(verbatim: "\(Int(o.score))%").font(.subheadline.weight(.semibold)).foregroundStyle(Theme.textMuted).frame(width: 44, alignment: .leading)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 4) {
                    Text(verbatim: o.language.languageName)
                    if o.forced { Text(verbatim: "·"); Text("subs_forced") }
                    if o.hearingImpaired { Text(verbatim: "·"); Text("subs_hi") }
                }
                .font(.subheadline.weight(.semibold)).lineLimit(1)
                Text(verbatim: [o.provider, o.uploader, o.release?.first].compactMap { $0 }.joined(separator: " · "))
                    .font(.footnote).foregroundStyle(Theme.textMuted).lineLimit(1)
            }
            Spacer(minLength: 0)
            Button { Task { await download(o) } } label: {
                if busy == o.id { ProgressView() } else { Image(systemName: "arrow.down.circle") }
            }
            .accessibilityLabel(Text("subs_download"))
            .buttonStyle(.borderless)
            .disabled(busy != nil)
        }
    }

    private func fetchMissing() async {
        do {
            try await client.searchMissingSubtitles(item: itemID)
            noticeKey = "subs_auto_started"
        } catch { self.error = error }
    }

    private func download(_ o: SubtitleOffer) async {
        busy = o.id
        defer { busy = nil }
        do {
            try await client.downloadSubtitle(item: itemID, offer: o)
            noticeKey = "subs_downloaded"
        } catch { self.error = error }
    }
}
