package net.scyde.streamero.api

import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class ApiTest {
    private val server = MockWebServer()
    private lateinit var api: Api

    @Before fun start() { server.start(); api = Api(server.url("/").toString(), "tok") }
    @After fun stop() = server.shutdown()

    @Test fun sendsTheBearerToken() = runTest {
        server.enqueue(MockResponse().setBody("""{"configured":false,"offers":[]}"""))
        assertFalse(api.searchSubtitles("abc").configured)
        val r = server.takeRequest()
        assertEquals("/api/v1/items/abc/subtitles/search", r.path)
        assertEquals("Bearer tok", r.getHeader("Authorization"))
    }

    @Test fun downloadSendsRequiredFieldsEvenWhenFalse() = runTest {
        server.enqueue(MockResponse().setResponseCode(204))
        api.downloadSubtitle("abc", SubtitleOffer("os", "hr", forced = false, hearingImpaired = false, score = 0.0, subtitle = "h", originalFormat = false))
        val body = server.takeRequest().body.readUtf8()
        listOf("forced", "hearingImpaired", "score", "originalFormat").forEach { assertTrue("$it missing in $body", "\"$it\"" in body) }
    }

    @Test fun savingKeepsBazarrAndOmitsEmptyKeys() = runTest {
        server.enqueue(MockResponse().setBody("""{"seerrConfigured":false,"usersCanRequest":true,"radarrConfigured":false,"sonarrConfigured":false,"bazarrUrl":"http://b","bazarrConfigured":true}"""))
        val saved = api.saveRequestSettings(RequestSettingsInput(true, bazarrUrl = "http://b"))
        assertTrue(saved.bazarrConfigured)
        val body = server.takeRequest().body.readUtf8()
        assertTrue(body, "\"bazarrUrl\":\"http://b\"" in body)
        assertFalse(body, "bazarrApiKey" in body)
    }

    @Test fun notInASyncGroupIsNull() = runTest {
        server.enqueue(MockResponse().setResponseCode(204))
        assertEquals(null, api.mySyncGroup())
        assertEquals("/api/v1/syncplay/me", server.takeRequest().path)
    }

    @Test fun joinsAndCreatesSyncGroups() = runTest {
        val g = """{"id":"g1","name":"n","state":"waiting","queue":[{"id":"e","itemId":"i","title":"t"}],"index":0,"positionMs":0,"anchorAt":0,"speed":1,"version":1,"members":[],"serverTime":1,"createdAt":"x","selfId":"m"}"""
        server.enqueue(MockResponse().setBody(g))
        assertEquals("i", api.joinSyncGroup("g1").current?.itemId)
        val join = server.takeRequest()
        assertEquals("POST", join.method)
        assertEquals("/api/v1/syncplay/groups/g1/join", join.path)
        server.enqueue(MockResponse().setBody(g))
        api.createSyncGroup(CreateSyncGroup(itemIds = listOf("i"), index = 0))
        assertEquals("""{"itemIds":["i"],"index":0}""", server.takeRequest().body.readUtf8())
    }

    @Test fun syncSocketSendsTheTokenAndNoOrigin() {
        server.enqueue(MockResponse().withWebSocketUpgrade(object : okhttp3.WebSocketListener() {}))
        val ws = api.syncSocket(object : okhttp3.WebSocketListener() {})
        val r = server.takeRequest()
        assertEquals("/api/v1/syncplay/socket", r.path)
        assertEquals("Bearer tok", r.getHeader("Authorization"))
        assertEquals(null, r.getHeader("Origin"))
        ws.cancel()
    }

    @Test fun aRejectedSessionIsReported() = runTest {
        var calls = 0
        api.onUnauthorized = { calls++ }
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"code":"invalid_pin","message":"wrong"}"""))
        runCatching { api.selectProfile("p", "0000") }
        assertEquals("a wrong PIN is not a lost session", 0, calls)
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"code":"unauthenticated","message":"not signed in"}"""))
        val e = runCatching { api.me() }.exceptionOrNull() as ApiException
        assertEquals("unauthenticated", e.code)
        assertEquals(1, calls)
    }

    @Test fun playbackRequestCarriesTheSubtitleChoice() = runTest {
        server.enqueue(MockResponse().setBody("""{"fileId":"f","method":"transcode","url":"/s.m3u8","startMs":0,"audio":[],"subtitles":[],
            "bitmapSubtitles":[{"index":2,"codec":"hdmv_pgs_subtitle","language":"hr"}],"burnedSubtitle":2}"""))
        val pi = api.playback("i", PlaybackRequest(net.scyde.streamero.player.SAFE_PROFILE, subtitleIndex = 2))
        assertEquals(2, pi.burnedSubtitle)
        assertEquals("hdmv_pgs_subtitle", pi.bitmapSubtitles.single().codec)
        val body = server.takeRequest().body.readUtf8()
        assertTrue(body, "\"subtitleIndex\":2" in body)
        assertFalse(body, "burnSubtitle" in body)
        assertTrue(body, "\"videoCodecs\":[\"h264\"]" in body)
    }

    @Test fun inviteLinkPointsAtTheServer() {
        assertEquals(server.url("/").toString().trimEnd('/') + "/together/abc", api.inviteLink("abc"))
    }

    @Test fun ignoresFieldsTheAppDoesNotKnow() {
        val s = json.decodeFromString(SubtitleSearch.serializer(), """{"configured":true,"future":1,"offers":[]}""")
        assertTrue(s.configured)
    }
}
