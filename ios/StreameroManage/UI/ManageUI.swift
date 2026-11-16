import SwiftUI

/// A translated message for a Manage error. The services' own messages are English and
/// shown as they are (they name fields and values the service uses).
func arrErrorText(_ error: Error) -> Text {
    guard let arr = error as? ArrError else { return errorMessage(error) }
    switch arr {
    case .unauthenticated: return Text("manage.error.unauthenticated")
    case .forbidden: return Text("manage.error.forbidden")
    case .notConfigured: return Text("manage.error.not_configured")
    case .unreachable(let message):
        if let message, !message.isEmpty { return Text("manage.error.unreachable") + Text(verbatim: " (\(message))") }
        return Text("manage.error.unreachable")
    case .invalidPath: return Text("manage.error.invalid_path")
    case .tooLarge: return Text("manage.error.too_large")
    case .validation(let failures):
        let lines = failures.map { f in f.propertyName.isEmpty ? f.message : "\(f.propertyName): \(f.message)" }
        return Text(verbatim: lines.joined(separator: "\n"))
    case .http(let status, let message):
        if let message { return Text(verbatim: "\(status): \(message)") }
        return Text("manage.error.http") + Text(verbatim: " \(status)")
    case .network: return Text("err_network")
    }
}

/// An error line for Manage screens.
struct ArrErrorLine: View {
    let error: Error?
    var body: some View {
        if let error { arrErrorText(error).font(.subheadline).foregroundStyle(.red) }
    }
}

/// A full-screen problem with a retry button.
struct ArrErrorState: View {
    let error: Error
    let retry: () async -> Void

    var body: some View {
        VStack(spacing: 14) {
            Image(systemName: icon).font(.system(size: 34, weight: .light)).foregroundStyle(Theme.textFaint)
            arrErrorText(error).multilineTextAlignment(.center).foregroundStyle(Theme.textMuted)
            Button { Task { await retry() } } label: { Text("manage.retry") }
                .buttonStyle(MutedButtonStyle())
        }
        .padding(32)
        .frame(maxWidth: .infinity)
    }

    private var icon: String {
        guard let arr = error as? ArrError else { return "exclamationmark.triangle" }
        switch arr {
        case .notConfigured: return "wrench.and.screwdriver"
        case .unreachable, .network: return "wifi.exclamationmark"
        default: return "exclamationmark.triangle"
        }
    }
}

/// Dark list/form background for every Manage screen.
extension View {
    func manageBackground() -> some View {
        scrollContentBackground(.hidden).background(Theme.background)
    }

    /// A short confirmation that slides in at the top ("Search started").
    func manageNotice(_ notice: Binding<ManageNotice?>) -> some View {
        modifier(NoticeModifier(notice: notice))
    }
}

/// A transient message after an action.
struct ManageNotice: Equatable, Identifiable {
    let id = UUID()
    let text: LocalizedStringKey
    var failed = false

    static func == (a: ManageNotice, b: ManageNotice) -> Bool { a.id == b.id }
}

private struct NoticeModifier: ViewModifier {
    @Binding var notice: ManageNotice?

    func body(content: Content) -> some View {
        content.overlay(alignment: .top) {
            if let notice {
                HStack(spacing: 8) {
                    Image(systemName: notice.failed ? "exclamationmark.circle.fill" : "checkmark.circle.fill")
                        .foregroundStyle(notice.failed ? .red : Theme.accent)
                    Text(notice.text).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text)
                }
                .padding(.horizontal, 16).padding(.vertical, 10)
                .background(Theme.surface2, in: Capsule())
                .overlay(Capsule().stroke(Theme.border))
                .padding(.top, 8)
                .transition(.move(edge: .top).combined(with: .opacity))
                .task(id: notice.id) {
                    try? await Task.sleep(for: .seconds(2.2))
                    withAnimation(.easeOut(duration: 0.25)) { self.notice = nil }
                }
            }
        }
        .animation(.spring(response: 0.35, dampingFraction: 0.85), value: notice)
        .sensoryFeedback(.success, trigger: notice) { _, now in now != nil && now?.failed == false }
    }
}

/// A small status pill with server text (versions, quality names).
struct PlainBadge: View {
    let text: String
    var tone: TagBadge.Tone = .neutral

    var body: some View {
        Text(verbatim: text)
            .font(.caption2.weight(.bold))
            .foregroundStyle(color)
            .padding(.horizontal, 7).padding(.vertical, 3)
            .background(color.opacity(0.14), in: RoundedRectangle(cornerRadius: 4, style: .continuous))
            .lineLimit(1)
    }

    private var color: Color {
        switch tone {
        case .neutral: Theme.textMuted
        case .success: .green
        case .accent: Theme.accent
        case .danger: .red
        }
    }
}

