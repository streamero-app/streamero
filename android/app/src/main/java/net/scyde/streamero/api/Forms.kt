package net.scyde.streamero.api

import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import java.net.URI
import java.net.URLDecoder

/**
 * Pure parsing and validation for the account and administration forms, kept out of the
 * Compose code so it can be unit tested on the JVM (no android.net.Uri here).
 */

/** An invite: the server it belongs to (null when only the token was pasted) and the token. */
data class InviteLink(val server: String?, val token: String)

private val TOKEN = Regex("^[A-Za-z0-9_-]{16,128}$")

/**
 * Reads what the user pasted: a web invite link (`https://media.example.com/invite/<token>`,
 * also behind a path prefix or without the scheme), the app link
 * (`streamero://invite?server=…&token=…`) or just the token. Null when it is none of these.
 */
fun parseInviteLink(input: String): InviteLink? {
    val s = input.trim()
    if (s.isEmpty()) return null
    if (isInviteScheme(s.substringBefore(':'))) return parseInviteDeepLink(s)
    if (TOKEN.matches(s)) return InviteLink(null, s)
    if (SCHEME.containsMatchIn(s) && !s.startsWith("http://", true) && !s.startsWith("https://", true)) return null
    val withScheme = if (SCHEME.containsMatchIn(s)) s else "https://$s"
    val url = withScheme.toHttpUrlOrNull() ?: return null
    // A link with a user name or password in it is not one the server shares; it could hide the real host.
    if (url.username.isNotEmpty() || url.password.isNotEmpty() || url.host.isBlank()) return null
    val segs = url.pathSegments
    val i = segs.lastIndexOf("invite")
    if (i < 0 || i + 1 >= segs.size) return null
    val token = segs[i + 1]
    if (!TOKEN.matches(token)) return null
    val prefix = segs.subList(0, i).filter { it.isNotEmpty() }
    val server = url.newBuilder().query(null).fragment(null).encodedPath("/").apply { prefix.forEach { addPathSegment(it) } }.build()
        .toString().trimEnd('/')
    return InviteLink(server, token)
}

private val SCHEME = Regex("^[A-Za-z][A-Za-z0-9+.-]*://")

/**
 * The address of a server as the app stores it: http or https, a host, no user name or
 * password, no query or fragment, no trailing slash; a path prefix (a server behind
 * `/streamero`) is kept. A bare host gets https. Null when [input] is not such an address.
 */
fun normalizeServerUrl(input: String?): String? {
    val s = input?.trim().orEmpty()
    if (s.isEmpty() || s.length > 2048) return null
    val hasScheme = SCHEME.containsMatchIn(s)
    if (hasScheme && !s.startsWith("http://", true) && !s.startsWith("https://", true)) return null
    val url = (if (hasScheme) s else "https://$s").toHttpUrlOrNull() ?: return null
    if (url.username.isNotEmpty() || url.password.isNotEmpty() || url.host.isBlank()) return null
    val segs = url.pathSegments.filter { it.isNotEmpty() }
    return url.newBuilder().query(null).fragment(null).encodedPath("/").apply { segs.forEach { addPathSegment(it) } }.build()
        .toString().trimEnd('/')
}

/**
 * True for a host that is only reachable on the home network (or a private overlay such as
 * Tailscale): private, loopback and link-local addresses, `.local`/`.lan`/`.home.arpa`/
 * `.internal` names and single-label names. Never looks a name up.
 */
fun isLocalHost(host: String): Boolean {
    val h = host.trim().trimEnd('.').removePrefix("[").removeSuffix("]").substringBefore('%').lowercase()
    if (h.isEmpty()) return false
    ipv4(h)?.let { return isLocalIpv4(it) }
    if (':' in h) {
        if (!h.all { it.isDigit() || it in 'a'..'f' || it == ':' || it == '.' }) return false
        // A literal address only (checked above), so this never reaches DNS.
        val a = runCatching { java.net.InetAddress.getByName(h) }.getOrNull() ?: return false
        val b = a.address
        if (b.size == 4) return isLocalIpv4(b.map { it.toInt() and 0xFF })
        val b0 = b[0].toInt() and 0xFF
        val b1 = b[1].toInt() and 0xFF
        return a.isLoopbackAddress || (b0 and 0xFE) == 0xFC || (b0 == 0xFE && (b1 and 0xC0) == 0x80)
    }
    if ('.' !in h) return true
    return LOCAL_SUFFIXES.any { h.endsWith(it) }
}

