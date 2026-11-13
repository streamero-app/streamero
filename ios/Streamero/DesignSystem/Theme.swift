import SwiftUI
import UIKit

/// Design tokens — "Kino platno" (D-019): the film owns the screen, warm near-black around it,
/// paper-white primary action, titles set wide like opening credits. Same palette as the web client.
enum Theme {
    static let background = Color(red: 0.039, green: 0.031, blue: 0.027)
    static let surface = Color(red: 0.078, green: 0.067, blue: 0.059)
    static let surface2 = Color(red: 0.110, green: 0.094, blue: 0.082)
    static let border = Color(red: 0.957, green: 0.937, blue: 0.914).opacity(0.1)
    static let text = Color(red: 0.957, green: 0.937, blue: 0.914)
    static let textMuted = text.opacity(0.64)
    static let textFaint = text.opacity(0.42)
    /// The film's light at rest; titles bring their own (items.colors).
    static let accent = Color(red: 0.839, green: 0.463, blue: 0.247)
    static let accentContrast = Color(red: 0.043, green: 0.035, blue: 0.031)
    static let cornerRadius: CGFloat = 8
    static let posterRadius: CGFloat = 6

    /// Wide, heavy title type (title cards, hero titles) at a fixed size. Views use
    /// `.displayFont(_:)`, which follows Dynamic Type.
    static func display(_ size: CGFloat) -> Font { .system(size: size, weight: .black).width(.expanded) }
    /// Section heads: the same voice, calmer (views use `.headFont(_:)`).
    static func head(_ size: CGFloat = 19) -> Font { .system(size: size, weight: .heavy).width(.expanded) }

    /// A brand size scaled for the reader's text size. Display type is already large, so its
    /// growth is capped (`maxScale`) to keep titles on screen at accessibility sizes.
    static func scaledSize(_ size: CGFloat, for typeSize: DynamicTypeSize, maxScale: CGFloat) -> CGFloat {
        let traits = UITraitCollection(preferredContentSizeCategory: contentSizeCategory(typeSize))
        let scaled = UIFontMetrics(forTextStyle: .title1).scaledValue(for: size, compatibleWith: traits)
        return min(scaled, size * maxScale)
    }

    static func contentSizeCategory(_ size: DynamicTypeSize) -> UIContentSizeCategory {
        switch size {
        case .xSmall: .extraSmall
        case .small: .small
        case .medium: .medium
        case .large: .large
        case .xLarge: .extraLarge
        case .xxLarge: .extraExtraLarge
        case .xxxLarge: .extraExtraExtraLarge
        case .accessibility1: .accessibilityMedium
        case .accessibility2: .accessibilityLarge
        case .accessibility3: .accessibilityExtraLarge
        case .accessibility4: .accessibilityExtraExtraLarge
        case .accessibility5: .accessibilityExtraExtraExtraLarge
        @unknown default: .large
        }
    }

    /// Height of a full-bleed hero: most of the screen, never more than `maximum`, and lower
    /// when the screen is short (landscape iPhone) so the content below still peeks in.
    static func heroHeight(container: CGFloat, compactHeight: Bool, maximum: CGFloat) -> CGFloat {
        guard container > 0 else { return maximum }
        if compactHeight { return min(maximum, max(220, container * 0.8)) }
        return min(maximum, max(300, container * 0.72))
    }

    /// Stable, muted hue for artwork placeholders.
    static func hue(for title: String) -> Double {
        var h = 0
        for u in title.unicodeScalars { h = (h &* 31 &+ Int(u.value)) % 360 }
        return Double(abs(h)) / 360
    }
}

/// Brand type that follows Dynamic Type (capped, see `Theme.scaledSize`).
struct ThemeFont: ViewModifier {
    let size: CGFloat
    let weight: Font.Weight
    let maxScale: CGFloat
    @Environment(\.dynamicTypeSize) private var typeSize

    func body(content: Content) -> some View {
        content.font(.system(size: Theme.scaledSize(size, for: typeSize, maxScale: maxScale), weight: weight).width(.expanded))
    }
}

/// Sizes a full-bleed hero relative to the scroll view it sits in (see `Theme.heroHeight`).
struct HeroFrame: ViewModifier {
    let maximum: CGFloat
    @Environment(\.verticalSizeClass) private var verticalSizeClass

    func body(content: Content) -> some View {
        let compact = verticalSizeClass == .compact
        content.containerRelativeFrame(.vertical) { length, _ in
            Theme.heroHeight(container: length, compactHeight: compact, maximum: maximum)
        }
    }
}

extension View {
    /// Wide, heavy title type. Pair with `.textCase(.uppercase)`.
    func displayFont(_ size: CGFloat) -> some View { modifier(ThemeFont(size: size, weight: .black, maxScale: 1.35)) }
    /// Section heads: the same voice, calmer.
    func headFont(_ size: CGFloat = 19) -> some View { modifier(ThemeFont(size: size, weight: .heavy, maxScale: 1.6)) }
    func heroFrame(maximum: CGFloat) -> some View { modifier(HeroFrame(maximum: maximum)) }
}

/// The dark around the screen, with the film's light pooling low on the left.
struct AmbientBackground: View {
    var colors: [String]?

    var body: some View {
        let a = Color(hex: colors?.first ?? "#d6763f")
        ZStack {
            Theme.background
            RadialGradient(colors: [a.opacity(0.26), .clear], center: UnitPoint(x: 0, y: 0.75), startRadius: 0, endRadius: 460)
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

/// Primary action: paper white on the dark, plain and rectangular. `hex` is accepted for
/// call sites that pass the film colour; the film's colour stays on the screen, not the button.
struct PrimaryButtonStyle: ButtonStyle {
    var hex: String? = nil

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(Theme.background)
            .tint(Theme.background)
            .padding(.horizontal, 22).frame(height: 50)
            .background(Theme.text.opacity(configuration.isPressed ? 0.8 : 1), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.spring(response: 0.25, dampingFraction: 0.8), value: configuration.isPressed)
    }
}

/// Square icon toggle (favorite, watched); paper white when on.
struct CircleButtonStyle: ButtonStyle {
    var active = false
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 18, weight: .semibold))
            .foregroundStyle(active ? Theme.background : Theme.text)
            .frame(width: 50, height: 50)
            .background(active ? Theme.text : Color.white.opacity(0.12), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
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

/// Secondary action: translucent, same shape as the primary (More info).
struct MutedButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(Theme.text)
            .padding(.horizontal, 20).frame(height: 50)
            .background(.white.opacity(configuration.isPressed ? 0.22 : 0.12), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.spring(response: 0.25, dampingFraction: 0.8), value: configuration.isPressed)
    }
}
