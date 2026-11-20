package net.scyde.streamero.data

import android.content.Context
import androidx.datastore.preferences.core.MutablePreferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withContext
import net.scyde.streamero.api.Api

private val Context.store by preferencesDataStore("session")
private val SERVER = stringPreferencesKey("server")
/** The token as older versions stored it, in plain text; moved to [SEALED_TOKEN] on the next start. */
private val TOKEN = stringPreferencesKey("token")
/** The token encrypted with a key held by the Android Keystore (see [AesGcmTokenCipher]). */
private val SEALED_TOKEN = stringPreferencesKey("token_sealed")
private val LANGUAGE = stringPreferencesKey("language")

/** The server this device is connected to and the sign-in token for it. */
class Session(
    private val context: Context,
    private val cipher: TokenCipher = AesGcmTokenCipher(AesGcmTokenCipher::keystoreKey),
) {
    var api: Api? = null
        private set

    suspend fun restore(): Api? {
        val p = context.store.data.first()
        val server = p[SERVER] ?: return null
        // Keystore work can take a moment on some devices: off the main thread.
        val read = withContext(Dispatchers.IO) { TokenStorage.read(p[SEALED_TOKEN], p[TOKEN], cipher) }
        if (read.changed(p[SEALED_TOKEN], p[TOKEN])) context.store.edit { it.putToken(read) }
        return Api(server, read.token).also { api = it }
    }

    suspend fun setServer(url: String): Api {
        context.store.edit { it[SERVER] = url; it.remove(TOKEN); it.remove(SEALED_TOKEN) }
        return Api(url).also { api = it }
    }

    suspend fun setToken(token: String?) {
        val write = withContext(Dispatchers.IO) { TokenStorage.write(token, cipher) }
        context.store.edit { it.putToken(write) }
        api?.token = token
    }

    suspend fun forgetServer() {
        context.store.edit { it.clear() }
        api = null
    }

    suspend fun language(): String? = context.store.data.first()[LANGUAGE]
    suspend fun setLanguage(lang: String) { context.store.edit { it[LANGUAGE] = lang } }

    private fun MutablePreferences.putToken(r: TokenStorage.Read) {
        if (r.sealed != null) this[SEALED_TOKEN] = r.sealed else remove(SEALED_TOKEN)
        if (r.legacy != null) this[TOKEN] = r.legacy else remove(TOKEN)
    }
}
