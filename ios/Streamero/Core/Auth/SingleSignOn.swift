import CryptoKit
import Foundation
import Security

/// Single sign-on (OpenID Connect) from the app (D-037).
///
/// Redirect choice: the server acts as a broker. The identity provider only redirects to
/// the server's own https callback; the server then sends the browser to the custom URL
/// scheme `streamero://oidc` with a one-time code.
/// - Not associated domains / universal links: they need an entitlement that Feather
///   re-signing drops, and an apple-app-site-association file on every self-hosted server.
/// - Not a loopback redirect: ASWebAuthenticationSession completes on a URL scheme, and a
///   local HTTP server inside the app would be fragile (backgrounding, ports).
/// - A scheme can be claimed by another app, so the code alone is worthless: the server
///   only redeems it with the PKCE verifier this app generated (S256 challenge sent when
///   the sign-in started). ASWebAuthenticationSession also delivers the callback only to
///   the app that started it.
enum SingleSignOn {
    static let callbackScheme = "streamero"
    static let redirectURI = "streamero://oidc"

    /// 32 random bytes, base64url without padding (43 characters).
    static func makeVerifier() -> String {
        var bytes = [UInt8](repeating: 0, count: 32)
        let status = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        precondition(status == errSecSuccess, "no secure random bytes")
        return base64URL(Data(bytes))
    }

    /// PKCE S256: BASE64URL(SHA-256(verifier)).
    static func challenge(for verifier: String) -> String {
        base64URL(Data(SHA256.hash(data: Data(verifier.utf8))))
    }

    static func base64URL(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    /// The URL to open for a provider's button.
    static func authorizeURL(_ authorizeURL: String, challenge: String) -> URL? {
        guard var parts = URLComponents(string: authorizeURL) else { return nil }
        var items = parts.queryItems ?? []
        items += [
            URLQueryItem(name: "client", value: "app"),
            URLQueryItem(name: "redirectUri", value: redirectURI),
            URLQueryItem(name: "codeChallenge", value: challenge),
        ]
        parts.queryItems = items
        // URLComponents leaves ":" and "/" unescaped in query values; the server decodes both forms,
        // but escape them so the value is unambiguous.
        parts.percentEncodedQuery = parts.percentEncodedQuery?
            .replacingOccurrences(of: ":", with: "%3A")
            .replacingOccurrences(of: "/", with: "%2F")
        return parts.url
    }

    enum Outcome: Equatable {
        case code(String)
        case failed(String)
    }

    /// Reads `streamero://oidc?code=…` or `?error=…` (nil for any other URL).
    static func parseCallback(_ url: URL) -> Outcome? {
        guard url.scheme?.lowercased() == callbackScheme, url.host()?.lowercased() == "oidc",
              let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems else {
            return url.scheme?.lowercased() == callbackScheme && url.host()?.lowercased() == "oidc" ? .failed("internal") : nil
        }
        if let code = items.first(where: { $0.name == "code" })?.value, !code.isEmpty {
            return .code(code)
        }
        return .failed(items.first(where: { $0.name == "error" })?.value ?? "internal")
    }
}