private val LOCAL_SUFFIXES = listOf(".local", ".lan", ".home.arpa", ".internal")

private fun ipv4(h: String): List<Int>? {
    val parts = h.split('.')
    if (parts.size != 4) return null
    val n = parts.map { p -> if (p.isEmpty() || p.length > 3 || !p.all { it.isDigit() }) return null else p.toInt() }
    return n.takeIf { it.all { x -> x in 0..255 } }
}

private fun isLocalIpv4(b: List<Int>): Boolean = when {
    b[0] == 10 || b[0] == 127 -> true
    b[0] == 172 && b[1] in 16..31 -> true
    b[0] == 192 && b[1] == 168 -> true
    b[0] == 169 && b[1] == 254 -> true
    b[0] == 100 && b[1] in 64..127 -> true
    else -> false
}

/** An http (not https) address of a server out on the internet: sign-in would cross it unencrypted. */
fun isCleartextRemote(serverUrl: String?): Boolean {
    val url = normalizeServerUrl(serverUrl)?.toHttpUrlOrNull() ?: return false
    return url.scheme == "http" && !isLocalHost(url.host)
}

/** A page of the web app on the same server, kept under the server's path prefix. */
fun webLink(base: String, vararg segments: String): String {
    val url = base.toHttpUrlOrNull() ?: return base
    return url.newBuilder().query(null).fragment(null).apply { segments.forEach { addPathSegment(it) } }.build().toString()
}

/** How an invite is shown in its field: the full link when it names a server, else just the token. */
fun inviteText(link: InviteLink): String = if (link.server != null) webLink(link.server, "invite", link.token) else link.token

/**
 * Invite links: this app's own scheme, and `streamero://` too (the form the web shares), so a
 * pasted link works in Streamero Manage as well.
 */
fun isInviteScheme(scheme: String?): Boolean = scheme.equals("streamero", true) || scheme.equals(APP_SCHEME, true)

/** `streamero://invite?server=https%3A%2F%2Fmedia.example.com&token=…`; null if it is not one. */
fun parseInviteDeepLink(link: String): InviteLink? {
    val uri = runCatching { URI(link.trim()) }.getOrNull() ?: return null
    if (!isInviteScheme(uri.scheme) || !uri.host.equals("invite", true)) return null
    val q = queryParams(uri.rawQuery) ?: return null
    val token = q["token"]?.takeIf { TOKEN.matches(it) } ?: return null
    // A server that is named must be a plain http(s) address; a bad one is not quietly dropped,
    // or the token would be sent to whatever server the app is connected to.
    val rawServer = q["server"]
    val server = if (rawServer == null) null else normalizeServerUrl(rawServer) ?: return null
    return InviteLink(server, token)
}

/** The decoded query; null when it cannot be decoded (a stray `%`). */
private fun queryParams(raw: String?): Map<String, String>? = runCatching {
    raw.orEmpty().split('&').filter { it.isNotEmpty() }.associate { p ->
        val k = p.substringBefore('=')
        val v = p.substringAfter('=', "")
        URLDecoder.decode(k, "UTF-8") to URLDecoder.decode(v, "UTF-8")
    }
}.getOrNull()

/** Why the invite sign-up form cannot be sent yet (in the order the user sees the fields). */
enum class SignUpProblem { Username, PasswordShort, Mismatch }

fun signUpProblem(username: String, password: String, confirm: String): SignUpProblem? = when {
    username.isBlank() || username.trim().length > 64 -> SignUpProblem.Username
    password.length < 8 -> SignUpProblem.PasswordShort
    password != confirm -> SignUpProblem.Mismatch
    else -> null
}

/** The account limit the server enforces (`too_many_profiles`); the add button hides at it. */
const val MAX_PROFILES = 6
val PROFILE_COLORS = listOf("#e5484d", "#f76b15", "#ffc53d", "#3dd68c", "#0090ff", "#8e4ec6", "#d6409f", "#6e6e80")
val PROFILE_RATINGS = listOf(0, 6, 7, 12, 13, 15, 16, 18)
private val PIN = Regex("^[0-9]{4,6}$")
private val HEX = Regex("^#[0-9a-fA-F]{6}$")

