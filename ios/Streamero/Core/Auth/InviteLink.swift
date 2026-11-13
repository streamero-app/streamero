import Foundation

/// Invite links: `https://<server>/invite/<token>` (what the web and the admin pages hand out),
/// `streamero://invite?server=<address>&token=<token>` (opens the app), or the bare token.
enum InviteLink {
    struct Parsed: Equatable {
        /// Nil when only a token was given: the server comes from the add-server field.
        var server: URL?
        var token: String
    }

    static let scheme = "streamero"

    /// Reads whatever the user pasted; nil when it is neither a link nor a plausible token.
    static func parse(_ input: String) -> Parsed? {
        let text = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        if text.lowercased().hasPrefix("\(scheme)://") {
            return URL(string: text).flatMap(parse(url:))
        }
        if text.contains("/") {
            let withScheme = text.lowercased().hasPrefix("http://") || text.lowercased().hasPrefix("https://") ? text : "https://" + text
            guard let comps = URLComponents(string: withScheme), let host = comps.host, !host.isEmpty else { return nil }
            let path = comps.path
            guard let range = path.range(of: "/invite/", options: .backwards) else { return nil }
            let token = String(path[range.upperBound...]).trimmingCharacters(in: CharacterSet(charactersIn: "/"))
            guard isToken(token) else { return nil }
            var base = comps
            base.path = String(path[..<range.lowerBound])
            base.query = nil
            base.fragment = nil
            guard let baseString = base.string, let server = ServerURL.normalize(baseString) else { return nil }
            return Parsed(server: server, token: token)
        }
        return isToken(text) ? Parsed(server: nil, token: text) : nil
    }

    /// `streamero://invite?server=…&token=…` (nil for other app links such as `streamero://oidc`).
    static func parse(url: URL) -> Parsed? {
        guard url.scheme?.lowercased() == scheme, url.host()?.lowercased() == "invite",
              let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems,
              let token = items.first(where: { $0.name == "token" })?.value, isToken(token) else { return nil }
        let server = items.first(where: { $0.name == "server" })?.value.flatMap(ServerURL.normalize)
        return Parsed(server: server, token: token)
    }

    /// Tokens are URL-safe and at most 128 characters (api/openapi.yaml).
    static func isToken(_ s: String) -> Bool {
        guard (8...128).contains(s.count) else { return false }
        let allowed = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~")
        return s.unicodeScalars.allSatisfy { allowed.contains($0) }
    }
}

/// The sign-up form behind an invite (same rules as the web page and the API schema).
struct InviteSignUpForm: Equatable {
    var username = ""
    var password = ""
    var confirm = ""

    enum Problem: Equatable {
        case username
        case passwordLength
        case mismatch
    }

    var problem: Problem? {
        let name = username.trimmingCharacters(in: .whitespaces)
        if name.isEmpty || name.count > 64 { return .username }
        if password.count < 8 || password.count > 256 { return .passwordLength }
        if password != confirm { return .mismatch }
        return nil
    }
}
