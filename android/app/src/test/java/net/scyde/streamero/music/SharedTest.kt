package net.scyde.streamero.music

import java.io.IOException
import kotlin.random.Random
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.delay
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class SharedTest {
    private class Conn(val n: Int) { var open = true }

    @Test fun quickCallersShareOneConnection() = runTest {
        var made = 0
        val shared = Shared<Conn> { delay(100); Conn(++made) }
        val all = List(5) { async { shared.get() } }.awaitAll()
        assertEquals(1, made)
        assertTrue(all.all { it === all[0] })
    }

    @Test fun aClosedConnectionIsReplaced() = runTest {
        var made = 0
        val shared = Shared<Conn>(valid = { it.open }) { Conn(++made) }
        val first = shared.get()
        assertSame(first, shared.current)
        first.open = false
        assertNull(shared.current)
        assertEquals(2, shared.get().n)
    }

    @Test fun forgetDropsOnlyTheGivenOne() = runTest {
        var made = 0
        val shared = Shared<Conn> { Conn(++made) }
        val first = shared.get()
        shared.forget(Conn(99))
        assertSame(first, shared.current)
        shared.forget(first)
        assertNull(shared.current)
        assertEquals(2, shared.get().n)
    }

    @Test fun aFailedAttemptIsTriedAgain() = runTest {
        var calls = 0
        val shared = Shared<Conn> { if (++calls == 1) throw IOException("service not up") else Conn(calls) }
        try { shared.get(); fail() } catch (e: IOException) { assertEquals("service not up", e.message) }
        assertEquals(2, shared.get().n)
    }

    @Test fun shufflingAnEmptyAlbumPicksNothing() {
        assertNull(randomStart(0))
        assertNull(randomStart(-1))
        assertEquals(0, randomStart(1))
        repeat(50) { val i = randomStart(7, Random(it))!!; assertTrue(i in 0 until 7) }
    }
}
