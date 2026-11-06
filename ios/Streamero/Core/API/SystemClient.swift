import Foundation
import OpenAPIRuntime
import OpenAPIURLSession

enum ServerCheckError: Error, Equatable {
    case starting
    case notStreamero
    case unexpected(Int)
}

/// Verifies a server by calling the generated GET /system/info operation.
struct SystemClient {
    let baseURL: URL

    func info() async throws -> Components.Schemas.SystemInfo {
        let client = Client(serverURL: ServerURL.apiBase(baseURL), transport: URLSessionTransport())
        switch try await client.getSystemInfo() {
        case .ok(let ok):
            let info = try ok.body.json
            guard info.product == "Streamero" else { throw ServerCheckError.notStreamero }
            return info
        case .serviceUnavailable:
            throw ServerCheckError.starting
        case .default(let statusCode, _):
            throw ServerCheckError.unexpected(statusCode)
        }
    }
}
