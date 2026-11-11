package net.scyde.streamero.api

import android.content.Context
import android.net.Uri
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Single sign-on (OpenID Connect) from the app (D-037).
 *
 * Redirect choice: the server is a broker. The provider only ever redirects to the
 * server's own https callback; the server then sends the browser to the custom scheme
 * `streamero://oidc` with a one-time code. A custom scheme (instead of App Links) works
 * with any self-hosted server: App Links would need an assetlinks.json per server
 * domain verified for this app's signing key. The code is useless without the PKCE
 * verifier that only this app instance holds (the server checks
 * BASE64URL(SHA-256(verifier)) on exchange), so another app claiming the scheme
 * cannot turn an intercepted redirect into a session.
 */
object Pkce {
    private val random = SecureRandom()
    private val b64 = Base64.getUrlEncoder().withoutPadding()

    /** 32 random bytes, base64url without padding (43 characters). */
    fun verifier(): String = ByteArray(32).also(random::nextBytes).let(b64::encodeToString)

    /** S256 challenge: BASE64URL(SHA-256(verifier)). */
    fun challenge(verifier: String): String =
        b64.encodeToString(MessageDigest.getInstance("SHA-256").digest(verifier.toByteArray(Charsets.US_ASCII)))
}

/** The only redirect URI the server accepts from apps. */
const val SSO_REDIRECT = "streamero://oidc"
private const val SSO_REDIRECT_ENCODED = "streamero%3A%2F%2Foidc"

/** URL to open in the browser to sign in with a provider. */
fun ssoAuthorizeUrl(authorizeUrl: String, challenge: String): String {
    val sep = if ('?' in authorizeUrl) '&' else '?'
    return authorizeUrl + sep + "client=app&redirectUri=" + SSO_REDIRECT_ENCODED + "&codeChallenge=" + challenge
}

/** What the browser sent back to the app. */
sealed interface SsoResult {
    data class Code(val code: String) : SsoResult
    data class Failed(val error: String) : SsoResult
}

/** Parses a `streamero://oidc?code=…` / `?error=…` redirect (null if it is not one). */
fun parseSsoRedirect(scheme: String?, host: String?, code: String?, error: String?): SsoResult? {
    if (!scheme.equals("streamero", ignoreCase = true) || !host.equals("oidc", ignoreCase = true)) return null
    return when {
        !code.isNullOrBlank() -> SsoResult.Code(code)
        else -> SsoResult.Failed(error?.takeIf { it.isNotBlank() } ?: "internal")
    }
}

/**
 * Hands the redirect from MainActivity to the login screen, and keeps the pending
 * verifier in private preferences so a sign-in survives the app being killed while
 * the browser is open.
 */
object SsoBroker {
    private const val PREFS = "sso"
    private const val KEY = "verifier"
    private val results = MutableStateFlow<SsoResult?>(null)
    val pending: StateFlow<SsoResult?> = results.asStateFlow()

    fun begin(context: Context): String {
        val v = Pkce.verifier()
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putString(KEY, v).apply()
        return v
    }

    /** Called by MainActivity for every incoming intent. */
    fun deliver(uri: Uri?) {
        if (uri == null) return
        parseSsoRedirect(uri.scheme, uri.host, uri.getQueryParameter("code"), uri.getQueryParameter("error"))?.let { results.value = it }
    }

    /** Takes the result and the stored verifier (each usable once). */
    fun take(context: Context): Pair<SsoResult, String?>? {
        val r = results.value ?: return null
        results.value = null
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val v = prefs.getString(KEY, null)
        prefs.edit().remove(KEY).apply()
        return r to v
    }
}
