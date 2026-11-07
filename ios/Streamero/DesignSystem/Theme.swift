import SwiftUI

/// Design tokens — "projection room": warm black, one amber light. Same palette as the web client.
enum Theme {
    static let background = Color(red: 0.043, green: 0.035, blue: 0.027)
    static let surface = Color(red: 0.082, green: 0.071, blue: 0.059)
    static let surface2 = Color(red: 0.122, green: 0.106, blue: 0.090)
    static let border = Color(red: 1, green: 0.925, blue: 0.84).opacity(0.09)
    static let text = Color(red: 0.965, green: 0.941, blue: 0.91)
    static let textMuted = Color(red: 0.66, green: 0.62, blue: 0.576)
    static let textFaint = Color(red: 0.48, green: 0.447, blue: 0.408)
    static let accent = Color(red: 1, green: 0.667, blue: 0.271)
    static let accentContrast = Color(red: 0.11, green: 0.063, blue: 0.024)
    static let cornerRadius: CGFloat = 14
    static let posterRadius: CGFloat = 10

    /// Film-title serif (New York) for display text; UI text stays SF.
    static func display(_ size: CGFloat) -> Font { .system(size: size, weight: .regular, design: .serif) }

    /// Stable, muted hue for artwork placeholders.
    static func hue(for title: String) -> Double {
        var h = 0
        for u in title.unicodeScalars { h = (h &* 31 &+ Int(u.value)) % 360 }
        return Double(abs(h)) / 360
    }
}

/// Primary pill: amber, presses down slightly.
struct PrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(Theme.accentContrast)
            .padding(.horizontal, 22).frame(height: 50)
            .background(Theme.accent, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .shadow(color: Theme.accent.opacity(0.35), radius: 14, y: 6)
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.spring(response: 0.25, dampingFraction: 0.8), value: configuration.isPressed)
    }
}

/// Round glass icon toggle (favorite, watched).
struct CircleButtonStyle: ButtonStyle {
    var active = false
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 18, weight: .semibold))
            .foregroundStyle(active ? Theme.accent : Theme.text)
            .frame(width: 50, height: 50)
            .background(active ? Theme.accent.opacity(0.16) : Color.white.opacity(0.08), in: Circle())
            .overlay(Circle().strokeBorder(active ? Theme.accent.opacity(0.4) : Theme.border))
            .scaleEffect(configuration.isPressed ? 0.94 : 1)
            .animation(.spring(response: 0.25, dampingFraction: 0.7), value: configuration.isPressed)
    }
}

/// Cards lift a touch when pressed (the touch equivalent of hover).
struct CardPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.spring(response: 0.3, dampingFraction: 0.75), value: configuration.isPressed)
    }
}