/// A thin progress bar in the accent colour.
struct ThinProgress: View {
    let value: Double

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(Theme.text.opacity(0.1))
                Capsule().fill(Theme.accent).frame(width: geo.size.width * max(0, min(1, value)))
            }
        }
        .frame(height: 4)
        .animation(.easeOut(duration: 0.4), value: value)
    }
}

/// Artwork of a Radarr/Sonarr/Lidarr item: the service's cached copy through the proxy,
/// or the public remote URL for lookup results that are not in the library yet.
struct ArrPoster: View {
    let client: ArrClient
    let item: JSONValue
    var coverType = "poster"
    var title: String = ""

    var body: some View {
        let image = Self.pick(item["images"].list, coverType)
        if let local = client.mediaCoverURL(image?["url"].string) {
            AuthImage(url: local, token: client.token, title: title)
        } else if let remote = (image?["remoteUrl"].string).flatMap(URL.init(string:)) {
            Color.clear.overlay {
                AsyncImage(url: remote) { phase in
                    if let img = phase.image {
                        img.resizable().scaledToFill()
                    } else {
                        PosterPlaceholder(title: title)
                    }
                }
            }
            .clipped()
        } else {
            PosterPlaceholder(title: title)
        }
    }

    static func pick(_ images: [JSONValue], _ type: String) -> JSONValue? {
        images.first { $0["coverType"].string == type }
            ?? images.first { $0["coverType"].string == "cover" }
            ?? images.first { $0["coverType"].string == "poster" }
    }
}

/// A public image (TMDB artwork from Seerr).
struct RemotePoster: View {
    let url: URL?
    var title: String = ""

    var body: some View {
        Color.clear.overlay {
            if let url {
                AsyncImage(url: url) { phase in
                    if let img = phase.image { img.resizable().scaledToFill() } else { PosterPlaceholder(title: title) }
                }
            } else {
                PosterPlaceholder(title: title)
            }
        }
        .clipped()
    }
}

// MARK: Dates and sizes

enum ArrDate {
    nonisolated(unsafe) private static let fractional: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    nonisolated(unsafe) private static let plain: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    nonisolated(unsafe) private static let dayOnly: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withFullDate]
        return f
    }()

    /// Parses the services' timestamps ("2024-05-01T12:30:00Z", with or without fractions, or a day).
    static func parse(_ s: String?) -> Date? {
        guard let s, !s.isEmpty else { return nil }
        if let d = fractional.date(from: s) ?? plain.date(from: s) { return d }
        // Seven-digit fractions (.NET) are too long for ISO8601DateFormatter.
        if let dot = s.firstIndex(of: "."), let z = s.lastIndex(where: { $0 == "Z" || $0 == "+" }), dot < z {
            let trimmed = String(s[..<dot]) + String(s[z...])
            if let d = plain.date(from: trimmed) { return d }
        }
        return dayOnly.date(from: String(s.prefix(10)))
    }

    /// "Today at 18:00", "12 May 2025".
    static func short(_ s: String?) -> String {
        guard let d = parse(s) else { return s ?? "" }
        return d.formatted(date: .abbreviated, time: .shortened)
    }

    static func day(_ d: Date) -> String {
        d.formatted(.dateTime.weekday(.wide).day().month(.wide))
    }

    static func iso(_ d: Date) -> String { plain.string(from: d) }
}

func byteText(_ value: JSONValue) -> String {
    guard let n = value.int64 ?? value.double.map({ Int64($0) }) else { return "" }
    return byteSize(n)
}

/// A labelled value row for detail screens.
struct InfoRow: View {
    let title: LocalizedStringKey
    let value: String

    var body: some View {
        LabeledContent {
            Text(verbatim: value).foregroundStyle(Theme.textMuted).multilineTextAlignment(.trailing).textSelection(.enabled)
        } label: {
            Text(title)
        }
    }
}

/// A navigation row with an icon, used by the hub and the service menus.
struct MenuRow: View {
    let title: LocalizedStringKey
    let icon: String
    var detail: String? = nil

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: icon)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(Theme.accent)
                .frame(width: 30, height: 30)
                .background(Theme.accent.opacity(0.14), in: RoundedRectangle(cornerRadius: 7, style: .continuous))
            Text(title).foregroundStyle(Theme.text)
            Spacer()
            if let detail { Text(verbatim: detail).font(.subheadline).foregroundStyle(Theme.textMuted) }
        }
    }
}

/// An empty list explained.
struct EmptyRow: View {
    let text: LocalizedStringKey
    var body: some View {
        Text(text).font(.subheadline).foregroundStyle(Theme.textFaint).frame(maxWidth: .infinity, alignment: .center).padding(.vertical, 18)
    }
}
