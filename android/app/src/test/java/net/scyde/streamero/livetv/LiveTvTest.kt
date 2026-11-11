package net.scyde.streamero.livetv

import kotlinx.coroutines.test.runTest
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.DeviceProfile
import net.scyde.streamero.api.LiveTvChannel
import net.scyde.streamero.api.LiveTvGuide
import net.scyde.streamero.api.LiveTvGuideRow
import net.scyde.streamero.api.LiveTvProgram
import net.scyde.streamero.api.Profile
import net.scyde.streamero.api.User
import net.scyde.streamero.api.UserPermissions
import net.scyde.streamero.api.json
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.time.Instant
import java.time.ZoneId

class LiveTvTest {
    private fun prog(id: String, start: String, end: String, status: String? = null) =
        LiveTvProgram(id = id, channelId = "c1", title = "T$id", start = start, end = end, recordingStatus = status)

    private val now = Instant.parse("2026-11-11T20:30:00Z")

    @Test fun progressIsClampedAndHandlesBadTimes() {
        assertEquals(0.5f, programProgress(prog("a", "2026-11-11T20:00:00Z", "2026-11-11T21:00:00Z"), now), 0.001f)
        assertEquals(0f, programProgress(prog("b", "2026-11-11T21:00:00Z", "2026-11-11T22:00:00Z"), now), 0.001f)
        assertEquals(1f, programProgress(prog("c", "2026-11-11T19:00:00Z", "2026-11-11T20:00:00Z"), now), 0.001f)
        assertEquals(0f, programProgress(prog("d", "garbage", "2026-11-11T22:00:00Z"), now), 0.001f)
        assertEquals(0f, programProgress(null, now), 0.001f)
        // Offsets other than Z are the same instant.
        assertEquals(0.5f, programProgress(prog("e", "2026-11-11T21:00:00+01:00", "2026-11-11T22:00:00+01:00"), now), 0.001f)
    }

    @Test fun nowAndNextFromUnsortedProgrammes() {
        val ps = listOf(
            prog("next", "2026-11-11T21:00:00Z", "2026-11-11T22:00:00Z"),
            prog("past", "2026-11-11T19:00:00Z", "2026-11-11T20:00:00Z"),
            prog("later", "2026-11-11T22:00:00Z", "2026-11-11T23:00:00Z"),
            prog("now", "2026-11-11T20:00:00Z", "2026-11-11T21:00:00Z"),
        )
        val (n, x) = nowAndNext(ps, now)
        assertEquals("now", n?.id)
        assertEquals("next", x?.id)
        val (gapNow, gapNext) = nowAndNext(listOf(ps[0]), now)
        assertNull(gapNow)
        assertEquals("next", gapNext?.id)
    }

    @Test fun channelNowNextPrefersTheServer() {
        val server = prog("s", "2026-11-11T20:00:00Z", "2026-11-11T21:00:00Z")
        val c = LiveTvChannel(id = "c1", number = "1", name = "One", tunerId = "t", now = server)
        assertEquals("s", channelNowNext(c, listOf(prog("x", "2026-11-11T20:00:00Z", "2026-11-11T21:00:00Z")), now).first?.id)
        assertEquals("x", channelNowNext(c.copy(now = null), listOf(prog("x", "2026-11-11T20:00:00Z", "2026-11-11T21:00:00Z")), now).first?.id)
    }

    @Test fun scheduleDropsEndedProgrammesAndSorts() {
        val c = LiveTvChannel(id = "c1", number = "1", name = "One", tunerId = "t")
        val g = LiveTvGuide("s", "e", listOf(LiveTvGuideRow(c, listOf(
            prog("b", "2026-11-11T21:00:00Z", "2026-11-11T22:00:00Z"),
            prog("old", "2026-11-11T19:00:00Z", "2026-11-11T20:00:00Z"),
            prog("a", "2026-11-11T20:00:00Z", "2026-11-11T21:00:00Z"),
        ))))
        assertEquals(listOf("a", "b"), channelSchedule(g, "c1", now).map { it.id })
        assertTrue(channelSchedule(g, "other", now).isEmpty())
        assertTrue(channelSchedule(null, "c1", now).isEmpty())
    }

