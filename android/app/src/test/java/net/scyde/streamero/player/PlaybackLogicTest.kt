package net.scyde.streamero.player

import net.scyde.streamero.api.PlaybackInfo
import net.scyde.streamero.api.PlaybackSubtitle
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PlaybackLogicTest {
    // The file has 4 subtitle tracks: 0 en (text), 1 hr (PGS), 2 de (text), 3 hr (text).
    // The player only gets the text ones, so positions and indexes differ.
    private val pi = PlaybackInfo(
        fileId = "f", method = "direct", url = "/v",
        subtitles = listOf(sub(0, "en"), sub(2, "de"), sub(3, "hr")),
    )
    private fun sub(i: Int, lang: String) = PlaybackSubtitle(i, lang, url = "/s$i.vtt")

    @Test fun titlePageChoiceIsMatchedByTrackIndexNotPosition() {
        assertEquals(SubtitleChoice.Text(2), Subtitles.initial(pi, 3))
        assertEquals(SubtitleChoice.Text(1), Subtitles.initial(pi, 2))
        assertEquals(SubtitleChoice.Text(0), Subtitles.initial(pi, 0))
    }

    @Test fun automaticOffAndUnknownChoices() {
        assertEquals(SubtitleChoice.Off, Subtitles.initial(pi, null))
        assertEquals(SubtitleChoice.Text(1), Subtitles.initial(pi.copy(subtitleDefault = 1), null))
        assertEquals(SubtitleChoice.Off, Subtitles.initial(pi.copy(subtitleDefault = 1), -1))
        assertEquals(SubtitleChoice.Off, Subtitles.initial(pi, 7))
    }

    @Test fun aBurnedTrackIsWhatTheStreamCarries() {
        val burned = pi.copy(method = "transcode", burnedSubtitle = 1)
        assertEquals(SubtitleChoice.Burned(1), Subtitles.initial(burned, 1))
        assertEquals(SubtitleChoice.Burned(1), Subtitles.afterReload(burned, SubtitleChoice.Text(0)))
        assertEquals(SubtitleChoice.Text(2), Subtitles.afterReload(pi, SubtitleChoice.Text(2)))
        assertEquals(SubtitleChoice.Off, Subtitles.afterReload(pi, SubtitleChoice.Burned(1)))
    }

    @Test fun onlyBurningInOrOutNeedsANewStream() {
        assertFalse(Subtitles.needsReload(SubtitleChoice.Off, SubtitleChoice.Text(1)))
        assertFalse(Subtitles.needsReload(SubtitleChoice.Text(0), SubtitleChoice.Text(1)))
        assertTrue(Subtitles.needsReload(SubtitleChoice.Text(0), SubtitleChoice.Burned(1)))
        assertTrue(Subtitles.needsReload(SubtitleChoice.Burned(1), SubtitleChoice.Off))
        assertFalse(Subtitles.needsReload(SubtitleChoice.Burned(1), SubtitleChoice.Burned(1)))
        assertEquals(1, Subtitles.requestIndex(SubtitleChoice.Burned(1)))
        assertEquals(-1, Subtitles.requestIndex(SubtitleChoice.Text(0)))
    }

    @Test fun firstErrorFallsBackSecondFails() {
        val r = ErrorRecovery()
        assertFalse(r.safe)
        assertEquals(ErrorAction.Fallback, r.onError())
        assertTrue(r.safe)
        assertEquals(ErrorAction.Fail, r.onError())
        assertEquals(listOf("h264"), SAFE_PROFILE.videoCodecs)
        assertEquals(listOf("aac"), SAFE_PROFILE.audioCodecs)
        assertEquals(listOf("mp4"), SAFE_PROFILE.containers)
    }

    @Test fun hevcNeedsAHardwareMain10Decoder() {
        assertEquals(listOf("h264"), videoCodecs(listOf(VideoDecoder("video/hevc", hardware = true, main10 = false))))
        assertEquals(listOf("h264"), videoCodecs(listOf(VideoDecoder("video/hevc", hardware = false, main10 = true))))
        assertEquals(listOf("h264", "hevc", "vp9"), videoCodecs(listOf(VideoDecoder("video/hevc", true, true), VideoDecoder("video/x-vnd.on2.vp9", false))))
    }

    @Test fun nextEpisodeStartsOnlyWhenWanted() {
        assertTrue(autoplayNextOnEnd(autoplay = true, hasNext = true, grouped = false))
        assertFalse(autoplayNextOnEnd(autoplay = false, hasNext = true, grouped = false))
        assertFalse(autoplayNextOnEnd(autoplay = true, hasNext = false, grouped = false))
        assertFalse(autoplayNextOnEnd(autoplay = true, hasNext = true, grouped = true))
    }

    @Test fun aReloadKeepsAPausedPlayerPaused() {
        assertFalse(playWhenReadyAfterReload(wasPlaying = false, grouped = false))
        assertTrue(playWhenReadyAfterReload(wasPlaying = true, grouped = false))
        assertTrue(playWhenReadyAfterReload(wasPlaying = null, grouped = false))
        assertFalse(playWhenReadyAfterReload(wasPlaying = true, grouped = true))
    }

    @Test fun captionLookFollowsTheSettings() {
        val d = captionLook(null, null, null)
        assertEquals(0xFFFFFFFF.toInt(), d.foreground)
        assertTrue(d.shadow)
        assertEquals(0, d.background)
        assertEquals(1f, d.scale)
        val box = captionLook("large", "yellow", "box")
        assertFalse(box.shadow)
        assertTrue(box.background != 0)
        assertTrue(box.scale > 1f)
        assertEquals(0xFFFFE14D.toInt(), box.foreground)
    }
}
