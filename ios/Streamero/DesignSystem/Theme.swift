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

/// The room lit by a title: two soft lights in its colours over near-black.
struct AmbientBackground: View {
    var colors: [String]?

    var body: some View {
        let a = Color(hex: colors?.first ?? "#ffaa45")
        let b = Color(hex: colors?.dropFirst().first ?? colors?.first ?? "#2f8f9d")
        ZStack {
            Theme.background
            RadialGradient(colors: [a.opacity(0.38), .clear], center: UnitPoint(x: 0.85, y: 0.05), startRadius: 0, endRadius: 520)
            RadialGradient(colors: [b.opacity(0.26), .clear], center: UnitPoint(x: 0.05, y: 0.95), startRadius: 0, endRadius: 480)
        }
        .ignoresSafeArea()
        .animation(.easeOut(duration: 0.9), value: colors)
    }
}

extension Color {
    /// "#rrggbb" (profile and ambient colours from the server).
    init(hex: String) {
        let v = UInt64(hex.trimmingCharacters(in: CharacterSet(charactersIn: "#")), radix: 16) ?? 0xffaa45
        self.init(red: Double((v >> 16) & 0xff) / 255, green: Double((v >> 8) & 0xff) / 255, blue: Double(v & 0xff) / 255)
    }

    /// Readable label colour on this colour.
    static func ink(onHex hex: String?) -> Color {
        let v = UInt64((hex ?? "#ffffff").trimmingCharacters(in: CharacterSet(charactersIn: "#")), radix: 16) ?? 0xffffff
        let r = Double((v >> 16) & 0xff) / 255, g = Double((v >> 8) & 0xff) / 255, b = Double(v & 0xff) / 255
        return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.55 ? .black : .white
    }
}

/// Primary action, lit in the title's colour (white when there is none).
struct PrimaryButtonStyle: ButtonStyle {
    var hex: String? = nil

    func makeBody(configuration: Configuration) -> some View {
        let fill = hex.map { Color(hex: $0) } ?? .white
        let ink = hex == nil ? Color.black : Color.ink(onHex: hex)
        return configuration.label
            .font(.body.weight(.bold))
            .foregroundStyle(ink)
            .tint(ink)
            .padding(.horizontal, 24).frame(height: 50)
            .background(fill.opacity(configuration.isPressed ? 0.8 : 1), in: Capsule())
            .shadow(color: fill.opacity(hex == nil ? 0 : 0.5), radius: 16, y: 8)
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
            .padding(.horizontal, 20).frame(height: 50)
            .background(.white.opacity(configuration.isPressed ? 0.2 : 0.1), in: Capsule())
            .overlay(Capsule().strokeBorder(.white.opacity(0.15)))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.spring(response: 0.25, dampingFraction: 0.8), value: configuration.isPressed)
    }
}
