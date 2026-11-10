import SwiftUI

/// Full-bleed spotlight: backdrop, scrims, serif title and actions.
struct HeroView: View {
    let item: ItemSummary
    let client: MediaClient
    let onPlay: () -> Void
    var onRandom: (() -> Void)? = nil

    private var playable: Bool { item.kind == "movie" || item.kind == "episode" }

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            Group {
                if item.hasBackdrop == true {
                    AuthImage(url: client.imageURL(item: item.id, type: "backdrop"), token: client.token)
                } else if item.hasPoster {
                    AuthImage(url: client.imageURL(item: item.id, type: "poster"), token: client.token).blur(radius: 40).saturation(1.3)
                } else {
                    PosterPlaceholder(title: "")
                }
            }
            .frame(height: 560).frame(maxWidth: .infinity).clipped()
            // The picture dissolves into the lit room instead of ending in a hard edge.
            .mask(LinearGradient(colors: [.black, .black, .black.opacity(0.6), .clear], startPoint: .top, endPoint: .bottom))
            LinearGradient(colors: [.black.opacity(0.5), .clear, .black.opacity(0.25), .black.opacity(0.45)],
                           startPoint: .top, endPoint: .bottom)
            VStack(alignment: .leading, spacing: 12) {
                TitleArt(id: item.id, title: item.title, hasLogo: item.hasLogo == true, client: client, size: 44)
                MetaLine(item: item)
                HStack(spacing: 12) {
                    if playable {
                        Button(action: onPlay) { Label("player.play", systemImage: "play.fill") }.buttonStyle(PrimaryButtonStyle(hex: item.colors?.first))
                    }
                    NavigationLink(value: Route.item(item.id)) { Label("home.moreInfo", systemImage: "info.circle") }
                        .buttonStyle(MutedButtonStyle())
                    if let onRandom {
                        Button(action: onRandom) { Image(systemName: "shuffle") }
                            .buttonStyle(CircleButtonStyle())
                            .accessibilityLabel(Text("enh_random"))
                    }
                }
                .padding(.top, 4)
            }
            .padding(.horizontal, 20).padding(.bottom, 8)
        }
        .frame(height: 560)
    }
}

/// The title as its logo artwork when there is one, else heavy type.
struct TitleArt: View {
    let id: String
    let title: String
    let hasLogo: Bool
    let client: MediaClient
    var size: CGFloat = 40
    var maxHeight: CGFloat = 110

    var body: some View {
        if hasLogo {
            AuthImage(url: client.imageURL(item: id, type: "logo"), token: client.token, fit: true)
                .frame(maxWidth: 300, maxHeight: maxHeight, alignment: .bottomLeading)
                .shadow(color: .black.opacity(0.5), radius: 10)
                .accessibilityLabel(Text(title))
        } else {
            Text(title).font(Theme.display(size)).foregroundStyle(Theme.text).lineLimit(3).minimumScaleFactor(0.5)
        }
    }
}

struct MetaLine: View {
    let item: ItemSummary

    var body: some View {
        HStack(spacing: 10) {
            if let r = item.rating {
                Label { Text(r, format: .number.precision(.fractionLength(1))) } icon: { Image(systemName: "star.fill").foregroundStyle(Theme.accent) }
                    .labelStyle(.titleAndIcon).fontWeight(.semibold)
            }
            if let y = item.year { Text(String(y)) }
            if let d = item.durationMs, d > 0 { Text(Duration.milliseconds(d), format: .units(allowed: [.hours, .minutes], width: .narrow)) }
        }
        .font(.subheadline.monospacedDigit())
        .foregroundStyle(Theme.text.opacity(0.75))
    }
}

/// Titled horizontal row.
struct Shelf<Content: View>: View {
    let title: LocalizedStringKey
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title).font(Theme.head()).textCase(.uppercase).padding(.horizontal, 20)
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(alignment: .top, spacing: 14) { content() }
                    .padding(.horizontal, 20)
                    .scrollTargetLayout()
            }
            .scrollTargetBehavior(.viewAligned)
        }
    }
}

