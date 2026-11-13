import Foundation

// Hand-written models for profiles, two-factor sign-in, notifications, backups, Live TV sources
// and sign-in settings, mirroring api/openapi.yaml (same rules as Models.swift: required fields
// are non-optional, optional fields are omitted when nil). Secrets are write-only: the server
// answers with a "configured" flag (`hasToken`, `clientSecretSet`, `bindPasswordSet`) and an
// empty or missing secret in a request keeps the stored one.

// MARK: Profiles

struct ProfileInput: Codable, Sendable, Equatable {
    var name: String
    var color: String
    var isKid: Bool
    var maxRating: Int?
    var pin: String?
    var removePin: Bool?
}

// MARK: Two-factor sign-in

struct TOTPSetup: Codable, Sendable, Equatable {
    var secret: String
    var otpauthUri: String
    var qrPng: String
}

struct RecoveryCodes: Codable, Sendable, Equatable {
    var recoveryCodes: [String]
}

// MARK: Notifications

struct NotificationTarget: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var name: String
    /// generic, discord, ntfy or gotify.
    var kind: String
    var url: String
    /// Write-only; nil keeps the stored token.
    var token: String?
    var hasToken: Bool?
    var events: [String]
    var enabled: Bool

    static let kinds = ["discord", "ntfy", "gotify", "generic"]
    static let eventKinds = ["item.added", "playback.started", "login.succeeded", "login.failed", "scan.failed"]
    static let maxTargets = 20

    static func new() -> NotificationTarget {
        NotificationTarget(id: UUID().uuidString.lowercased(), name: "", kind: "discord", url: "",
                           token: nil, hasToken: nil, events: ["item.added"], enabled: true)
    }

    /// Whether the form can be saved: a name and an http(s) address.
    var isValid: Bool {
        let trimmed = name.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, trimmed.count <= 80, url.count <= 2000,
              let parsed = URL(string: url.trimmingCharacters(in: .whitespaces)),
              let scheme = parsed.scheme?.lowercased(), scheme == "http" || scheme == "https",
              parsed.host() != nil else { return false }
        return true
    }

    /// The body to send: an empty token means "keep the stored one".
    var forSaving: NotificationTarget {
        var copy = self
        copy.name = name.trimmingCharacters(in: .whitespaces)
        copy.url = url.trimmingCharacters(in: .whitespaces)
        if copy.token?.isEmpty == true { copy.token = nil }
        if copy.kind == "discord" { copy.token = nil }
        return copy
    }
}

struct NotificationSettings: Codable, Sendable, Equatable {
    var targets: [NotificationTarget]
}

// MARK: Backups

struct BackupSettings: Codable, Sendable, Equatable {
    var enabled: Bool
    var keepLast: Int
    var includeImages: Bool
}

struct Backup: Codable, Sendable, Equatable, Identifiable {
    var name: String
    var sizeBytes: Int64
    var createdAt: String
    /// scheduled, manual, pre_restore or uploaded.
    var kind: String
    var serverVersion: String
    var schemaVersion: Int64
    var includesImages: Bool
    var scanRunning: Bool
    var restorable: Bool
    var problem: String?
    var skippedTables: [String]?

    var id: String { name }

    /// The restore is only sent once the backup's name was typed exactly (as on the web).
    func confirms(_ typed: String) -> Bool { typed == name }
}

struct RestoreStatus: Codable, Sendable, Equatable {
    var backup: String
    /// running, succeeded or failed.
    var state: String
    var startedAt: String
    var finishedAt: String?
    var error: String?
    var safetyBackup: String?
    var warnings: [String]?
}

struct BackupOverview: Codable, Sendable, Equatable {
    var dir: String
    var backups: [Backup]
    var settings: BackupSettings
    var scanRunning: Bool
    var running: Bool
    var schemaVersion: Int64
    var lastRestore: RestoreStatus?

    /// Poll while a backup is written or a restore runs.
    var isBusy: Bool { running || lastRestore?.state == "running" }
}

struct BackupQueued: Codable, Sendable, Equatable {
    var scanRunning: Bool
}

