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

class AdultApiTest {
    private val server = MockWebServer()
    private lateinit var api: Api

    @Before fun start() { server.start(); api = Api(server.url("/").toString(), "tok") }
    @After fun stop() = server.shutdown()

    @Test fun meCarriesAdultAccess() = runTest {
        server.enqueue(MockResponse().setBody("""{"id":"u","username":"a","adultAccess":true}"""))
        assertTrue(api.me().adultAccess)
        server.enqueue(MockResponse().setBody("""{"id":"u","username":"a"}"""))
        assertFalse(api.me().adultAccess)
    }

    @Test fun parsesStatusAndScenes() = runTest {
        server.enqueue(MockResponse().setBody("""{"pinSet":true,"unlocked":false,"blur":true,"metadataConfigured":false,"libraries":[{"id":"l","name":"X","sceneCount":3}]}"""))
        val s = api.adultStatus()
        assertTrue(s.pinSet); assertFalse(s.unlocked); assertEquals(3, s.libraries.single().sceneCount)
        assertEquals("/api/v1/adult/status", server.takeRequest().path)

        server.enqueue(MockResponse().setBody("""{"items":[{"id":"s1","libraryId":"l","kind":"scene","title":"T","date":"2024-01-02","studio":{"id":"st","name":"S"},
            "performers":[{"id":"p","name":"P"}],"tags":["a"],"durationMs":1000,"hasPoster":true,"hasBackdrop":false,"positionMs":500,"played":false,"addedAt":"2024-01-01T00:00:00Z"}],"total":1}"""))
        val page = api.adultScenes(mapOf("q" to "x", "kind" to null, "offset" to 0, "limit" to 60))
        assertEquals(1, page.total)
        assertEquals("S", page.items.single().studio?.name)
        assertEquals(0.5f, page.items.single().progress, 0.001f)
        assertEquals("/api/v1/adult/scenes?q=x&offset=0&limit=60", server.takeRequest().path)
    }

    @Test fun parsesSceneDetail() = runTest {
        server.enqueue(MockResponse().setBody("""{"id":"s1","libraryId":"l","kind":"movie","title":"T","performers":[],"tags":[],"hasPoster":false,"hasBackdrop":true,
            "positionMs":0,"played":true,"addedAt":"x","description":"D","files":[{"id":"f","name":"a.mp4","size":1,"container":"mp4","height":1080}],"providerIds":{"tpdb":"1"}}"""))
        val d = api.adultScene("s1")
        assertEquals("D", d.description)
        assertEquals(1080, d.files.single().height)
        assertEquals("1", d.providerIds["tpdb"])
    }

    @Test fun unlockPlaybackAndProgressUseTheAdultEndpoints() = runTest {
        server.enqueue(MockResponse().setResponseCode(204))
        api.adultUnlock("1234")
        val unlock = server.takeRequest()
        assertEquals("/api/v1/adult/unlock", unlock.path)
        assertEquals("""{"pin":"1234"}""", unlock.body.readUtf8())

        server.enqueue(MockResponse().setBody("""{"fileId":"f","method":"direct","url":"/stream/adult/x"}"""))
        val pi = api.adultPlayback("s1", PlaybackRequest(DeviceProfile(listOf("mp4"), listOf("h264"), listOf("aac"), hls = true)))
        assertEquals("/stream/adult/x", pi.url)
        assertEquals("/api/v1/adult/scenes/s1/playback", server.takeRequest().path)

        server.enqueue(MockResponse().setResponseCode(204))
        api.adultProgress("s1", ProgressReport(1000, 2000))
        val p = server.takeRequest()
        assertEquals("PUT", p.method)
        assertEquals("/api/v1/adult/scenes/s1/progress", p.path)
    }

    @Test fun preferencesLeaveOutUnsetFields() = runTest {
        val status = """{"pinSet":false,"unlocked":true,"blur":false,"metadataConfigured":false,"libraries":[]}"""
        server.enqueue(MockResponse().setBody(status))
        api.adultPreferences(AdultPreferencesInput(blur = false))
        assertEquals("""{"blur":false}""", server.takeRequest().body.readUtf8())
        server.enqueue(MockResponse().setBody(status))
        api.adultPreferences(AdultPreferencesInput(pin = "", currentPin = "1234"))
        assertEquals("""{"pin":"","currentPin":"1234"}""", server.takeRequest().body.readUtf8())
    }

    @Test fun lockedIsRecognised() = runTest {
        server.enqueue(MockResponse().setResponseCode(403).setBody("""{"code":"adult_locked","message":"locked"}"""))
        val e = runCatching { api.adultScenes(emptyMap()) }.exceptionOrNull()
        assertTrue(Api.isAdultLocked(e))
        assertFalse(Api.isAdultLocked(ApiException(403, "forbidden", "")))
    }

    @Test fun imageUrl() {
        assertTrue(api.adultImageUrl("scene", "s1", "poster").endsWith("/api/v1/adult/images/scene/s1/poster"))
    }

    @Test fun galleriesHomeTagsAndFavourites() = runTest {
        server.enqueue(MockResponse().setBody("""{"items":[{"id":"g1","title":"Set","imageCount":3,"coverImageId":"i1","tags":["Outdoor"]}],"total":1}"""))
        val page = api.adultGalleries(net.scyde.streamero.adult.AdultGalleryFilter(performerId = "p1").query(0, 60))
        assertEquals(3, page.items.single().imageCount)
        assertEquals("/api/v1/adult/galleries?performerId=p1&offset=0&limit=60", server.takeRequest().path)

        server.enqueue(MockResponse().setBody("""{"continue":[{"id":"s1","title":"A"}],"recentlyAdded":[],"recentlyReleased":[],"favoritePerformers":[],"recentGalleries":[{"id":"g1","title":"Set"}]}"""))
        val home = api.adultHome()
        assertEquals("s1", home.continueWatching.single().id)
        assertEquals("g1", home.recentGalleries.single().id)
        assertEquals("/api/v1/adult/home", server.takeRequest().path)

        server.enqueue(MockResponse().setBody("""{"items":[{"name":"Outdoor","sceneCount":2,"galleryCount":1,"hidden":false}],"total":1}"""))
        assertEquals("Outdoor", api.adultTags("out", "name").items.single().name)
        assertEquals("/api/v1/adult/tags?q=out&sort=name&limit=500", server.takeRequest().path)

        server.enqueue(MockResponse().setResponseCode(204))
        api.adultFavorite("p1", true)
        val fav = server.takeRequest()
        assertEquals("PUT", fav.method)
        assertEquals("/api/v1/adult/performers/p1/favorite", fav.path)

        server.enqueue(MockResponse().setBody("""{"id":"g1","title":"Set","images":[{"id":"i1","position":0,"name":"01.jpg","width":800,"height":600}]}"""))
        assertEquals("01.jpg", api.adultGallery("g1").images.single().name)
        assertTrue(api.adultGalleryImageUrl("i1", "thumb").endsWith("/api/v1/adult/gallery-images/i1/thumb"))
    }
}
