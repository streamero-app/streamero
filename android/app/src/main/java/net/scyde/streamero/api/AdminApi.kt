package net.scyde.streamero.api

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import okhttp3.Request
import java.io.OutputStream

// Administration pages that used to exist only on the web: notifications, backups,
// Live TV sources and sign-in (OIDC / LDAP). Secrets are write-only: the server only says
// whether one is stored (hasToken, clientSecretSet, bindPasswordSet).

@Serializable data class NotificationTarget(
    val id: String, val name: String = "", val kind: String = "discord", val url: String = "",
    val token: String? = null, val hasToken: Boolean = false, val events: List<String> = emptyList(), val enabled: Boolean = true,
)
@Serializable data class NotificationSettings(val targets: List<NotificationTarget> = emptyList())

@Serializable data class BackupSettings(val enabled: Boolean = false, val keepLast: Int = 7, val includeImages: Boolean = false)
@Serializable data class Backup(
    val name: String, val sizeBytes: Long = 0, val createdAt: String = "", val kind: String = "manual",
    val serverVersion: String = "", val schemaVersion: Long = 0, val includesImages: Boolean = false,
    val scanRunning: Boolean = false, val restorable: Boolean = true, val problem: String? = null, val skippedTables: List<String> = emptyList(),
)
@Serializable data class RestoreStatus(
    val backup: String, val state: String, val startedAt: String = "", val finishedAt: String? = null,
    val error: String? = null, val safetyBackup: String? = null, val warnings: List<String> = emptyList(),
)
@Serializable data class BackupOverview(
    val dir: String = "", val backups: List<Backup> = emptyList(), val settings: BackupSettings = BackupSettings(),
    val scanRunning: Boolean = false, val running: Boolean = false, val schemaVersion: Long = 0, val lastRestore: RestoreStatus? = null,
)
@Serializable data class BackupQueued(val scanRunning: Boolean = false)
@Serializable data class RestoreRequest(val confirm: String)

@Serializable data class LiveTvTuner(
    val id: String, val kind: String, val name: String, val url: String? = null, val filePath: String? = null,
    val tunerCount: Int = 0, val userAgent: String? = null, val enabled: Boolean = true, val channelCount: Int = 0,
    val deviceId: String? = null, val model: String? = null, val lastRefreshAt: String? = null, val error: String? = null,
)
@Serializable data class LiveTvTunerInput(
    val kind: String, val name: String, val url: String? = null, val filePath: String? = null,
    val tunerCount: Int? = null, val userAgent: String? = null, val enabled: Boolean = true,
)
@Serializable data class LiveTvDiscoveredTuner(val deviceId: String, val name: String, val model: String? = null, val url: String, val tunerCount: Int = 0, val added: Boolean = false)
@Serializable data class LiveTvTestResult(val ok: Boolean, val message: String = "", val channelCount: Int? = null, val programCount: Int? = null, val tunerCount: Int? = null, val model: String? = null)
@Serializable data class LiveTvGuideSource(
    val id: String, val name: String, val url: String? = null, val filePath: String? = null, val timezone: String? = null,
    val refreshHours: Int = 12, val enabled: Boolean = true, val channelCount: Int = 0, val programCount: Int = 0,
    val lastRefreshAt: String? = null, val lastChangedAt: String? = null, val lastResult: String? = null, val error: String? = null,
)
@Serializable data class LiveTvGuideSourceInput(
    val name: String, val url: String? = null, val filePath: String? = null, val timezone: String? = null,
    val refreshHours: Int = 12, val enabled: Boolean = true,
)

