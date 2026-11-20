package net.scyde.streamero.player

import net.scyde.streamero.api.MediaSegment
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PlayerRulesTest {
    @Test fun errorCodesAreSortedByWhatHelps() {
        assertEquals(ErrorKind.BehindLive, errorKind(1002))
        assertEquals(ErrorKind.Source, errorKind(2001)) // network connection failed
        assertEquals(ErrorKind.Source, errorKind(2004)) // bad HTTP status
        assertEquals(ErrorKind.Playback, errorKind(4001)) // decoder init failed
        assertEquals(ErrorKind.Playback, errorKind(3001)) // malformed container
        assertEquals(ErrorKind.Playback, errorKind(5001)) // audio track init failed
    }

    @Test fun aNetworkBlipIsRetriedAsItWasNotDowngraded() {
        val r = ErrorRecovery()
        assertEquals(ErrorAction.Retry, r.onError(ErrorKind.Source, 1_000))
        assertFalse(r.safe)
        // Failing again right away: the stream itself is the problem, try the safe one.
        assertEquals(ErrorAction.Fallback, r.onError(ErrorKind.Source, 5_000))
        assertTrue(r.safe)
        assertEquals(ErrorAction.Fail, r.onError(ErrorKind.Source, 6_000))
    }

    @Test fun aLaterBlipIsRetriedAgainButNotInALoop() {
        val r = ErrorRecovery()
        assertEquals(ErrorAction.Retry, r.onError(ErrorKind.Source, 0))
        assertEquals(ErrorAction.Retry, r.onError(ErrorKind.Source, ErrorRecovery.RETRY_GAP_MS))
        assertEquals(ErrorAction.Fallback, r.onError(ErrorKind.Source, ErrorRecovery.RETRY_GAP_MS + 10))
    }

    @Test fun fallingBehindALiveStreamResyncsEveryTime() {
        val r = ErrorRecovery()
        repeat(3) { assertEquals(ErrorAction.Resync, r.onError(ErrorKind.BehindLive, it * 1000L)) }
        assertFalse(r.safe)
    }

    @Test fun aDecoderFailureFallsBackOnceThenFails() {
        val r = ErrorRecovery()
        assertEquals(ErrorAction.Fallback, r.onError(ErrorKind.Playback, 0))
        assertEquals(ErrorAction.Fail, r.onError(ErrorKind.Playback, 100_000))
    }

    @Test fun screenStaysOnOnlyWhilePlayingOrAboutTo() {
        assertTrue(keepsScreenOn(playWhenReady = true, buffering = false, ready = true))
        assertTrue(keepsScreenOn(playWhenReady = true, buffering = true, ready = false))
        assertFalse(keepsScreenOn(playWhenReady = false, buffering = false, ready = true)) // paused
        assertFalse(keepsScreenOn(playWhenReady = true, buffering = false, ready = false)) // ended or idle
    }

    private val segments = listOf(MediaSegment("recap", 0, 30_000), MediaSegment("intro", 60_000, 90_000), MediaSegment("credits", 2_500_000, 2_600_000))

    @Test fun introIsFoundUntilItsLastSecond() {
        assertEquals("recap", introAt(segments, 0)?.kind)
        assertEquals("intro", introAt(segments, 60_000)?.kind)
        assertEquals("intro", introAt(segments, 88_999)?.kind)
        assertNull(introAt(segments, 89_000))
        assertNull(introAt(segments, 45_000))
        assertNull(introAt(emptyList(), 10))
        // A segment shorter than a second never offers a skip.
        assertNull(introAt(listOf(MediaSegment("intro", 100, 600)), 200))
    }

    @Test fun creditsStartAtTheirSegment() {
        assertFalse(inCredits(segments, 2_499_999))
        assertTrue(inCredits(segments, 2_500_000))
        assertFalse(inCredits(emptyList(), 2_500_000))
    }

    @Test fun nextUpShowsNearTheEndOnly() {
        assertFalse(showNextUp(hasNext = false, ended = true, inCredits = true, durationMs = 100_000, positionMs = 99_000))
        assertTrue(showNextUp(true, ended = true, inCredits = false, durationMs = 0, positionMs = 0))
        assertTrue(showNextUp(true, ended = false, inCredits = true, durationMs = 0, positionMs = 0))
        assertTrue(showNextUp(true, ended = false, inCredits = false, durationMs = 100_000, positionMs = 85_000))
        assertFalse(showNextUp(true, ended = false, inCredits = false, durationMs = 100_000, positionMs = 50_000))
        // Not started yet, on a short clip: position 0 is not the end.
        assertFalse(showNextUp(true, ended = false, inCredits = false, durationMs = 15_000, positionMs = 0))
    }

    @Test fun aSavedPlaceWinsOverTheTitlePageStart() {
        assertEquals(120_000L, startPosition(120_000, 5_000))
        assertEquals(5_000L, startPosition(0, 5_000))
        assertNull(startPosition(0, null))
    }

    @Test fun seekingBackFromTheEndClearsIt() {
        assertTrue(stillEnded(false, 4))
        assertFalse(stillEnded(true, 3))
        assertFalse(stillEnded(true, 2))
        assertTrue(stillEnded(true, 1)) // idle (released, or a reload starting) keeps what it was
        assertFalse(stillEnded(false, 1))
    }

    @Test fun closingSavesTheSpotUnlessThereIsNothingToSave() {
        assertEquals(42_000L to 100_000L, closingProgress(loaded = true, positionMs = 42_000, durationMs = 100_000, ended = false))
        assertNull(closingProgress(loaded = false, positionMs = 42_000, durationMs = 100_000, ended = false))
        assertNull(closingProgress(loaded = true, positionMs = 0, durationMs = 100_000, ended = false))
        assertNull(closingProgress(loaded = true, positionMs = 100_000, durationMs = 100_000, ended = true)) // reported as watched
    }

    @Test fun av1AndHevcNeedHardware() {
        assertEquals(listOf("h264"), videoCodecs(listOf(VideoDecoder("video/av01", hardware = false))))
        assertEquals(listOf("h264", "av1"), videoCodecs(listOf(VideoDecoder("video/av01", hardware = true))))
    }

    @Test fun dolbyAudioIsReportedWhenDecodedOrPassedThrough() {
        assertEquals(listOf("aac", "mp3", "opus", "flac", "vorbis"), audioCodecs(false, false, false, false))
        assertEquals(listOf("aac", "mp3", "opus", "flac", "vorbis", "ac3"), audioCodecs(true, false, false, false))
        assertEquals(listOf("aac", "mp3", "opus", "flac", "vorbis", "ac3", "eac3"), audioCodecs(false, false, true, true))
        assertEquals(listOf("aac", "mp3", "opus", "flac", "vorbis", "eac3"), audioCodecs(false, true, false, false))
    }
}
