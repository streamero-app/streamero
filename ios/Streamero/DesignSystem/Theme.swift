import SwiftUI

/// Design tokens. Views use only these, so the SCYDE theme can replace them later.
enum Theme {
    static let background = Color(red: 0.043, green: 0.043, blue: 0.063)
    static let surface = Color(red: 0.082, green: 0.082, blue: 0.11)
    static let surface2 = Color(red: 0.118, green: 0.118, blue: 0.157)
    static let border = Color(red: 0.165, green: 0.165, blue: 0.212)
    static let text = Color(red: 0.95, green: 0.95, blue: 0.96)
    static let textMuted = Color(red: 0.6, green: 0.6, blue: 0.67)
    static let accent = Color(red: 0.898, green: 0.282, blue: 0.302)
    static let cornerRadius: CGFloat = 14
}
