import Foundation
import Security

/// Stores session tokens per server. Tokens never go to UserDefaults.
protocol TokenStore: Sendable {
    func token(for serverID: String) -> String?
    func setToken(_ token: String?, for serverID: String)
}

/// Keychain-backed token store (generic password items, this device only).
/// Works without extra entitlements, so it survives Feather re-signing (PLAN.md §6).
struct KeychainTokenStore: TokenStore {
    private let service = "net.scyde.streamero.session"

    func token(for serverID: String) -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: serverID,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
              let data = item as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    func setToken(_ token: String?, for serverID: String) {
        let base: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: serverID,
        ]
        SecItemDelete(base as CFDictionary)
        guard let token, let data = token.data(using: .utf8) else { return }
        var add = base
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        SecItemAdd(add as CFDictionary, nil)
    }
}

/// In-memory store for tests and previews.
final class MemoryTokenStore: TokenStore, @unchecked Sendable {
    private var tokens: [String: String] = [:]
    private let lock = NSLock()

    func token(for serverID: String) -> String? {
        lock.withLock { tokens[serverID] }
    }

    func setToken(_ token: String?, for serverID: String) {
        lock.withLock { tokens[serverID] = token }
    }
}
