package net.scyde.streamero.data

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.first
import net.scyde.streamero.api.Api

private val Context.store by preferencesDataStore("session")
private val SERVER = stringPreferencesKey("server")
private val TOKEN = stringPreferencesKey("token")
private val LANGUAGE = stringPreferencesKey("language")

/** The server this device is connected to and the sign-in token for it. */
class Session(private val context: Context) {
    var api: Api? = null
        private set

    suspend fun restore(): Api? {
        val p = context.store.data.first()
        val server = p[SERVER] ?: return null
        return Api(server, p[TOKEN]).also { api = it }
    }

    suspend fun setServer(url: String): Api {
        context.store.edit { it[SERVER] = url; it.remove(TOKEN) }
        return Api(url).also { api = it }
    }

    suspend fun setToken(token: String?) {
        context.store.edit { if (token == null) it.remove(TOKEN) else it[TOKEN] = token }
        api?.token = token
    }

    suspend fun forgetServer() {
        context.store.edit { it.clear() }
        api = null
    }

    suspend fun language(): String? = context.store.data.first()[LANGUAGE]
    suspend fun setLanguage(lang: String) { context.store.edit { it[LANGUAGE] = lang } }
}
