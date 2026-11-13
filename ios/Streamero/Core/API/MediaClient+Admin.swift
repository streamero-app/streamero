import Foundation

/// Profiles, two-factor sign-in, own devices and the administration pages that were web-only.
extension MediaClient {
    // MARK: Profiles

    func createProfile(_ input: ProfileInput) async throws -> Profile { try await post("profiles", input) }
    func updateProfile(_ id: String, _ input: ProfileInput) async throws -> Profile { try await put("profiles/\(id)", input) }
    func deleteProfile(_ id: String) async throws { try await send("DELETE", "profiles/\(id)") }

    func uploadProfileAvatar(_ id: String, jpeg: Data) async throws {
        try await upload("PUT", "profiles/\(id)/avatar", data: jpeg, contentType: "application/octet-stream")
    }

    func removeProfileAvatar(_ id: String) async throws { try await send("DELETE", "profiles/\(id)/avatar") }

    // MARK: Two-factor sign-in

    func startTOTPSetup() async throws -> TOTPSetup { try await call("POST", "auth/totp/setup") }
    func enableTOTP(code: String) async throws -> RecoveryCodes { try await post("auth/totp/enable", ["code": code]) }
    func disableTOTP(password: String) async throws { try await send("POST", "auth/totp/disable", ["password": password]) }

    // MARK: Notifications

    func notificationSettings() async throws -> NotificationSettings { try await get("settings/notifications") }
    func saveNotificationSettings(_ s: NotificationSettings) async throws -> NotificationSettings {
        try await put("settings/notifications", s)
    }
    func testNotification(_ target: NotificationTarget) async throws {
        try await send("POST", "settings/notifications/test", target)
    }

    // MARK: Backups

    func backups() async throws -> BackupOverview { try await get("backups") }
    func createBackup() async throws -> BackupQueued { try await call("POST", "backups") }
    func saveBackupSettings(_ s: BackupSettings) async throws -> BackupSettings { try await put("backups/settings", s) }
    func deleteBackup(_ name: String) async throws { try await send("DELETE", "backups/\(name)") }
    func restoreBackup(_ name: String, confirm: String) async throws {
        try await send("POST", "backups/\(name)/restore", RestoreBackupRequest(confirm: confirm))
    }

    /// Downloads a backup archive to a temporary file named like the backup (for the share sheet).
    func downloadBackup(_ name: String) async throws -> URL {
        var req = URLRequest(url: url(path: "backups/\(name)/download"))
        req.timeoutInterval = 300
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let (file, response) = try await URLSession.shared.download(for: req)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else {
            try? FileManager.default.removeItem(at: file)
            throw APIError(status: status, code: "http_\(status)")
        }
        let dir = FileManager.default.temporaryDirectory.appending(path: "backups", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        // The name comes from the server's list; keep only its last path component.
        let target = dir.appending(path: (name as NSString).lastPathComponent)
        try? FileManager.default.removeItem(at: target)
        try FileManager.default.moveItem(at: file, to: target)
        return target
    }

    // MARK: Live TV sources

    func liveTvTuners() async throws -> [LiveTvTuner] { try await get("livetv/tuners") }
    func createTuner(_ input: LiveTvTunerInput) async throws -> LiveTvTuner { try await post("livetv/tuners", input) }
    func updateTuner(_ id: String, _ input: LiveTvTunerInput) async throws -> LiveTvTuner { try await put("livetv/tuners/\(id)", input) }
    func testTuner(_ input: LiveTvTunerInput) async throws -> LiveTvTestResult { try await post("livetv/tuners/test", input) }
    func refreshTuner(_ id: String) async throws -> LiveTvTuner { try await call("POST", "livetv/tuners/\(id)/refresh") }
    func deleteTuner(_ id: String) async throws { try await send("DELETE", "livetv/tuners/\(id)") }

    func liveTvGuides() async throws -> [LiveTvGuideSource] { try await get("livetv/guides") }
    func createGuide(_ input: LiveTvGuideSourceInput) async throws -> LiveTvGuideSource { try await post("livetv/guides", input) }
    func updateGuide(_ id: String, _ input: LiveTvGuideSourceInput) async throws -> LiveTvGuideSource { try await put("livetv/guides/\(id)", input) }
    func testGuide(_ input: LiveTvGuideSourceInput) async throws -> LiveTvTestResult { try await post("livetv/guides/test", input) }
    func refreshGuide(_ id: String) async throws { try await send("POST", "livetv/guides/\(id)/refresh") }
    func deleteGuide(_ id: String) async throws { try await send("DELETE", "livetv/guides/\(id)") }

    // MARK: Sign-in settings

    func authSettings() async throws -> AuthSettings { try await get("settings/auth") }
    func saveAuthSettings(_ s: AuthSettingsInput) async throws -> AuthSettings { try await put("settings/auth", s) }
    func oidcProviders() async throws -> [OidcProvider] { try await get("settings/auth/oidc") }
    func createOidcProvider(_ input: OidcProviderInput) async throws -> OidcProvider { try await post("settings/auth/oidc", input) }
    func updateOidcProvider(_ id: String, _ input: OidcProviderInput) async throws -> OidcProvider {
        try await put("settings/auth/oidc/\(id)", input)
    }
    func deleteOidcProvider(_ id: String) async throws { try await send("DELETE", "settings/auth/oidc/\(id)") }
    func testOidc(issuer: String) async throws -> OidcTestResult { try await post("settings/auth/oidc-test", ["issuer": issuer]) }
    func ldapSettings() async throws -> LdapSettings { try await get("settings/auth/ldap") }
    func saveLdapSettings(_ s: LdapSettingsInput) async throws -> LdapSettings { try await put("settings/auth/ldap", s) }
    func testLdap(_ request: LdapTestRequest) async throws -> LdapTestResult { try await post("settings/auth/ldap/test", request) }
}
