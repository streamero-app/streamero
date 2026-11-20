import Foundation
import HTTPTypes
import OpenAPIRuntime
import OpenAPIURLSession
#if canImport(UIKit)
import UIKit
#endif

/// Adds `Authorization: Bearer <token>` to every request.
struct BearerMiddleware: ClientMiddleware {
    let token: String

    func intercept(
        _ request: HTTPRequest, body: HTTPBody?, baseURL: URL, operationID: String,
        next: @Sendable (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)
    ) async throws -> (HTTPResponse, HTTPBody?) {
        var request = request
        request.headerFields[.authorization] = "Bearer \(token)"
        return try await next(request, body, baseURL)
    }
}

/// An API error with the server's stable error code (used for messages).
struct APIError: Error, Equatable {
    let status: Int
    let code: String
    var message: String? = nil

    /// The error for a non-2xx answer. The body may be the usual `{code, message}`, an empty
    /// body, or HTML from a proxy in front of the server; anything unreadable becomes `http_<status>`.
    static func from(status: Int, data: Data) -> APIError {
        let body = try? JSONDecoder().decode(APIErrorBody.self, from: data)
        let code = body.flatMap { $0.code.isEmpty ? nil : $0.code } ?? "http_\(status)"
        return APIError(status: status, code: code, message: body?.message)
    }
}

/// Result of a Quick Connect poll.
enum QuickConnectState: Equatable {
    case pending
    case signedIn(token: String)
}

/// Sign-in operations. Requests use the types generated from api/openapi.yaml. The calls that
/// return a session (password, single sign-on, Quick Connect, invite) read only the token: the
/// generated `User` has strict enums (the interface language), so a value this version does not
/// know would fail a sign-in that worked. The account is loaded afterwards through the tolerant
/// hand-written model (`MediaClient.me`).
struct AuthClient {
    let baseURL: URL
    var token: String?

    private var client: Client {
        let middlewares: [any ClientMiddleware] = token.map { [BearerMiddleware(token: $0)] } ?? []
        return Client(serverURL: ServerURL.apiBase(baseURL), transport: URLSessionTransport(), middlewares: middlewares)
    }

    /// UIDevice is main-actor isolated in Swift 6.
    @MainActor static var device: Components.Schemas.DeviceInfo {
        #if canImport(UIKit)
        let name = UIDevice.current.name
        #else
        let name = "Apple device"
        #endif
        let version = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0"
        return .init(name: name, client: "\(AppIdentity.isManage ? "Streamero Manage" : "Streamero") iOS \(version)")
    }

    /// The part of a sign-in answer the app needs.
    private struct SignedIn: Decodable {
        var token: String?
    }

    /// The token from a sign-in answer: 200 with a token, otherwise the server's error.
    static func sessionToken(status: Int, data: Data) throws -> String {
        guard status == 200 else { throw APIError.from(status: status, data: data) }
        guard let token = try? JSONDecoder().decode(SignedIn.self, from: data).token, !token.isEmpty else {
            throw APIError(status: status, code: "no_token")
        }
        return token
    }

    /// Quick Connect: 202 while the code waits for approval, 200 with the session once approved.
    static func quickConnectState(status: Int, data: Data) throws -> QuickConnectState {
        if status == 202 { return .pending }
        return .signedIn(token: try sessionToken(status: status, data: data))
    }

    private func post<B: Encodable>(_ path: String, _ body: B) async throws -> (Int, Data) {
        var req = URLRequest(url: ServerURL.apiBase(baseURL).appending(path: path))
        req.httpMethod = "POST"
        req.timeoutInterval = 30
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        if let token { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        req.httpBody = try JSONEncoder().encode(body)
        let (data, response) = try await HTTP.data(for: req)
        guard let http = response as? HTTPURLResponse else { throw APIError(status: 0, code: "bad_response") }
        return (http.statusCode, data)
    }

    /// Signs in; throws APIError(code: "totp_required") when a 2FA code is needed.
    func login(username: String, password: String, totpCode: String?) async throws -> String {
        let device = await Self.device
        let body = Components.Schemas.LoginRequest(
            username: username, password: password, totpCode: totpCode, useCookie: false, device: device)
        let (status, data) = try await post("auth/login", body)
        return try Self.sessionToken(status: status, data: data)
    }

    /// Sign-in methods the server offers (single sign-on buttons, whether passwords work).
    func providers() async throws -> Components.Schemas.AuthProviders {
        switch try await client.getAuthProviders() {
        case .ok(let ok):
            return try ok.body.json
        case .default(let status, let response):
            throw APIError(status: status, code: (try? response.body.json.code) ?? "http_\(status)")
        }
    }

    /// Redeems the one-time code from the `streamero://oidc` redirect with the PKCE verifier.
    func exchangeOIDC(code: String, verifier: String) async throws -> String {
        let device = await Self.device
        let body = Components.Schemas.OidcExchangeRequest(code: code, codeVerifier: verifier, device: device)
        let (status, data) = try await post("auth/oidc/exchange", body)
        return try Self.sessionToken(status: status, data: data)
    }

    func initiateQuickConnect() async throws -> Components.Schemas.QuickConnectStart {
        let device = await Self.device
        switch try await client.initiateQuickConnect(body: .json(device)) {
        case .ok(let ok):
            return try ok.body.json
        case .default(let status, let response):
            throw APIError(status: status, code: (try? response.body.json.code) ?? "http_\(status)")
        }
    }

    func pollQuickConnect(secret: String) async throws -> QuickConnectState {
        let (status, data) = try await post("quickconnect/poll",
                                            Components.Schemas.QuickConnectPollRequest(secret: secret, useCookie: false))
        return try Self.quickConnectState(status: status, data: data)
    }

    /// Checks an invite link; the server answers with its name when the invite is still valid.
    func invite(token: String) async throws -> Components.Schemas.InviteInfo {
        switch try await client.getInvite(path: .init(token: token)) {
        case .ok(let ok):
            return try ok.body.json
        case .default(let status, let response):
            throw APIError(status: status, code: (try? response.body.json.code) ?? "http_\(status)")
        }
    }

    /// Creates an account from an invite and signs it in (returns the session token).
    func acceptInvite(token: String, username: String, password: String) async throws -> String {
        let device = await Self.device
        let body = Components.Schemas.AcceptInviteRequest(username: username, password: password, useCookie: false, device: device)
        // Invite tokens are URL-safe (no "/"), so the token is exactly one path segment.
        guard InviteLink.isToken(token) else { throw APIError(status: 404, code: "not_found") }
        let (status, data) = try await post("invite/\(token)", body)
        return try Self.sessionToken(status: status, data: data)
    }

    /// Ends the session on the server. Best effort: the app forgets the token either way.
    func logout() async {
        _ = try? await client.logout()
    }
}