func progress(_ item: ItemSummary) -> Double? {
    guard let pos = item.positionMs, let dur = item.durationMs, dur > 0, pos > 0 else { return nil }
    return min(1, Double(pos) / Double(dur))
}

struct ProgressStrip: View {
    let value: Double
    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Rectangle().fill(.white.opacity(0.25))
                Rectangle().fill(Theme.accent).frame(width: g.size.width * value)
            }
        }
        .frame(height: 4)
    }
}

struct PosterCard: View {
    let item: ItemSummary
    let client: MediaClient
    @Environment(AppState.self) private var app

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ZStack(alignment: .topTrailing) {
                AuthImage(url: item.hasPoster ? client.imageURL(item: item.id, type: item.kind == "episode" ? "still" : "poster") : nil,
                          token: client.token, title: item.title, year: item.year)
                    .aspectRatio(2 / 3, contentMode: .fill)
                if item.played == true {
                    Image(systemName: "checkmark").font(.caption.bold()).foregroundStyle(Theme.accentContrast)
                        .frame(width: 22, height: 22).background(Theme.accent, in: Circle()).padding(7)
                } else if item.favorite == true {
                    Image(systemName: "bookmark.fill").font(.caption2).foregroundStyle(Theme.accent)
                        .frame(width: 22, height: 22).background(.black.opacity(0.55), in: Circle()).padding(7)
                }
            }
            .overlay(alignment: .bottomLeading) {
                if app.enhancements.qualityTags {
                    HStack(spacing: 4) {
                        ForEach(qualityTags(height: item.videoHeight, hdr: item.hdr), id: \.self) { QualityTag(text: $0) }
                    }
                    .padding(6)
                }
            }
            .overlay(alignment: .bottom) {
                if let p = progress(item) { ProgressStrip(value: p).padding(8) }
            }
            .clipShape(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.posterRadius, style: .continuous).strokeBorder(Theme.border))
            VStack(alignment: .leading, spacing: 2) {
                Text(item.title).font(.footnote.weight(.medium)).foregroundStyle(Theme.text).lineLimit(1)
                Text(item.year.map(String.init) ?? " ").font(.caption2.monospacedDigit()).foregroundStyle(Theme.textMuted)
            }
        }
    }
}

/// Landscape card for continue watching.
struct WideCard: View {
    let item: ItemSummary
    let client: MediaClient
    var showPlay = true
    var width: CGFloat? = 240

    private var art: URL? {
        if item.hasBackdrop == true { return client.imageURL(item: item.id, type: "backdrop") }
        if item.kind == "episode" && item.hasPoster { return client.imageURL(item: item.id, type: "still") }
        if let series = item.seriesId { return client.imageURL(item: series, type: "backdrop") }
        return nil
    }

    var body: some View {
        Color.clear
            .frame(width: width)
            .aspectRatio(16 / 9, contentMode: .fit)
            .overlay { AuthImage(url: art, token: client.token, title: item.title) }
            .overlay(alignment: .bottomLeading) {
                if art != nil {
                    ZStack(alignment: .bottomLeading) {
                        LinearGradient(colors: [.clear, .black.opacity(0.75)], startPoint: .center, endPoint: .bottom)
                        Group {
                            if item.hasLogo == true {
                                AuthImage(url: client.imageURL(item: item.id, type: "logo"), token: client.token, fit: true)
                                    .frame(maxWidth: 130, maxHeight: 40, alignment: .bottomLeading)
                            } else {
                                Text(item.title).font(.subheadline.weight(.heavy)).foregroundStyle(.white).lineLimit(2)
                            }
                        }
                        .padding(.horizontal, 10).padding(.bottom, 12)
                    }
                }
            }
            .overlay {
                if showPlay {
                    Image(systemName: "play.fill").font(.callout).foregroundStyle(.white)
                        .frame(width: 34, height: 34).background(.black.opacity(0.5), in: Circle())
                        .overlay(Circle().strokeBorder(.white, lineWidth: 1.5))
                }
            }
            .overlay(alignment: .bottom) {
                if let p = progress(item) { ProgressStrip(value: p).padding(.horizontal, 8).padding(.bottom, 5) }
            }
            .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
            .contentShape(Rectangle())
    }
}

