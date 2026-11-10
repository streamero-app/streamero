import SwiftUI

/// A title page: play options, facts, seasons, chapters, cast and technical details.
/// Music items (album, artist) open their own layouts.
struct ItemView: View {
    let itemID: String
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var detail: ItemDetail?
    @State private var error: Error?
    @State private var fileIndex = 0
    @State private var audio: Int?
    @State private var sub: Int?
    @State private var season: String?

    var body: some View {
        Group {
            if let d = detail {
                switch d.item.kind {
                case "album": AlbumView(detail: d, client: client)
                case "artist": ArtistView(detail: d, client: client)
                case "track": trackRedirect(d)
                default: page(d)
                }
            } else if let error {
                ErrorLine(error: error).padding(20).frame(maxWidth: .infinity, maxHeight: .infinity).background(Theme.background)
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity).background(Theme.background)
            }
        }
        .navigationBarTitleDisplayMode(.inline)
        .task(id: "\(itemID)-\(app.reloadTick)") { await reload() }
    }

    @ViewBuilder private func trackRedirect(_ d: ItemDetail) -> some View {
        if let parent = d.parentId {
            ItemView(itemID: parent, client: client)
        } else {
            EmptyView()
        }
    }

    private func reload() async {
        do {
            let d = try await client.item(itemID)
            detail = d
            error = nil
            if season == nil { season = defaultSeason(d) }
        } catch is CancellationError {
            return
        } catch {
            if detail == nil { self.error = error }
        }
    }

    private func defaultSeason(_ d: ItemDetail) -> String? {
        let seasons = d.children.filter { $0.kind == "season" }
        return seasons.first { $0.seasonNumber == 1 }?.id ?? seasons.first?.id
    }

    private func page(_ d: ItemDetail) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                header(d)
                actions(d).padding(.horizontal, 20)
                if let overview = d.overview {
                    Text(verbatim: overview).font(.callout).foregroundStyle(Theme.text.opacity(0.85)).lineSpacing(3).padding(.horizontal, 20)
                }
                facts(d).padding(.horizontal, 20)
                seasonsSection(d)
                episodesSection(d)
                chaptersSection(d)
                castSection(d)
                mediaInfo(d).padding(.horizontal, 20)
            }
            .padding(.bottom, 32)
        }
        .ignoresSafeArea(edges: .top)
        .background(AmbientBackground(colors: d.item.colors))
        .toolbarBackground(.hidden, for: .navigationBar)
    }

    @ViewBuilder private func header(_ d: ItemDetail) -> some View {
        let it = d.item
        let isEpisode = it.kind == "episode"
        ZStack(alignment: .bottomLeading) {
            Group {
                if d.hasBackdrop {
                    AuthImage(url: client.imageURL(item: it.id, type: "backdrop"), token: client.token)
                } else if it.hasPoster {
                    AuthImage(url: client.imageURL(item: it.id, type: isEpisode ? "still" : "poster"), token: client.token)
                        .blur(radius: isEpisode ? 0 : 40)
                } else {
                    PosterPlaceholder(title: "")
                }
            }
            .frame(height: 480).frame(maxWidth: .infinity).clipped()
            .mask(LinearGradient(colors: [.black, .black, .black.opacity(0.6), .clear], startPoint: .top, endPoint: .bottom))
            LinearGradient(colors: [.black.opacity(0.45), .clear, .black.opacity(0.25), .black.opacity(0.45)],
                           startPoint: .top, endPoint: .bottom)
            HStack(alignment: .bottom, spacing: 16) {
                if !isEpisode {
                    AuthImage(url: it.hasPoster ? client.imageURL(item: it.id, type: "poster") : nil, token: client.token, title: it.title, year: it.year)
                        .frame(width: 112, height: 168)
                        .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
                        .overlay(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous).strokeBorder(.white.opacity(0.12)))
                        .shadow(color: .black.opacity(0.7), radius: 20, y: 12)
                }
                VStack(alignment: .leading, spacing: 8) {
                    if let series = it.seriesTitle, let seriesId = it.seriesId {
                        NavigationLink(value: Route.item(seriesId)) {
                            Text(verbatim: series).font(Theme.head(15)).textCase(.uppercase).foregroundStyle(Theme.textMuted)
                        }
                    }
                    if isEpisode {
                        Text(verbatim: episodeTitle(it)).font(Theme.display(30)).foregroundStyle(Theme.text).lineLimit(3).minimumScaleFactor(0.5)
                    } else {
                        TitleArt(id: it.id, title: it.title, hasLogo: d.hasLogo == true, client: client, size: 34, maxHeight: 90)
                    }
                    MetaLine(item: it)
                    let extras = factsLine(d)
                    if !extras.isEmpty {
                        Text(verbatim: extras.joined(separator: " · ")).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(1)
                    }
                    if let tagline = d.tagline { Text(verbatim: tagline).font(.callout.italic()).foregroundStyle(Theme.text.opacity(0.8)) }
                }
            }
            .padding(.horizontal, 20)
        }
        .frame(height: 480)
    }

    private func factsLine(_ d: ItemDetail) -> [String] {
        var parts = Array(d.genres.prefix(2))
        if let rating = d.officialRating { parts.append(rating) }
        parts += qualityTags(height: d.item.videoHeight, hdr: d.item.hdr)
        return parts
    }

    private func actions(_ d: ItemDetail) -> some View {
        let it = d.item
        let fileID = d.files.count > 1 && d.files.indices.contains(fileIndex) ? d.files[fileIndex].id : nil
        // The pickers use -1 ("automatic") for audio and -2 ("automatic") for subtitles; neither is sent.
        let chosenAudio = audio.flatMap { $0 >= 0 ? $0 : nil }
        let chosenSub = sub.flatMap { $0 >= -1 ? $0 : nil }
        return VStack(alignment: .leading, spacing: 12) {
            if !d.files.isEmpty {
                Button { app.play(PlayTarget(id: it.id, file: fileID, audio: chosenAudio, sub: chosenSub)) } label: {
                    Group {
                        if let pos = it.positionMs, pos > 0 {
                            Label { Text("resume_from \(clock(pos))") } icon: { Image(systemName: "play.fill") }
                        } else {
                            Label("player.play", systemImage: "play.fill")
                        }
                    }
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(PrimaryButtonStyle(hex: it.colors?.first))
                if (it.positionMs ?? 0) > 0 {
                    Button { app.play(PlayTarget(id: it.id, start: 0, file: fileID, audio: chosenAudio, sub: chosenSub)) } label: {
                        Label("from_start", systemImage: "arrow.counterclockwise").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(MutedButtonStyle())
                }
            }
            HStack(spacing: 12) {
                Button { Task { await toggle(favorite: it) } } label: { Image(systemName: it.favorite == true ? "checkmark" : "plus") }
                    .buttonStyle(CircleButtonStyle(active: it.favorite == true))
                    .accessibilityLabel(Text("library.addToList"))
                Button { Task { await toggle(played: it) } } label: {
                    Label(it.played == true ? "watched" : "mark_watched", systemImage: "checkmark")
                }
                .buttonStyle(ToggleCapsuleStyle(active: it.played == true))
                .accessibilityLabel(Text("library.watched"))
            }
            if let p = progress(it) { ProgressStrip(value: p).frame(maxWidth: 240) }
            playOptions(d)
            if !d.files.isEmpty { SubtitleFinderButton(itemID: it.id, client: client) }
        }
    }

    private func toggle(favorite it: ItemSummary) async {
        try? await client.setUserData(item: it.id, favorite: !(it.favorite ?? false))
        await reload()
    }

    private func toggle(played it: ItemSummary) async {
        try? await client.setUserData(item: it.id, played: !(it.played ?? false))
        await reload()
    }

    @ViewBuilder private func playOptions(_ d: ItemDetail) -> some View {
        if d.files.indices.contains(fileIndex) {
            let f = d.files[fileIndex]
            let audios = f.audio ?? []
            let subs = f.subtitles ?? []
            if d.files.count > 1 || audios.count > 1 || !subs.isEmpty {
                VStack(spacing: 10) {
                    if d.files.count > 1 {
                        optionRow("version", selection: $fileIndex,
                                  options: d.files.enumerated().map { i, x in Opt(i, verbatim: versionLabel(x)) })
                            .onChange(of: fileIndex) { _, _ in audio = nil; sub = nil }
                    }
                    if audios.count > 1 {
                        optionRow("audio", selection: withDefault($audio, -1),
                                  options: [Opt(-1, "automatic")] + audios.enumerated().map { i, a in Opt(i, verbatim: audioLabel(a)) })
                    }
                    if !subs.isEmpty {
                        let options: [Opt<Int>] = [Opt(-2, "automatic"), Opt(-1, "off")] + subs.enumerated().map { i, s in Opt(i, verbatim: s.language?.uppercased() ?? "?") }
                        optionRow("subtitles", selection: withDefault($sub, -2), options: options)
                    }
                }
            }
        }
    }

    private func optionRow<V: Hashable>(_ title: LocalizedStringKey, selection: Binding<V>, options: [Opt<V>]) -> some View {
        HStack {
            Text(title).font(.subheadline).foregroundStyle(Theme.textMuted)
            Spacer()
            Picker(selection: selection) {
                ForEach(options) { $0.label.tag($0.value) }
            } label: { EmptyView() }
            .pickerStyle(.menu)
            .tint(Theme.text)
        }
        .padding(.horizontal, 12).frame(height: 44)
        .background(Theme.surface2, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
    }

    private func versionLabel(_ f: MediaFile) -> String {
        f.label ?? [f.height.map { "\($0)p" }, f.hdr, f.videoCodec?.uppercased()].compactMap { $0 }.joined(separator: " ")
    }

    private func audioLabel(_ a: AudioStream) -> String {
        [a.language?.uppercased(), a.codec.uppercased(), a.channels.map { "\($0)ch" }, a.title].compactMap { $0 }.joined(separator: " · ")
    }

    private func facts(_ d: ItemDetail) -> some View {
        let directors = d.crew.filter { $0.role == "Director" }.map(\.name).joined(separator: ", ")
        let writers = d.crew.filter { $0.role != "Director" }.map(\.name).joined(separator: ", ")
        let starring = d.cast.prefix(5).map(\.name).joined(separator: ", ")
        let collections = (d.collections ?? []).map(\.name).joined(separator: ", ")
        let it = d.item
        return VStack(alignment: .leading, spacing: 10) {
            fact("directed_by", directors)
            fact("starring", starring)
            fact("written_by", writers)
            fact("studio", (d.studios ?? []).joined(separator: ", "))
            fact("released", d.premiereDate ?? "")
            fact("collection", collections)
            HStack(spacing: 14) {
                if let imdb = d.providerIds["imdb"], let url = URL(string: "https://www.imdb.com/title/\(imdb)/") {
                    Link(destination: url) { Text(verbatim: "IMDb") }
                }
                if let tmdb = d.providerIds["tmdb"], let url = URL(string: "https://www.themoviedb.org/\(it.kind == "series" ? "tv" : "movie")/\(tmdb)") {
                    Link(destination: url) { Text(verbatim: "TMDb") }
                }
            }
            .font(.subheadline.weight(.semibold)).foregroundStyle(Theme.textMuted)
        }
    }

    @ViewBuilder private func fact(_ label: LocalizedStringKey, _ value: String) -> some View {
        if !value.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                Text(label).font(.caption2.bold()).textCase(.uppercase).foregroundStyle(Theme.textFaint)
                Text(verbatim: value).font(.subheadline).foregroundStyle(Theme.text)
            }
        }
    }

    @ViewBuilder private func seasonsSection(_ d: ItemDetail) -> some View {
        let seasons = d.children.filter { $0.kind == "season" }
        if !seasons.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                SectionHead(title: "episodes").padding(.horizontal, 20)
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(seasons) { s in
                            Button { season = s.id } label: {
                                Group {
                                    if s.seasonNumber == 0 { Text("specials") } else { Text("season_n \(s.seasonNumber ?? 0)") }
                                }
                                .padding(.horizontal, 14).frame(height: 42)
                            }
                            .buttonStyle(ToggleCapsuleStyle(active: s.id == season))
                        }
                    }
                    .padding(.horizontal, 20)
                }
                if let season { SeasonEpisodes(seasonID: season, client: client).padding(.horizontal, 20) }
            }
        }
    }

    @ViewBuilder private func episodesSection(_ d: ItemDetail) -> some View {
        let episodes = d.children.filter { $0.kind == "episode" }
        if !episodes.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                SectionHead(title: "episodes")
                ForEach(episodes) { e in EpisodeButton(episode: e, client: client) }
            }
            .padding(.horizontal, 20)
        }
    }

    @ViewBuilder private func chaptersSection(_ d: ItemDetail) -> some View {
        if let chapters = d.chapters, chapters.count > 1 {
            Shelf(title: "chapters") {
                ForEach(chapters, id: \.self) { c in
                    Button { app.play(PlayTarget(id: d.item.id, start: c.startMs)) } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            Theme.surface2
                                .frame(width: 200, height: 112)
                                .overlay {
                                    if let tp = d.trickplay {
                                        TrickFrame(info: tp, ms: c.startMs + 20_000, client: client).frame(width: 200, height: 112)
                                    }
                                }
                                .overlay(alignment: .bottomLeading) { QualityTag(text: clock(c.startMs)).padding(6) }
                                .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
                            Text(verbatim: c.title).font(.footnote).foregroundStyle(Theme.text).lineLimit(1).frame(width: 200, alignment: .leading)
                        }
                    }
                    .buttonStyle(CardPressStyle())
                }
            }
        }
    }

    @ViewBuilder private func castSection(_ d: ItemDetail) -> some View {
        if !d.cast.isEmpty {
            Shelf(title: "cast") {
                ForEach(Array(d.cast.prefix(20).enumerated()), id: \.offset) { _, p in
                    VStack(spacing: 6) {
                        Text(verbatim: initials(p.name)).font(Theme.display(24)).foregroundStyle(Theme.text.opacity(0.7))
                            .frame(width: 72, height: 72)
                            .overlay {
                                if let url = p.imageUrl.flatMap(client.absolute) {
                                    AuthImage(url: url, token: client.token).clipShape(Circle())
                                }
                            }
                            .background(RadialGradient(colors: [Color(hue: Theme.hue(for: p.name), saturation: 0.25, brightness: 0.22), Theme.surface],
                                                       center: .topLeading, startRadius: 0, endRadius: 80), in: Circle())
                            .overlay(Circle().strokeBorder(Theme.border))
                        Text(verbatim: p.name).font(.caption.weight(.medium)).lineLimit(2).multilineTextAlignment(.center)
                        if let role = p.role { Text(verbatim: role).font(.caption2).foregroundStyle(Theme.textMuted).lineLimit(1) }
                    }
                    .frame(width: 92)
                }
            }
        }
    }

    private func initials(_ name: String) -> String {
        name.split(separator: " ").prefix(2).compactMap(\.first).map { String($0).uppercased() }.joined()
    }

    private func audioLine(_ tracks: [AudioStream]) -> String {
        tracks.map { a -> String in
            let parts: [String?] = [a.language?.uppercased(), a.codec.uppercased(), a.channels.map { "\($0)ch" }]
            return parts.compactMap { $0 }.joined(separator: " ")
        }.joined(separator: " · ")
    }

    @ViewBuilder private func mediaInfo(_ d: ItemDetail) -> some View {
        if !d.files.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                SectionHead(title: "media_info")
                ForEach(d.files) { f in
                    VStack(alignment: .leading, spacing: 4) {
                        let title = [f.label, f.height.map { "\($0)p" }, f.hdr, f.videoCodec?.uppercased(), f.container.uppercased()].compactMap { $0 }.joined(separator: " · ")
                        Text(verbatim: title).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text)
                        if let a = f.audio, !a.isEmpty {
                            (Text("audio") + Text(verbatim: ": " + audioLine(a)))
                                .font(.caption).foregroundStyle(Theme.textMuted)
                        }
                        if let s = f.subtitles, !s.isEmpty {
                            (Text("subtitles") + Text(verbatim: ": " + s.map { $0.language?.uppercased() ?? "?" }.joined(separator: " · ")))
                                .font(.caption).foregroundStyle(Theme.textMuted)
                        }
                    }
                    .padding(14)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Theme.surface, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                }
            }
        }
    }
}