@Serializable data class AuthSettings(val publicUrl: String = "", val disablePasswordLogin: Boolean = false, val recoveryActive: Boolean = false)
@Serializable data class AuthSettingsInput(val publicUrl: String, val disablePasswordLogin: Boolean)
@Serializable data class OidcProvider(
    val id: String, val label: String, val issuer: String, val clientId: String, val clientSecretSet: Boolean = false,
    val scopes: List<String> = emptyList(), val usernameClaim: String = "", val emailClaim: String = "", val nameClaim: String = "",
    val groupsClaim: String = "", val adminGroup: String = "", val allowedGroup: String = "", val autoCreate: Boolean = false,
    val linkByEmail: Boolean = true, val trustEmail: Boolean = false, val enabled: Boolean = true, val redirectUri: String = "",
)
@Serializable data class OidcProviderInput(
    val id: String = "", val label: String = "", val issuer: String = "", val clientId: String = "", val clientSecret: String? = null,
    val scopes: List<String> = listOf("openid", "profile", "email"), val usernameClaim: String = "preferred_username",
    val emailClaim: String = "email", val nameClaim: String = "name", val groupsClaim: String = "groups",
    val adminGroup: String = "", val allowedGroup: String = "", val autoCreate: Boolean = false, val linkByEmail: Boolean = true,
    val trustEmail: Boolean = false, val enabled: Boolean = true,
)
@Serializable data class OidcTestRequest(val issuer: String)
@Serializable data class OidcTestResult(val issuer: String = "", val authorizationEndpoint: String = "", val tokenEndpoint: String = "", val signingKeys: Int = 0, val pkceS256: Boolean = false)
@Serializable data class LdapSettings(
    val enabled: Boolean = false, val url: String = "", val startTls: Boolean = true, val skipVerify: Boolean = false, val caCert: String = "",
    val bindDn: String = "", val bindPasswordSet: Boolean = false, val baseDn: String = "", val userFilter: String = "",
    val usernameAttribute: String = "", val emailAttribute: String = "", val nameAttribute: String = "", val groupAttribute: String = "",
    val groupFilter: String = "", val adminGroup: String = "", val allowedGroup: String = "",
)
@Serializable data class LdapSettingsInput(
    val enabled: Boolean = false, val url: String = "", val startTls: Boolean = true, val skipVerify: Boolean = false, val caCert: String = "",
    val bindDn: String = "", val bindPassword: String? = null, val baseDn: String = "", val userFilter: String = "",
    val usernameAttribute: String = "", val emailAttribute: String = "", val nameAttribute: String = "", val groupAttribute: String = "",
    val groupFilter: String = "", val adminGroup: String = "", val allowedGroup: String = "",
)
@Serializable data class LdapTestRequest(val settings: LdapSettingsInput, val username: String? = null)
@Serializable data class LdapTestResult(
    val ok: Boolean, val message: String = "", val userDn: String? = null, val username: String? = null, val email: String? = null,
    val displayName: String? = null, val groups: List<String> = emptyList(), val isAdmin: Boolean = false, val allowed: Boolean = false,
)

/** The stored LDAP settings as a form; the bind password stays empty (empty keeps the stored one). */
fun LdapSettings.toInput() = LdapSettingsInput(enabled, url, startTls, skipVerify, caCert, bindDn, null, baseDn, userFilter,
    usernameAttribute, emailAttribute, nameAttribute, groupAttribute, groupFilter, adminGroup, allowedGroup)

/** A provider as a form; the secret stays empty, which keeps the stored one. */
fun OidcProvider.toInput() = OidcProviderInput(id, label, issuer, clientId, null, scopes, usernameClaim, emailClaim, nameClaim,
    groupsClaim, adminGroup, allowedGroup, autoCreate, linkByEmail, trustEmail, enabled)

// Notifications
suspend fun Api.notificationSettings(): NotificationSettings = get("settings/notifications")
suspend fun Api.saveNotificationSettings(s: NotificationSettings): NotificationSettings = put("settings/notifications", s)
suspend fun Api.testNotification(t: NotificationTarget) = send("POST", "settings/notifications/test", t)

