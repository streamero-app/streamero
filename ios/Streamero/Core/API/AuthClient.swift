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
}

/// Result of a Quick Connect poll.
enum QuickConnectState: Equatable {
    case pending
    case signedIn(token: String, user: Components.Schemas.User)
}

/// Sign-in operations, all through the client generated from api/openapi.yaml.
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
        return .init(name: name, client: "Streamero iOS \(version)")
    }

    /// Signs in; throws APIError(code: "totp_required") when a 2FA code is needed.
    func login(username: String, password: String, totpCode: String?) async throws -> (String, Components.Schemas.User) {
        let device = await Self.device
        let body = Components.Schemas.LoginRequest(
            username: username, password: password, totpCode: totpCode, useCookie: false, device: device)
        switch try await client.login(body: .json(body)) {
        case .ok(let ok):
            let result = try ok.body.json
            guard let token = result.token else { throw APIError(status: 200, code: "no_token") }
            return (token, result.user)
        case .default(let status, let response):
            throw APIError(status: status, code: (try? response.body.json.code) ?? "http_\(status)")
        }
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
    func exchangeOIDC(code: String, verifier: String) async throws -> (String, Components.Schemas.User) {
        let device = await Self.device
        let body = Components.Schemas.OidcExchangeRequest(code: code, codeVerifier: verifier, device: device)
        switch try await client.oidcExchange(body: .json(body)) {
        case .ok(let ok):
            let result = try ok.body.json
            guard let token = result.token else { throw APIError(status: 200, code: "no_token") }
            return (token, result.user)
        case .default(let status, let response):
            throw APIError(status: status, code: (try? response.body.json.code) ?? "http_\(status)")
        }
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
        switch try await client.pollQuickConnect(body: .json(.init(secret: secret, useCookie: false))) {
        case .ok(let ok):
            let result = try ok.body.json
            guard let token = result.token else { throw APIError(status: 200, code: "no_token") }
            return .signedIn(token: token, user: result.user)
        case .accepted:
            return .pending
        case .default(let status, let response):
            throw APIError(status: status, code: (try? response.body.json.code) ?? "http_\(status)")
        }
    }

    func me() async throws -> Components.Schemas.User {
        switch try await client.getMe() {
        case .ok(let ok):
            return try ok.body.json
        case .default(let status, let response):
            throw APIError(status: status, code: (try? response.body.json.code) ?? "http_\(status)")
        }
    }

    func logout() async {
        _ = try? await client.logout()
    }
}
