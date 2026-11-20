package net.scyde.streamero.data

import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TokenStorageTest {
    private val key: SecretKey = KeyGenerator.getInstance("AES").apply { init(256) }.generateKey()
    private val cipher = AesGcmTokenCipher { key }

    /** A device whose Keystore cannot be used. */
    private val broken = object : TokenCipher {
        override fun seal(plain: String) = throw IllegalStateException("keystore unavailable")
        override fun open(sealed: String) = throw IllegalStateException("keystore unavailable")
    }

    @Test fun sealsAndOpens() {
        val sealed = cipher.seal("secret-token")
        assertFalse(sealed.contains("secret-token"))
        assertEquals("secret-token", cipher.open(sealed))
    }

    @Test fun everySealUsesAFreshIv() {
        assertNotEquals(cipher.seal("same"), cipher.seal("same"))
    }

    @Test fun tamperedValueIsRefused() {
        val sealed = cipher.seal("secret-token")
        val flipped = sealed.dropLast(2) + (if (sealed.last() == 'A') "BB" else "AA")
        assertTrue(runCatching { cipher.open(flipped) }.isFailure)
        assertTrue(runCatching { cipher.open("plain") }.isFailure)
        assertTrue(runCatching { cipher.open("v1:onlyonepart") }.isFailure)
    }

    @Test fun plainTokenFromOlderVersionIsMigrated() {
        val r = TokenStorage.read(sealed = null, legacy = "old-token", cipher = cipher)
        assertEquals("old-token", r.token)
        assertNull(r.legacy)
        assertEquals("old-token", cipher.open(r.sealed!!))
        assertTrue(r.changed(null, "old-token"))
    }

    @Test fun migrationThatCannotSealKeepsUserSignedIn() {
        val r = TokenStorage.read(sealed = null, legacy = "old-token", cipher = broken)
        assertEquals("old-token", r.token)
        assertEquals("old-token", r.legacy)
        assertFalse(r.changed(null, "old-token"))
    }

    @Test fun sealedTokenIsReadWithoutRewriting() {
        val sealed = cipher.seal("t")
        val r = TokenStorage.read(sealed, null, cipher)
        assertEquals("t", r.token)
        assertFalse(r.changed(sealed, null))
    }

    @Test fun sealedTokenWithLostKeyFallsBackToPlainCopy() {
        val other = AesGcmTokenCipher { KeyGenerator.getInstance("AES").apply { init(256) }.generateKey() }
        val sealed = other.seal("t")
        assertNull(TokenStorage.read(sealed, null, cipher).token)
        assertEquals("plain", TokenStorage.read(sealed, "plain", cipher).token)
    }

    @Test fun writingNullRemovesBoth() {
        val r = TokenStorage.write(null, cipher)
        assertNull(r.token); assertNull(r.sealed); assertNull(r.legacy)
    }

    @Test fun writingStoresOnlyTheSealedValue() {
        val r = TokenStorage.write("new", cipher)
        assertNull(r.legacy)
        assertEquals("new", cipher.open(r.sealed!!))
    }
}