    @Test fun recordableOnlyWhenNotEndedAndNotScheduled() {
        assertTrue(canRecord(prog("a", "2026-11-11T21:00:00Z", "2026-11-11T22:00:00Z"), now))
        assertTrue(canRecord(prog("a", "2026-11-11T20:00:00Z", "2026-11-11T21:00:00Z"), now)) // airing now
        assertFalse(canRecord(prog("a", "2026-11-11T19:00:00Z", "2026-11-11T20:00:00Z"), now))
        assertFalse(canRecord(prog("a", "2026-11-11T21:00:00Z", "2026-11-11T22:00:00Z", "scheduled"), now))
        assertTrue(canRecord(prog("a", "2026-11-11T21:00:00Z", "2026-11-11T22:00:00Z", "cancelled"), now))
    }

    @Test fun timesInTheViewersZone() {
        val p = prog("a", "2026-11-11T20:00:00Z", "2026-11-11T21:30:00Z")
        assertEquals("21:00–22:30", timeRange(p, ZoneId.of("Europe/Zagreb")))
        assertEquals("21:00", startTime(p, ZoneId.of("Europe/Zagreb")))
    }

    @Test fun permissions() {
        val viewer = User(id = "u", username = "u", permissions = UserPermissions(liveTv = true))
        assertTrue(canWatchLiveTv(viewer))
        assertFalse(canManageLiveTv(viewer))
        assertFalse(canWatchLiveTv(User(id = "u", username = "u", permissions = UserPermissions(liveTv = false))))
        val admin = User(id = "a", username = "a", isAdmin = true, permissions = UserPermissions(liveTv = false))
        assertTrue(canWatchLiveTv(admin))
        assertTrue(canManageLiveTv(admin))
        assertFalse(canManageLiveTv(admin.copy(currentProfile = Profile(id = "p", name = "Kid", color = "#fff", isKid = true))))
        assertFalse(canWatchLiveTv(null))
    }

    @Test fun favouriteToggleKeepsOrder() {
        val cs = listOf(LiveTvChannel("1", "1", "A", "t"), LiveTvChannel("2", "2", "B", "t"))
        val out = withFavorite(cs, "2", true)
        assertEquals(listOf("1", "2"), out.map { it.id })
        assertTrue(out[1].favorite)
        assertFalse(out[0].favorite)
    }

    @Test fun parsesChannelsWithNowNextAndUnknownFields() {
        val text = """[{"id":"c1","number":"5.1","name":"KXYZ","tunerId":"t","favorite":true,"logoUrl":"/stream/img/x/logo?exp=1&sig=s",
            "category":"news","futureField":1,
            "now":{"id":"p1","channelId":"c1","title":"News","start":"2026-11-11T20:00:00Z","end":"2026-11-11T21:00:00Z","categories":["News"],"isNew":true,"recordingStatus":"scheduled"},
            "next":{"id":"p2","channelId":"c1","title":"Film","start":"2026-11-11T21:00:00Z","end":"2026-11-11T23:00:00Z","categories":[],"isNew":false,"season":2,"episode":3}}]"""
        val cs = json.decodeFromString(kotlinx.serialization.builtins.ListSerializer(LiveTvChannel.serializer()), text)
        assertEquals("5.1", cs[0].number)
        assertTrue(cs[0].favorite)
        assertEquals("scheduled", cs[0].now?.recordingStatus)
        assertEquals(3, cs[0].next?.episode)
        assertTrue(cs[0].now!!.isNew)
    }
}

class LiveTvApiTest {
    private val server = MockWebServer()
    private lateinit var api: Api