struct LibraryTile: View {
    let library: Library

    private var icon: String {
        switch library.kind {
        case "movies": "film"
        case "shows": "tv"
        case "music": "music.note"
        default: "film.stack"
        }
    }

    var body: some View {
        HStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 6) {
                Text(library.name).font(Theme.display(26)).foregroundStyle(Theme.text)
                Text("home.items \(library.itemCount)").font(.subheadline).foregroundStyle(Theme.textMuted)
            }
            Spacer()
            Image(systemName: icon).font(.title2).foregroundStyle(Theme.accent.opacity(0.8))
        }
        .padding(20)
        .frame(height: 110)
        .background(
            RadialGradient(colors: [Color(hue: Theme.hue(for: library.name), saturation: 0.25, brightness: 0.16), Theme.surface],
                           center: .bottomLeading, startRadius: 0, endRadius: 360),
            in: RoundedRectangle(cornerRadius: Theme.cornerRadius, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.cornerRadius, style: .continuous).strokeBorder(Theme.border))
    }
}

func episodeTitle(_ it: ItemSummary) -> String {
    guard let e = it.episodeNumber else { return it.title }
    return "E\(String(format: "%02d", e)) · \(it.title)"
}

struct EpisodeRow: View {
    let item: ItemSummary
    let client: MediaClient

    var body: some View {
        HStack(spacing: 14) {
            AuthImage(url: item.hasPoster ? client.imageURL(item: item.id, type: "still") : nil, token: client.token)
                .frame(width: 128, height: 72)
                .overlay(alignment: .bottom) { if let p = progress(item) { ProgressStrip(value: p).padding(6) } }
                .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(Theme.border))
            VStack(alignment: .leading, spacing: 3) {
                if let e = item.episodeNumber {
                    Text("E\(String(format: "%02d", e))").font(.caption.weight(.semibold).monospacedDigit()).foregroundStyle(Theme.accent)
                }
                Text(item.title).font(.subheadline.weight(.medium)).foregroundStyle(Theme.text).lineLimit(2)
            }
            Spacer(minLength: 0)
            if item.played == true { Image(systemName: "checkmark").font(.footnote.bold()).foregroundStyle(Theme.accent) }
        }
        .contentShape(Rectangle())
    }
}

/// Approve a Quick Connect code shown on a TV or browser with this phone's account.
struct QuickConnectApproveView: View {
    let client: MediaClient
    @Environment(\.dismiss) private var dismiss
    @State private var code = ""
    @State private var busy = false
    @State private var message: String?
    @State private var failed = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("quickConnect.code", text: $code)
                        .keyboardType(.numberPad)
                        .font(.title2.monospaced())
                } footer: {
                    Text("quickConnect.approveHint")
                }
                if let message {
                    Text(message).foregroundStyle(failed ? .red : Theme.accent)
                }
                Button {
                    Task { await approve() }
                } label: {
                    if busy { ProgressView() } else { Text("quickConnect.approve").bold() }
                }
                .disabled(busy || code.count < 6)
            }
            .navigationTitle(Text("quickConnect.approveTitle"))
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("player.back") { dismiss() } } }
        }
    }

    private func approve() async {
        busy = true
        defer { busy = false }
        do {
            let device = try await client.authorizeQuickConnect(code: code.trimmingCharacters(in: .whitespaces))
            failed = false
            message = String(localized: "quickConnect.approved \(device)")
            code = ""
        } catch {
            failed = true
            message = String(localized: "quickConnect.expired")
        }
    }
}
