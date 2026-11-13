package net.scyde.streamero.api

import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable

// Profiles, two-factor sign-in, invites and the user's own devices (see api/openapi.yaml).

@Serializable data class ProfileInput(
    val name: String, val color: String, val isKid: Boolean, val maxRating: Int? = null,
    val pin: String? = null, val removePin: Boolean? = null,
)
@Serializable data class TotpSetup(val secret: String, val otpauthUri: String, val qrPng: String)
@Serializable data class TotpCode(val code: String)
@Serializable data class RecoveryCodes(val recoveryCodes: List<String>)
@Serializable data class PasswordConfirm(val password: String)
@Serializable data class InviteInfo(val serverName: String, val expiresAt: String)
@Serializable data class AcceptInvite(val username: String, val password: String, val device: DeviceInfo)
@Serializable data class RenameSession(val deviceName: String)

suspend fun Api.createProfile(p: ProfileInput): Profile = post("profiles", p)
suspend fun Api.updateProfile(id: String, p: ProfileInput): Profile = put("profiles/$id", p)
suspend fun Api.deleteProfile(id: String) = send("DELETE", "profiles/$id")

suspend fun Api.totpSetup(): TotpSetup = call("POST", "auth/totp/setup", Api.EMPTY, emptyMap(), TotpSetup.serializer())!!
suspend fun Api.enableTotp(code: String): RecoveryCodes = post("auth/totp/enable", TotpCode(code))
suspend fun Api.disableTotp(password: String) = send("POST", "auth/totp/disable", PasswordConfirm(password))

/** Public: whether an invite token is still valid (and for which server). */
suspend fun Api.inviteInfo(token: String): InviteInfo = get("invite/$token")
/** Public: creates the account from the invite and returns the new session. */
suspend fun Api.acceptInvite(token: String, a: AcceptInvite): AuthResult = post("invite/$token", a)

suspend fun Api.renameSession(id: String, name: String) {
    call("PATCH", "sessions/$id", jsonBody(RenameSession(name)), emptyMap(), null as KSerializer<Unit>?)
}