struct RestoreBackupRequest: Codable, Sendable, Equatable {
    var confirm: String
}

// MARK: Live TV sources

struct LiveTvTuner: Codable, Sendable, Equatable, Identifiable {
    var id: String
    /// m3u or hdhomerun.
    var kind: String
    var name: String
    var url: String?
    var filePath: String?
    var tunerCount: Int
    var userAgent: String?
    var enabled: Bool
    var channelCount: Int
    var deviceId: String?
    var model: String?
    var lastRefreshAt: String?
    var error: String?
}

struct LiveTvTunerInput: Codable, Sendable, Equatable {
    var kind: String
    var name: String
    var url: String?
    var filePath: String?
    var tunerCount: Int?
    var userAgent: String?
    var enabled: Bool?
}

struct LiveTvTestResult: Codable, Sendable, Equatable {
    var ok: Bool
    var message: String
    var channelCount: Int?
    var programCount: Int?
    var tunerCount: Int?
    var model: String?
}

struct LiveTvGuideSource: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var name: String
    var url: String?
    var filePath: String?
    var timezone: String?
    var refreshHours: Int
    var enabled: Bool
    var channelCount: Int
    var programCount: Int
    var lastRefreshAt: String?
    var lastChangedAt: String?
    var lastResult: String?
    var error: String?
}

struct LiveTvGuideSourceInput: Codable, Sendable, Equatable {
    var name: String
    var url: String?
    var filePath: String?
    var timezone: String?
    var refreshHours: Int?
    var enabled: Bool?
}

/// The add/edit form for a tuner source (same rules as the web form).
struct TunerForm: Equatable {
    var kind = "m3u"
    var name = ""
    var fromFile = false
    var url = ""
    var filePath = ""
    /// Empty = unlimited (m3u) or what the device reports (HDHomeRun).
    var count = ""
    var userAgent = ""
    var enabled = true

    init() {}

    init(_ tuner: LiveTvTuner) {
        kind = tuner.kind
        name = tuner.name
        fromFile = tuner.filePath?.isEmpty == false
        url = tuner.url ?? ""
        filePath = tuner.filePath ?? ""
        count = String(tuner.tunerCount)
        userAgent = tuner.userAgent ?? ""
        enabled = tuner.enabled
    }

    private var usesFile: Bool { kind == "m3u" && fromFile }

    var isValid: Bool {
        let countOK = count.trimmingCharacters(in: .whitespaces).isEmpty || (Int(count.trimmingCharacters(in: .whitespaces)).map { (0...64).contains($0) } ?? false)
        return !name.trimmingCharacters(in: .whitespaces).isEmpty && countOK
            && !(usesFile ? filePath : url).trimmingCharacters(in: .whitespaces).isEmpty
    }

    var input: LiveTvTunerInput {
        func clean(_ s: String) -> String? {
            let t = s.trimmingCharacters(in: .whitespaces)
            return t.isEmpty ? nil : t
        }
        return LiveTvTunerInput(
            kind: kind, name: name.trimmingCharacters(in: .whitespaces),
            url: usesFile ? nil : clean(url),
            filePath: usesFile ? clean(filePath) : nil,
            tunerCount: Int(count.trimmingCharacters(in: .whitespaces)),
            userAgent: kind == "m3u" ? clean(userAgent) : nil,
            enabled: enabled)
    }
}

/// The add/edit form for an XMLTV guide source.
struct GuideForm: Equatable {
    var name = ""
    var fromFile = false
    var url = ""
    var filePath = ""
    var timezone = ""
    var refreshHours = 12
    var enabled = true

    init() {}

    init(_ guide: LiveTvGuideSource) {
        name = guide.name
        fromFile = guide.filePath?.isEmpty == false
        url = guide.url ?? ""
        filePath = guide.filePath ?? ""
        timezone = guide.timezone ?? ""
        refreshHours = guide.refreshHours
        enabled = guide.enabled
    }

