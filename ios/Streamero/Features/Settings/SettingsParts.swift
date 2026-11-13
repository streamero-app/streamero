import SwiftUI
import UIKit

/// A small status pill ("On", "Kids", "This device").
struct TagBadge: View {
    enum Tone { case neutral, success, accent, danger }

    let text: LocalizedStringKey
    var tone: Tone = .neutral

    var body: some View {
        Text(text)
            .font(.caption2.weight(.bold))
            .textCase(.uppercase)
            .foregroundStyle(foreground)
            .padding(.horizontal, 7).padding(.vertical, 3)
            .background(foreground.opacity(0.14), in: RoundedRectangle(cornerRadius: 4, style: .continuous))
    }

    private var foreground: Color {
        switch tone {
        case .neutral: Theme.textMuted
        case .success: .green
        case .accent: Theme.accent
        case .danger: .red
        }
    }
}

/// A muted explanation at the top of a settings page.
struct IntroText: View {
    let text: LocalizedStringKey
    var body: some View {
        Text(text).font(.subheadline).foregroundStyle(Theme.textMuted).listRowBackground(Color.clear)
    }
}

/// A one-line hint under a form field.
struct FieldHint: View {
    let text: LocalizedStringKey
    var body: some View {
        Text(text).font(.caption).foregroundStyle(Theme.textFaint)
    }
}

/// Copies text to the clipboard and confirms it briefly.
struct CopyButton: View {
    let text: String
    @State private var copied = false

    var body: some View {
        Button {
            UIPasteboard.general.string = text
            copied = true
            Task {
                try? await Task.sleep(for: .seconds(2))
                copied = false
            }
        } label: {
            Label(copied ? LocalizedStringKey("common.copied") : LocalizedStringKey("common.copy"), systemImage: copied ? "checkmark" : "doc.on.doc")
        }
        .sensoryFeedback(.success, trigger: copied) { _, now in now }
    }
}

/// The system share sheet for a file (a downloaded backup).
struct ShareSheet: UIViewControllerRepresentable {
    let items: [Any]

    func makeUIViewController(context: Context) -> UIActivityViewController {
        UIActivityViewController(activityItems: items, applicationActivities: nil)
    }

    func updateUIViewController(_ controller: UIActivityViewController, context: Context) {}
}

/// A file to hand to the share sheet (Identifiable for `.sheet(item:)`).
struct SharedFile: Identifiable {
    let url: URL
    var id: String { url.path }
}

/// A labelled text field for settings forms (label above, value below, like the web forms).
struct LabeledField: View {
    let title: LocalizedStringKey
    @Binding var text: String
    var prompt: String? = nil
    var secure = false
    var keyboard: UIKeyboardType = .default

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.caption).foregroundStyle(Theme.textMuted)
            Group {
                if secure {
                    SecureField(text: $text, prompt: prompt.map { Text(verbatim: $0) }) { Text(title) }
                } else {
                    TextField(text: $text, prompt: prompt.map { Text(verbatim: $0) }) { Text(title) }
                }
            }
            .keyboardType(keyboard)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        }
    }
}

/// Server text shown as is; nil when missing or empty (for `dotJoined`).
func plain(_ s: String?) -> Text? {
    guard let s, !s.isEmpty else { return nil }
    return Text(verbatim: s)
}

/// "a · b · c" from translated and verbatim parts (plurals stay with the localized Text).
func dotJoined(_ parts: [Text?]) -> Text {
    let present = parts.compactMap { $0 }
    guard var result = present.first else { return Text(verbatim: "") }
    for part in present.dropFirst() { result = result + Text(verbatim: " · ") + part }
    return result
}

/// Formats a byte count ("1.2 GB").
func byteSize(_ bytes: Int64) -> String {
    ByteCountFormatter.string(fromByteCount: bytes, countStyle: .file)
}
