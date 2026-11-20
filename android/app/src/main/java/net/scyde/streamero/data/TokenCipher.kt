package net.scyde.streamero.data

import java.security.KeyStore
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Seals the session token before it is written to disk. */
interface TokenCipher {
    fun seal(plain: String): String
    fun open(sealed: String): String
}

/**
 * AES-256-GCM with a random 96-bit IV per value: `v1:<iv>:<ciphertext+tag>` in base64.
 * The key comes from [key]; on a device that is the Android Keystore ([keystoreKey]), so the
 * key itself never exists in the app's files and cannot be copied off the device with them.
 */
class AesGcmTokenCipher(private val key: () -> SecretKey) : TokenCipher {
    private val b64 = Base64.getEncoder().withoutPadding()
    private val unb64 = Base64.getDecoder()

    override fun seal(plain: String): String {
        val c = Cipher.getInstance(TRANSFORMATION)
        // No IV is passed: the provider picks a fresh random one (the Keystore refuses caller IVs).
        c.init(Cipher.ENCRYPT_MODE, key())
        val out = c.doFinal(plain.toByteArray(Charsets.UTF_8))
        return PREFIX + b64.encodeToString(c.iv) + ":" + b64.encodeToString(out)
    }

    override fun open(sealed: String): String {
        require(sealed.startsWith(PREFIX)) { "unknown token format" }
        val parts = sealed.removePrefix(PREFIX).split(':')
        require(parts.size == 2) { "malformed token" }
        val iv = unb64.decode(parts[0])
        require(iv.size == IV_BYTES) { "malformed token" }
        val c = Cipher.getInstance(TRANSFORMATION)
        c.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(TAG_BITS, iv))
        return String(c.doFinal(unb64.decode(parts[1])), Charsets.UTF_8)
    }

    companion object {
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
        private const val PREFIX = "v1:"
        private const val TAG_BITS = 128
        private const val IV_BYTES = 12
        private const val ALIAS = "streamero.session"

        /** The app's session key in the Android Keystore, created on first use. */
        fun keystoreKey(): SecretKey {
            val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
            (ks.getEntry(ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }
            val spec = android.security.keystore.KeyGenParameterSpec.Builder(
                ALIAS, android.security.keystore.KeyProperties.PURPOSE_ENCRYPT or android.security.keystore.KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(android.security.keystore.KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(android.security.keystore.KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build()
            return KeyGenerator.getInstance(android.security.keystore.KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
                .apply { init(spec) }
                .generateKey()
        }
    }
}

/**
 * How the stored token is read and written, independent of the storage: [sealed] is the
 * encrypted value, [legacy] the plain one older versions wrote.
 */
object TokenStorage {
    /** What [read] found: the token to use and what should be stored from now on. */
    data class Read(val token: String?, val sealed: String?, val legacy: String?) {
        fun changed(oldSealed: String?, oldLegacy: String?) = sealed != oldSealed || legacy != oldLegacy
    }

    /**
     * Prefers the sealed token. A plain one from an older version is sealed and the plain copy
     * dropped; when sealing is impossible (a device whose Keystore is broken) the plain one is
     * kept, because signing the user out would not make anything safer. A sealed token that can
     * no longer be opened (its key is gone) falls back to a plain copy if there is one.
     */
    fun read(sealed: String?, legacy: String?, cipher: TokenCipher): Read {
        if (sealed != null) {
            val token = try { cipher.open(sealed) } catch (e: Exception) { null }
            if (token != null) return Read(token, sealed, null)
            return if (legacy != null) migrate(legacy, cipher) else Read(null, null, null)
        }
        if (legacy != null) return migrate(legacy, cipher)
        return Read(null, null, null)
    }

    /** What to store for a new token (null removes both). */
    fun write(token: String?, cipher: TokenCipher): Read = if (token == null) Read(null, null, null) else migrate(token, cipher)

    private fun migrate(token: String, cipher: TokenCipher): Read {
        val sealed = try { cipher.seal(token) } catch (e: Exception) { null }
        return if (sealed != null) Read(token, sealed, null) else Read(token, null, token)
    }
}