    var isValid: Bool {
        !name.trimmingCharacters(in: .whitespaces).isEmpty && (1...168).contains(refreshHours)
            && !(fromFile ? filePath : url).trimmingCharacters(in: .whitespaces).isEmpty
    }

    var input: LiveTvGuideSourceInput {
        func clean(_ s: String) -> String? {
            let t = s.trimmingCharacters(in: .whitespaces)
            return t.isEmpty ? nil : t
        }
        return LiveTvGuideSourceInput(
            name: name.trimmingCharacters(in: .whitespaces),
            url: fromFile ? nil : clean(url), filePath: fromFile ? clean(filePath) : nil,
            timezone: clean(timezone), refreshHours: refreshHours, enabled: enabled)
    }
}

// MARK: Sign-in settings

struct AuthSettings: Codable, Sendable, Equatable {
    var publicUrl: String
    var disablePasswordLogin: Bool
    var recoveryActive: Bool
}

struct AuthSettingsInput: Codable, Sendable, Equatable {
    var publicUrl: String
    var disablePasswordLogin: Bool
}

struct OidcProvider: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var label: String
    var issuer: String
    var clientId: String
    var clientSecretSet: Bool
    var scopes: [String]
    var usernameClaim: String
    var emailClaim: String
    var nameClaim: String
    var groupsClaim: String
    var adminGroup: String
    var allowedGroup: String
    var autoCreate: Bool
    var linkByEmail: Bool
    var trustEmail: Bool
    var enabled: Bool
    var redirectUri: String
}

struct OidcProviderInput: Codable, Sendable, Equatable {
    var id: String
    var label: String
    var issuer: String
    var clientId: String
    var clientSecret: String?
    var removeClientSecret: Bool?
    var scopes: [String]?
    var usernameClaim: String?
    var emailClaim: String?
    var nameClaim: String?
    var groupsClaim: String?
    var adminGroup: String?
    var allowedGroup: String?
    var autoCreate: Bool?
    var linkByEmail: Bool?
    var trustEmail: Bool?
    var enabled: Bool?
}

struct OidcTestResult: Codable, Sendable, Equatable {
    var issuer: String
    var authorizationEndpoint: String
    var tokenEndpoint: String
    var userinfoEndpoint: String?
    var signingKeys: Int
    var pkceS256: Bool
    var scopesSupported: [String]?
}

/// The add/edit form for an OpenID Connect provider (defaults as on the web).
struct OidcForm: Equatable {
    var id = ""
    var label = ""
    var issuer = ""
    var clientId = ""
    var clientSecret = ""
    var scopes = "openid profile email"
    var usernameClaim = "preferred_username"
    var emailClaim = "email"
    var nameClaim = "name"
    var groupsClaim = "groups"
    var adminGroup = ""
    var allowedGroup = ""
    var autoCreate = false
    var linkByEmail = true
    var trustEmail = false
    var enabled = true

    init() {}

    init(_ p: OidcProvider) {
        id = p.id
        label = p.label
        issuer = p.issuer
        clientId = p.clientId
        scopes = p.scopes.joined(separator: " ")
        usernameClaim = p.usernameClaim
        emailClaim = p.emailClaim
        nameClaim = p.nameClaim
        groupsClaim = p.groupsClaim
        adminGroup = p.adminGroup
        allowedGroup = p.allowedGroup
        autoCreate = p.autoCreate
        linkByEmail = p.linkByEmail
        trustEmail = p.trustEmail
        enabled = p.enabled
    }

    /// Starting points for well-known providers; the admin fills in their own address.
    enum Preset: String, CaseIterable, Identifiable {
        case authentik, keycloak, authelia, entra, google
        var id: String { rawValue }
        var label: String {
            switch self {
            case .authentik: "Authentik"
            case .keycloak: "Keycloak"
            case .authelia: "Authelia"
            case .entra: "Microsoft"
            case .google: "Google"
            }
        }
    }

