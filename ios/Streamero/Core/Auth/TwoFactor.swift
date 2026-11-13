import CoreImage
import CoreImage.CIFilterBuiltins
import Foundation
import UIKit

/// Helpers for turning on two-factor sign-in (TOTP): the QR code is drawn on the phone from the
/// otpauth:// URI with CoreImage, so it stays sharp at any size.
enum TwoFactor {
    /// The QR payload for an otpauth URI; nil for anything that is not one.
    static func qrMessage(for otpauthURI: String) -> Data? {
        guard let url = URL(string: otpauthURI), url.scheme?.lowercased() == "otpauth",
              url.host()?.lowercased() == "totp" else { return nil }
        return Data(otpauthURI.utf8)
    }

    /// The base32 secret inside an otpauth URI (for manual entry when the server sent none).
    static func secret(in otpauthURI: String) -> String? {
        guard qrMessage(for: otpauthURI) != nil,
              let items = URLComponents(string: otpauthURI)?.queryItems,
              let secret = items.first(where: { $0.name.lowercased() == "secret" })?.value, !secret.isEmpty else { return nil }
        return secret
    }

    /// A crisp QR code image (`scale` device pixels per module), or nil when the input is not an otpauth URI.
    static func qrImage(for otpauthURI: String, scale: CGFloat = 10) -> UIImage? {
        guard let message = qrMessage(for: otpauthURI) else { return nil }
        let filter = CIFilter.qrCodeGenerator()
        filter.message = message
        filter.correctionLevel = "M"
        guard let output = filter.outputImage?.transformed(by: CGAffineTransform(scaleX: scale, y: scale)),
              let cgImage = CIContext().createCGImage(output, from: output.extent) else { return nil }
        return UIImage(cgImage: cgImage)
    }

    /// The code typed from the authenticator app without spaces (apps show "123 456").
    static func normalizedCode(_ input: String) -> String {
        input.filter { !$0.isWhitespace && $0 != "-" }
    }

    /// 6 to 8 digits (the server accepts up to 16 characters; authenticator codes are digits).
    static func isValidCode(_ input: String) -> Bool {
        let code = normalizedCode(input)
        return (6...8).contains(code.count) && code.allSatisfy { $0.isASCII && $0.isNumber }
    }

    /// Recovery codes as plain text for copying or sharing.
    static func recoveryText(_ codes: [String]) -> String { codes.joined(separator: "\n") }
}

/// The add/edit form for a profile (same rules as the web form and the API schema).
struct ProfileForm: Equatable {
    static let colors = ["#e5484d", "#f76b15", "#ffc53d", "#3dd68c", "#0090ff", "#8e4ec6", "#d6409f", "#6e6e80"]
    static let ratings = [0, 6, 7, 12, 13, 15, 16, 18]
    /// The server allows six profiles besides the account itself.
    static let maxProfiles = 6

    var name = ""
    var color = ProfileForm.colors[0]
    var isKid = false
    var maxRating: Int?
    var pin = ""
    var removePin = false

    init() {}

    init(_ profile: Profile) {
        name = profile.name
        color = profile.color
        isKid = profile.isKid
        maxRating = profile.maxRating
    }

    enum Problem: Equatable {
        case name
        case pin
    }

    var problem: Problem? {
        let trimmed = name.trimmingCharacters(in: .whitespaces)
        if trimmed.isEmpty || trimmed.count > 40 { return .name }
        if !pin.isEmpty && !Self.isValidPin(pin) { return .pin }
        return nil
    }

    static func isValidPin(_ pin: String) -> Bool {
        (4...6).contains(pin.count) && pin.allSatisfy { $0.isASCII && $0.isNumber }
    }

    static func canAdd(existing count: Int) -> Bool { count < maxProfiles }

    /// An empty PIN keeps the current one; removing wins over nothing typed.
    var input: ProfileInput {
        ProfileInput(name: name.trimmingCharacters(in: .whitespaces), color: color, isKid: isKid, maxRating: maxRating,
                     pin: pin.isEmpty || removePin ? nil : pin, removePin: removePin ? true : nil)
    }
}