/// Rectangular toggle with text ("Watched", season chips); paper white when on.
struct ToggleCapsuleStyle: ButtonStyle {
    var active = false
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(active ? Theme.background : Theme.text)
            .padding(.horizontal, 14).frame(minHeight: 44)
            .background(active ? Theme.text : Color.white.opacity(0.12), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.spring(response: 0.25, dampingFraction: 0.8), value: configuration.isPressed)
    }
}

struct SeasonEpisodes: View {
    let seasonID: String
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var episodes: [ItemSummary]?

    var body: some View {
        Group {
            if let episodes {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(episodes) { e in EpisodeButton(episode: e, client: client) }
                }
            } else {
                LoadingView()
            }
        }
        .task(id: "\(seasonID)-\(app.reloadTick)") {
            episodes = nil
            let d = try? await client.item(seasonID)
            episodes = (d?.children ?? []).filter { $0.kind == "episode" }
        }
    }
}

/// An episode row: plays when tapped, like the web and Android clients.
struct EpisodeButton: View {
    let episode: ItemSummary
    let client: MediaClient
    @Environment(AppState.self) private var app

    var body: some View {
        Button { app.play(PlayTarget(id: episode.id)) } label: { EpisodeRow(item: episode, client: client) }
            .buttonStyle(CardPressStyle())
    }
}

/// One frame from the trickplay sheets, used as a chapter picture.
struct TrickFrame: View {
    let info: TrickplayInfo
    let ms: Int64
    let client: MediaClient

    var body: some View {
        let index = min(max(0, Int(ms) / max(1, info.intervalMs)), max(0, info.count - 1))
        let perSheet = max(1, info.columns * info.rows)
        let sheet = index / perSheet
        let local = index % perSheet
        let col = local % max(1, info.columns)
        let row = local / max(1, info.columns)
        if info.sheetUrls.indices.contains(sheet), let url = client.absolute(info.sheetUrls[sheet]) {
            GeometryReader { geo in
                AuthImage(url: url, token: client.token)
                    .frame(width: geo.size.width * CGFloat(info.columns), height: geo.size.height * CGFloat(info.rows))
                    .offset(x: -CGFloat(col) * geo.size.width, y: -CGFloat(row) * geo.size.height)
            }
            .clipped()
        }
    }
}