    mutating func apply(_ preset: Preset) {
        id = preset.rawValue
        label = preset.label
        switch preset {
        case .authentik: issuer = "https://auth.example.com/application/o/streamero/"; groupsClaim = "groups"
        case .keycloak: issuer = "https://auth.example.com/realms/home"; groupsClaim = "groups"
        case .authelia:
            issuer = "https://auth.example.com"
            groupsClaim = "groups"
            scopes = "openid profile email groups"
        case .entra: issuer = "https://login.microsoftonline.com/<tenant-id>/v2.0"; groupsClaim = "groups"
        case .google: issuer = "https://accounts.google.com"; usernameClaim = "email"
        }
    }

    static func isValidID(_ id: String) -> Bool {
        id.range(of: "^[a-z0-9][a-z0-9-]{0,31}$", options: .regularExpression) != nil
    }

    var isValid: Bool {
        Self.isValidID(id) && !label.trimmingCharacters(in: .whitespaces).isEmpty && label.count <= 60
            && !issuer.trimmingCharacters(in: .whitespaces).isEmpty && !clientId.trimmingCharacters(in: .whitespaces).isEmpty
    }

    var input: OidcProviderInput {
        OidcProviderInput(
            id: id, label: label.trimmingCharacters(in: .whitespaces), issuer: issuer.trimmingCharacters(in: .whitespaces),
            clientId: clientId.trimmingCharacters(in: .whitespaces),
            clientSecret: clientSecret.isEmpty ? nil : clientSecret, removeClientSecret: nil,
            scopes: scopes.split(whereSeparator: \.isWhitespace).map(String.init),
            usernameClaim: usernameClaim, emailClaim: emailClaim, nameClaim: nameClaim, groupsClaim: groupsClaim,
            adminGroup: adminGroup, allowedGroup: allowedGroup, autoCreate: autoCreate,
            linkByEmail: linkByEmail, trustEmail: trustEmail, enabled: enabled)
    }
}

struct LdapSettings: Codable, Sendable, Equatable {
    var enabled: Bool
    var url: String
    var startTls: Bool
    var skipVerify: Bool
    var caCert: String
    var bindDn: String
    var bindPasswordSet: Bool
    var baseDn: String
    var userFilter: String
    var usernameAttribute: String
    var emailAttribute: String
    var nameAttribute: String
    var groupAttribute: String
    var groupFilter: String
    var adminGroup: String
    var allowedGroup: String
}

struct LdapSettingsInput: Codable, Sendable, Equatable {
    var enabled: Bool
    var url: String
    var startTls: Bool
    var skipVerify: Bool
    var caCert: String?
    var bindDn: String?
    var bindPassword: String?
    var removeBindPassword: Bool?
    var baseDn: String
    var userFilter: String
    var usernameAttribute: String?
    var emailAttribute: String?
    var nameAttribute: String?
    var groupAttribute: String?
    var groupFilter: String?
    var adminGroup: String?
    var allowedGroup: String?

    /// The stored settings as an editable body; the bind password stays empty (kept).
    init(_ s: LdapSettings) {
        enabled = s.enabled
        url = s.url
        startTls = s.startTls
        skipVerify = s.skipVerify
        caCert = s.caCert
        bindDn = s.bindDn
        bindPassword = nil
        baseDn = s.baseDn
        userFilter = s.userFilter
        usernameAttribute = s.usernameAttribute
        emailAttribute = s.emailAttribute
        nameAttribute = s.nameAttribute
        groupAttribute = s.groupAttribute
        groupFilter = s.groupFilter
        adminGroup = s.adminGroup
        allowedGroup = s.allowedGroup
    }

    /// Without StartTLS or ldaps:// the directory sees passwords in clear text.
    var isPlainText: Bool { url.lowercased().hasPrefix("ldap://") && !startTls }
}

struct LdapTestRequest: Codable, Sendable, Equatable {
    var settings: LdapSettingsInput
    var username: String?
}

struct LdapTestResult: Codable, Sendable, Equatable {
    var ok: Bool
    var message: String
    var userDn: String?
    var username: String?
    var email: String?
    var displayName: String?
    var groups: [String]?
    var isAdmin: Bool?
    var allowed: Bool?
}
