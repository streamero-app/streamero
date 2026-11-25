package net.scyde.streamero.api

import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class LibraryAccessTest {
    private val server = MockWebServer()
    private lateinit var api: Api
    private val libs = listOf(
        Library("m", "Movies", "movies"), Library("x", "Private", "adult"), Library("s", "Shows", "shows"),
    )

    @Before fun start() { server.start(); api = Api(server.url("/").toString(), "tok") }
    @After fun stop() = server.shutdown()

    private fun body(): JsonObject = Json.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject

    @Test fun olderServerWithoutTheFieldsMeansAllLibraries() = runTest {
        server.enqueue(MockResponse().setBody("""[{"id":"u","username":"ana","isAdmin":false,"disabled":false,"totpEnabled":false,"createdAt":"x"}]"""))
        val u = api.users().single()
        assertEquals(emptyList<String>(), u.libraryIds)
        assertFalse(u.newLibraries)
        assertEquals(LibraryAccess(all = true), LibraryAccess.of(u))
    }

    @Test fun decodesChosenList() = runTest {
        server.enqueue(MockResponse().setBody("""[{"id":"u","username":"ana","displayName":"Ana","permissions":{"allLibraries":false},
            "libraryIds":["m","s"],"newLibraries":true,"createdAt":"x"}]"""))
        val u = api.users().single()
        assertEquals("Ana", u.displayName)
        assertEquals(LibraryAccess(all = false, ids = setOf("m", "s"), newLibraries = true), LibraryAccess.of(u))
    }

    @Test fun pickerLeavesOutAdultLibraries() {
        assertEquals(listOf("m", "s"), pickableLibraries(libs).map { it.id })
    }

    @Test fun savingAChosenListPatchesPermissionsAndIds() = runTest {
        val perms = UserPermissions(download = true, maxSessions = 2)
        // "x" is adult and "gone" no longer exists: neither is sent.
        val upd = LibraryAccess(all = false, ids = setOf("s", "x", "gone", "m"), newLibraries = true).toUpdate(perms, libs)
        server.enqueue(MockResponse().setBody("""{"id":"u","username":"ana"}"""))
        api.updateUser("u", upd)
        val req = server.takeRequest()
        assertEquals("PATCH", req.method)
        assertEquals("/api/v1/users/u", req.path)
        val b = Json.parseToJsonElement(req.body.readUtf8()).jsonObject
        val p = b["permissions"]!!.jsonObject
        assertFalse(p["allLibraries"]!!.jsonPrimitive.boolean)
        assertTrue(p["download"]!!.jsonPrimitive.boolean)
        assertEquals("2", p["maxSessions"]!!.jsonPrimitive.content)
        assertEquals(listOf("m", "s"), b["libraryIds"]!!.jsonArray.map { it.jsonPrimitive.content })
        assertTrue(b["newLibraries"]!!.jsonPrimitive.boolean)
        assertNull(b["isAdmin"])
    }

    @Test fun savingAllLibrariesOnlyFlipsThePermission() {
        val upd = LibraryAccess(all = true, ids = setOf("m")).toUpdate(UserPermissions(allLibraries = false), libs)
        val b = Json.parseToJsonElement(json.encodeToString(upd)).jsonObject
        assertTrue(b["permissions"]!!.jsonObject["allLibraries"]!!.jsonPrimitive.boolean)
        assertNull(b["libraryIds"])
        assertNull(b["newLibraries"])
    }

    @Test fun inviteBodies() = runTest {
        server.enqueue(MockResponse().setBody("""{"token":"t","invite":{"id":"i"}}"""))
        api.createInvite(LibraryAccess().toInvite(72, 1, libs))
        val plain = body()
        assertEquals(setOf("expiresInHours", "maxUses"), plain.keys)

        server.enqueue(MockResponse().setBody("""{"token":"t","invite":{"id":"i","libraryIds":["m"],"newLibraries":false}}"""))
        val created = api.createInvite(LibraryAccess(all = false, ids = setOf("m", "x")).toInvite(72, 1, libs))
        assertEquals(listOf("m"), created.invite.libraryIds)
        val chosen = body()
        val p = chosen["permissions"]!!.jsonObject
        assertFalse(p["allLibraries"]!!.jsonPrimitive.boolean)
        // The rest matches what the server gives a new account.
        assertTrue(p["download"]!!.jsonPrimitive.boolean)
        assertTrue(p["liveTv"]!!.jsonPrimitive.boolean)
        assertEquals(listOf("m"), chosen["libraryIds"]!!.jsonArray.map { it.jsonPrimitive.content })
        assertFalse(chosen["newLibraries"]!!.jsonPrimitive.boolean)
    }
}
