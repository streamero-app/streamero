package net.scyde.streamero.books

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SleepTimerTest {
    @Test fun offIsNeverDue() = assertFalse(sleepDue(null, 10_000, 0, 10_000, 0))

    @Test fun endOfChapterFollowsTheBook() {
        assertFalse(sleepDue(SLEEP_END_OF_CHAPTER, positionMs = 59_000, sleepAtMs = 60_000, now = 0, deadline = 0))
        assertTrue(sleepDue(SLEEP_END_OF_CHAPTER, positionMs = 60_000, sleepAtMs = 60_000, now = 0, deadline = 0))
    }

    @Test fun minutesFollowTheClock() {
        assertFalse(sleepDue(15, positionMs = 0, sleepAtMs = 0, now = 899_999, deadline = 900_000))
        assertTrue(sleepDue(15, positionMs = 0, sleepAtMs = 0, now = 900_000, deadline = 900_000))
    }
}
