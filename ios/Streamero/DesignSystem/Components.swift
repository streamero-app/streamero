import SwiftUI
import UIKit

/// "m:ss" or "h:mm:ss".
func clock(_ ms: Int64) -> String {
    let s = ms / 1000
    if s >= 3600 { return String(format: "%d:%02d:%02d", s / 3600, (s % 3600) / 60, s % 60) }
    return String(format: "%d:%02d", s / 60, s % 60)
}

/// "2024-05-01 12:30" from a server timestamp.
func shortTime(_ s: String, length: Int = 16) -> String {
    String(s.prefix(length)).replacingOccurrences(of: "T", with: " ")
}

/// A translated message for an API error.
func errorMessage(_ error: Error) -> Text {
    guard let api = error as? APIError else { return Text("err_network") }
    switch api.code {
    case "invalid_credentials": return Text("err_credentials")
    case "totp_required": return Text("err_totp")
    case "forbidden": return Text("err_forbidden")
    case "upstream_failed": return Text("err_upstream")
    default:
        // The server's own message is English; unknown codes get a translated fallback.
        if api.status == 0 || api.status >= 500 { return Text("err_network") }
        return Text("err_generic")
    }
}

struct ErrorLine: View {
    let error: Error?
    var body: some View {
        if let error { errorMessage(error).font(.subheadline).foregroundStyle(.red) }
    }
}

struct LoadingView: View {
    var body: some View {
        ProgressView().tint(Theme.text).frame(maxWidth: .infinity).padding(40)
    }
}

/// Heading inside a screen ("Episodes").
struct SectionHead: View {
    let title: LocalizedStringKey
    var body: some View {
        Text(title).headFont().textCase(.uppercase).foregroundStyle(Theme.text)
    }
}

/// Page title: wide uppercase type.
struct PageTitle: View {
    let title: LocalizedStringKey
    var body: some View {
        Text(title).displayFont(34).foregroundStyle(Theme.text)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct QualityTag: View {
    let text: String
    var body: some View {
        Text(verbatim: text).font(.system(size: 10, weight: .bold)).foregroundStyle(Theme.text)
            .padding(.horizontal, 5).padding(.vertical, 1)
            .background(.black.opacity(0.75), in: RoundedRectangle(cornerRadius: 3))
    }
}

func qualityTags(height: Int?, hdr: String?) -> [String] {
    var out: [String] = []
    if let height {
        if height >= 2000 { out.append("4K") } else if height >= 700 { out.append("HD") }
    }
    if let hdr, !hdr.isEmpty {
        let lower = hdr.lowercased()
        if lower.contains("dolby") || lower.contains("dovi") || lower.contains("dv") { out.append("DV") }
        else if lower.contains("hlg") { out.append("HLG") }
        else { out.append("HDR") }
    }
    return out
}

/// Square-cornered profile or account picture with an initial behind it.
struct AvatarView: View {
    let name: String
    let color: String?
    let url: URL?
    let token: String
    var size: CGFloat = 64

    var body: some View {
        let hex = color ?? "#8A5A3A"
        ZStack {
            LinearGradient(colors: [Color(hex: hex), Color(hex: hex).opacity(0.55)], startPoint: .topLeading, endPoint: .bottomTrailing)
            Text(verbatim: String(name.prefix(1)).uppercased())
                .font(.system(size: size * 0.42, weight: .bold)).foregroundStyle(.white)
            if let url {
                AuthImage(url: url, token: token).frame(width: size, height: size).clipped()
            }
        }
        .frame(width: size, height: size)
        .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
    }
}

/// A choice in a settings picker.
struct Opt<V: Hashable>: Identifiable {
    let value: V
    let label: Text
    var id: V { value }

    init(_ value: V, _ key: LocalizedStringKey) {
        self.value = value
        self.label = Text(key)
    }

    init(_ value: V, verbatim: String) {
        self.value = value
        self.label = Text(verbatim: verbatim)
    }
}

/// A labelled picker row for forms.
struct OptPicker<V: Hashable>: View {
    let title: LocalizedStringKey
    @Binding var selection: V
    let options: [Opt<V>]

    var body: some View {
        Picker(selection: $selection) {
            ForEach(options) { option in option.label.tag(option.value) }
        } label: {
            Text(title)
        }
    }
}

/// Binds an optional setting with a default shown when it is unset.
@MainActor
func withDefault<T: Sendable>(_ binding: Binding<T?>, _ fallback: T) -> Binding<T> {
    Binding(get: { binding.wrappedValue ?? fallback }, set: { binding.wrappedValue = $0 })
}

/// Empty text clears an optional setting.
@MainActor
func optionalText(_ binding: Binding<String?>) -> Binding<String> {
    Binding(get: { binding.wrappedValue ?? "" }, set: { binding.wrappedValue = $0.isEmpty ? nil : $0 })
}

extension String {
    /// Display name of a language code in the current locale.
    var languageName: String {
        Locale.current.localizedString(forLanguageCode: self) ?? self
    }
}

/// Zero clears an optional number (for "no limit" / "original" choices).
@MainActor
func zeroAsNil(_ binding: Binding<Int?>) -> Binding<Int> {
    Binding(get: { binding.wrappedValue ?? 0 }, set: { binding.wrappedValue = $0 == 0 ? nil : $0 })
}

/// An optional string edited as text, kept even when empty (clearing a server URL).
@MainActor
func keptText(_ binding: Binding<String?>) -> Binding<String> {
    Binding(get: { binding.wrappedValue ?? "" }, set: { binding.wrappedValue = $0 })
}
