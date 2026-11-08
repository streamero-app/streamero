import SwiftUI

/// Design tokens — streaming-service look on near-black, white primary action, amber brand accent. Same palette as the web client.
enum Theme {
    static let background = Color(red: 0.063, green: 0.063, blue: 0.063)
    static let surface = Color(red: 0.094, green: 0.094, blue: 0.094)
    static let surface2 = Color(red: 0.137, green: 0.137, blue: 0.137)
    static let border = Color.white.opacity(0.09)
    static let text = Color(red: 0.96, green: 0.96, blue: 0.96)
    static let textMuted = Color(red: 0.64, green: 0.64, blue: 0.64)
    static let textFaint = Color(red: 0.45, green: 0.45, blue: 0.45)
    static let accent = Color(red: 1, green: 0.667, blue: 0.271)
    static let accentContrast = Color(red: 0.11, green: 0.063, blue: 0.024)
    static let cornerRadius: CGFloat = 14
    static let posterRadius: CGFloat = 10

    /// Heavy, tight title type for billboards and headings.
    static func display(_ size: CGFloat) -> Font { .system(size: size, weight: .heavy) }

    /// Stable, muted hue for artwork placeholders.
    static func hue(for title: String) -> Double {
        var h = 0
        for u in title.unicodeScalars { h = (h &* 31 &+ Int(u.value)) % 360 }
        return Double(abs(h)) / 360
    }
}

/// Primary action: white with black label (Play), presses down slightly.
struct PrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.bold))
            .foregroundStyle(.black)
            .tint(.black)
            .padding(.horizontal, 22).frame(height: 48)
            .background(.white.opacity(configuration.isPressed ? 0.8 : 1), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
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

/// Secondary action: translucent grey (More info).
struct MutedButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.bold))
            .foregroundStyle(.white)
            .padding(.horizontal, 20).frame(height: 48)
            .background(.white.opacity(configuration.isPressed ? 0.3 : 0.2), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.spring(response: 0.25, dampingFraction: 0.8), value: configuration.isPressed)
    }
}