    @Before fun start() { server.start(); api = Api(server.url("/").toString(), "tok") }
    @After fun stop() = server.shutdown()

    @Test fun favouritesUsePutAndDelete() = runTest {
        server.enqueue(MockResponse().setResponseCode(204))
        server.enqueue(MockResponse().setResponseCode(204))
        api.setLiveFavorite("c1", true)
        api.setLiveFavorite("c1", false)
        val put = server.takeRequest()
        assertEquals("PUT", put.method)
        assertEquals("/api/v1/livetv/channels/c1/favorite", put.path)
        assertEquals("DELETE", server.takeRequest().method)
    }

    @Test fun favouritesFilterOnlyWhenAsked() = runTest {
        server.enqueue(MockResponse().setBody("[]"))
        server.enqueue(MockResponse().setBody("[]"))
        api.liveChannels()
        api.liveChannels(favorites = true)
        assertEquals("/api/v1/livetv/channels", server.takeRequest().path)
        assertEquals("/api/v1/livetv/channels?favorites=true", server.takeRequest().path)
    }

    @Test fun playSendsTheDeviceProfile() = runTest {
        server.enqueue(MockResponse().setBody("""{"channelId":"c1","url":"/stream/hls/abc/index.m3u8?exp=1&sig=s","method":"remux","timeshiftSeconds":3600}"""))
        val pb = api.playChannel("c1", DeviceProfile(listOf("mp4"), listOf("h264"), listOf("aac"), hls = true))
        assertEquals(3600, pb.timeshiftSeconds)
        assertEquals("remux", pb.method)
        val r = server.takeRequest()
        assertEquals("/api/v1/livetv/channels/c1/play", r.path)
        val body = r.body.readUtf8()
        assertTrue(body, body.startsWith("""{"device":{"containers":["mp4"],"videoCodecs":["h264"],"audioCodecs":["aac"],"hls":true"""))
    }

    @Test fun recordingAndSeriesBodies() = runTest {
        server.enqueue(MockResponse().setResponseCode(201).setBody("""{"id":"r","title":"T","start":"2026-11-11T20:00:00Z","end":"2026-11-11T21:00:00Z","status":"scheduled","prePaddingSeconds":60,"postPaddingSeconds":300,"priority":0,"createdAt":"x"}"""))
        assertEquals("scheduled", api.recordProgram("p1", "c1").status)
        val rec = server.takeRequest()
        assertEquals("/api/v1/livetv/recordings", rec.path)
        assertEquals("""{"programId":"p1","channelId":"c1"}""", rec.body.readUtf8())

        server.enqueue(MockResponse().setResponseCode(201).setBody("""{"id":"s","title":"T","newOnly":true,"priority":0,"keepLast":0,"upcoming":4,"createdAt":"x"}"""))
        assertEquals(4, api.recordSeries("p1", "c1").upcoming)
        val rule = server.takeRequest()
        assertEquals("/api/v1/livetv/series-rules", rule.path)
        assertEquals("""{"programId":"p1","channelId":"c1","newOnly":true}""", rule.body.readUtf8())
    }

    @Test fun guideAsksForTwelveHours() = runTest {
        server.enqueue(MockResponse().setBody("""{"start":"2026-11-11T20:00:00Z","end":"2026-11-12T08:00:00Z","rows":[]}"""))
        assertTrue(api.liveGuide(12).rows.isEmpty())
        assertEquals("/api/v1/livetv/guide?hours=12", server.takeRequest().path)
    }

    @Test fun busyTunersSurfaceTheCode() = runTest {
        server.enqueue(MockResponse().setResponseCode(503).setBody("""{"code":"tuners_busy","message":"all tuners are in use"}"""))
        val e = runCatching { api.playChannel("c1", DeviceProfile(emptyList(), emptyList(), emptyList(), hls = true)) }.exceptionOrNull()
        assertEquals("tuners_busy", (e as net.scyde.streamero.api.ApiException).code)
    }
}