// Backups
suspend fun Api.backups(): BackupOverview = get("backups")
suspend fun Api.createBackup(): BackupQueued = call("POST", "backups", Api.EMPTY, emptyMap(), BackupQueued.serializer()) ?: BackupQueued()
suspend fun Api.saveBackupSettings(s: BackupSettings): BackupSettings = put("backups/settings", s)
suspend fun Api.deleteBackup(name: String) = send("DELETE", "backups/$name")
suspend fun Api.restoreBackup(name: String, confirm: String) = send("POST", "backups/$name/restore", RestoreRequest(confirm))

/** Streams a backup archive into [out] (a file the user picked); returns the bytes written. */
suspend fun Api.downloadBackup(name: String, out: OutputStream): Long = withContext(Dispatchers.IO) {
    val req = Request.Builder().url(url("api/v1/backups/$name/download")).get().build()
    http.newCall(req).execute().use { resp ->
        if (!resp.isSuccessful) throw ApiException(resp.code, "generic", "HTTP ${resp.code}")
        resp.body!!.byteStream().use { it.copyTo(out) }
    }
}

// Live TV sources
suspend fun Api.tuners(): List<LiveTvTuner> = get("livetv/tuners")
suspend fun Api.createTuner(t: LiveTvTunerInput): LiveTvTuner = post("livetv/tuners", t)
suspend fun Api.updateTuner(id: String, t: LiveTvTunerInput): LiveTvTuner = put("livetv/tuners/$id", t)
suspend fun Api.deleteTuner(id: String) = send("DELETE", "livetv/tuners/$id")
suspend fun Api.refreshTuner(id: String): LiveTvTuner = call("POST", "livetv/tuners/$id/refresh", Api.EMPTY, emptyMap(), LiveTvTuner.serializer())!!
suspend fun Api.testTuner(t: LiveTvTunerInput): LiveTvTestResult = post("livetv/tuners/test", t)
suspend fun Api.discoverTuners(): List<LiveTvDiscoveredTuner> = get("livetv/tuners/discover")
suspend fun Api.guideSources(): List<LiveTvGuideSource> = get("livetv/guides")
suspend fun Api.createGuideSource(g: LiveTvGuideSourceInput): LiveTvGuideSource = post("livetv/guides", g)
suspend fun Api.updateGuideSource(id: String, g: LiveTvGuideSourceInput): LiveTvGuideSource = put("livetv/guides/$id", g)
suspend fun Api.deleteGuideSource(id: String) = send("DELETE", "livetv/guides/$id")
suspend fun Api.refreshGuideSource(id: String) = send("POST", "livetv/guides/$id/refresh")
suspend fun Api.testGuideSource(g: LiveTvGuideSourceInput): LiveTvTestResult = post("livetv/guides/test", g)

// Sign-in (single sign-on and LDAP)
suspend fun Api.authSettings(): AuthSettings = get("settings/auth")
suspend fun Api.saveAuthSettings(s: AuthSettingsInput): AuthSettings = put("settings/auth", s)
suspend fun Api.oidcProviders(): List<OidcProvider> = get("settings/auth/oidc")
suspend fun Api.createOidcProvider(p: OidcProviderInput): OidcProvider = post("settings/auth/oidc", p)
suspend fun Api.updateOidcProvider(id: String, p: OidcProviderInput): OidcProvider = put("settings/auth/oidc/$id", p)
suspend fun Api.deleteOidcProvider(id: String) = send("DELETE", "settings/auth/oidc/$id")
suspend fun Api.testOidcProvider(issuer: String): OidcTestResult = post("settings/auth/oidc-test", OidcTestRequest(issuer))
suspend fun Api.ldapSettings(): LdapSettings = get("settings/auth/ldap")
suspend fun Api.saveLdapSettings(s: LdapSettingsInput): LdapSettings = put("settings/auth/ldap", s)
suspend fun Api.testLdapSettings(r: LdapTestRequest): LdapTestResult = post("settings/auth/ldap/test", r)