fun isValidPin(pin: String) = PIN.matches(pin)

/** A profile can be saved: a name of 1–40 characters, a colour, and a PIN only if it is 4–6 digits. */
fun profileFormValid(name: String, color: String, pin: String): Boolean =
    name.trim().length in 1..40 && HEX.matches(color) && (pin.isEmpty() || isValidPin(pin))

/** The request for a profile form: an empty PIN keeps the current one, removing wins over nothing. */
fun profileInput(name: String, color: String, isKid: Boolean, maxRating: Int?, pin: String, removePin: Boolean) =
    ProfileInput(name.trim(), color, isKid, maxRating, pin.ifEmpty { null }, removePin.takeIf { it })

/** An `otpauth://totp/Issuer:account?secret=…&issuer=…` link, as authenticator apps read it. */
data class OtpAuth(val issuer: String?, val account: String, val secret: String, val digits: Int, val period: Int)

fun parseOtpAuth(uri: String): OtpAuth? {
    val u = runCatching { URI(uri.trim()) }.getOrNull() ?: return null
    if (!u.scheme.equals("otpauth", true) || !u.host.equals("totp", true)) return null
    val label = URLDecoder.decode(u.rawPath.orEmpty().trimStart('/'), "UTF-8")
    val q = queryParams(u.rawQuery) ?: return null
    val secret = q["secret"]?.takeIf { it.isNotBlank() } ?: return null
    val labelIssuer = label.substringBefore(':', "").ifBlank { null }
    val account = label.substringAfter(':').trim()
    return OtpAuth(q["issuer"]?.ifBlank { null } ?: labelIssuer, account, secret,
        q["digits"]?.toIntOrNull() ?: 6, q["period"]?.toIntOrNull() ?: 30)
}

/** The secret in groups of four, easier to type into an app by hand. */
fun groupSecret(secret: String): String = secret.replace(" ", "").chunked(4).joinToString(" ")

/** What a TOTP field accepts: the 6-digit code, or a recovery code (letters, digits, dashes). */
fun cleanTotpCode(input: String): String = input.filter { it.isLetterOrDigit() || it == '-' }.take(16)

private val OIDC_ID = Regex("^[a-z0-9][a-z0-9-]{0,31}$")
fun isValidOidcId(id: String) = OIDC_ID.matches(id)

/** Scopes typed separated by spaces (or commas), without duplicates. */
fun parseScopes(text: String): List<String> = text.split(Regex("[\\s,]+")).filter { it.isNotBlank() }.distinct()

fun isHttpUrl(s: String): Boolean {
    val u = s.trim().toHttpUrlOrNull() ?: return false
    return u.host.isNotBlank()
}

/** A notification target can be saved and tested: a name and an http(s) address. */
fun notificationTargetValid(t: NotificationTarget): Boolean = t.name.isNotBlank() && t.name.length <= 80 && isHttpUrl(t.url)

/** A tuner or guide source needs a name and, depending on the source, an address or a file path. */
fun sourceValid(name: String, fromFile: Boolean, url: String, filePath: String): Boolean =
    name.isNotBlank() && if (fromFile) filePath.isNotBlank() else url.isNotBlank()

/** An optional number field: empty is "not set", anything else must be a number in range. */
fun optionalInt(text: String, range: IntRange): Int? = text.trim().takeIf { it.isNotEmpty() }?.toIntOrNull()?.coerceIn(range)

/** A restore only goes ahead when the backup's name was typed exactly. */
fun restoreConfirmed(typed: String, backupName: String) = typed == backupName

/** "1.2 GB" style sizes for backups. */
fun formatBytes(bytes: Long): String {
    val units = listOf("B", "KB", "MB", "GB", "TB")
    var v = bytes.toDouble()
    var i = 0
    while (v >= 1024 && i < units.lastIndex) { v /= 1024; i++ }
    return if (i == 0) "$bytes B" else String.format(java.util.Locale.ROOT, "%.1f %s", v, units[i])
}
