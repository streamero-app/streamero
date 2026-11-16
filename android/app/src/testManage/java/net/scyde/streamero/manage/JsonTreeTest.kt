package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import net.scyde.streamero.api.json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class JsonTreeTest {
    // A Lidarr-style metadata profile (shape from the public API docs) plus a key the app does not know.
    private val profile = json.parseToJsonElement("""
        {"id":4,"name":"Standard","futureKey":{"x":1},
         "primaryAlbumTypes":[{"id":0,"albumType":{"id":0,"name":"Album"},"allowed":true},{"id":1,"albumType":{"id":1,"name":"EP"},"allowed":false}],
         "releaseStatuses":[{"id":0,"releaseStatus":{"id":0,"name":"Official"},"allowed":true}],
         "tags":[1,2],"required":["x264"],"ratios":[1.5],"flags":[true]}
    """) as JsonObject

    private val albums = listOf("primaryAlbumTypes")

    @Test fun addsAnItemShapedLikeItsSiblings() {
        val out = JsonTree.add(profile, albums) as JsonObject
        val items = out.objs("primaryAlbumTypes")
        assertEquals(3, items.size)
        val added = items.last()
        assertNull(added["id"])
        assertEquals(setOf("albumType", "allowed"), added.keys)
        assertEquals(JsonPrimitive(false), added["allowed"])
        assertEquals("", added.o("albumType")!!.s("name"))
        assertFalse(added.o("albumType")!!.containsKey("id"))
        // Everything else untouched, unknown keys included.
        assertEquals(profile.without("primaryAlbumTypes"), out.without("primaryAlbumTypes"))
        assertEquals(profile.objs("primaryAlbumTypes"), items.take(2))
    }

    @Test fun blankValuesKeepTheirType() {
        assertEquals(JsonPrimitive(""), JsonTree.blankLike(JsonPrimitive("abc")))
        assertEquals(JsonPrimitive(0), JsonTree.blankLike(JsonPrimitive(42)))
        assertEquals(JsonPrimitive(0.0), JsonTree.blankLike(JsonPrimitive(1.5)))
        assertEquals(JsonPrimitive(false), JsonTree.blankLike(JsonPrimitive(true)))
        assertEquals(JsonNull, JsonTree.blankLike(JsonNull))
        assertEquals(JsonArray(emptyList()), JsonTree.blankLike(JsonArray(listOf(JsonPrimitive(1)))))
        assertEquals(JsonPrimitive(""), JsonTree.newItem(JsonArray(emptyList())))
        assertEquals(JsonPrimitive(0), JsonTree.newItem(profile.a("tags")!!))
    }

    @Test fun removesWithoutTouchingTheRest() {
        val out = JsonTree.remove(profile, albums, 0) as JsonObject
        assertEquals(listOf("EP"), out.objs("primaryAlbumTypes").map { it.o("albumType")!!.s("name") })
        assertEquals(profile.a("releaseStatuses"), out.a("releaseStatuses"))
        assertEquals(profile, JsonTree.remove(profile, albums, 9))
    }

    @Test fun movesUpAndDown() {
        val down = JsonTree.move(profile, albums, 0, 1) as JsonObject
        assertEquals(listOf("EP", "Album"), down.objs("primaryAlbumTypes").map { it.o("albumType")!!.s("name") })
        val back = JsonTree.move(down, albums, 1, 0)
        assertEquals(profile, back)
        assertEquals(profile, JsonTree.move(profile, albums, 0, -1))
        assertEquals(profile, JsonTree.move(profile, albums, 1, 2))
    }

    @Test fun duplicatesWithoutTheId() {
        val out = JsonTree.duplicate(profile, albums, 0) as JsonObject
        val items = out.objs("primaryAlbumTypes")
        assertEquals(3, items.size)
        assertEquals(items[0].without("id"), items[1])
        assertEquals("EP", items[2].o("albumType")!!.s("name"))
    }

    @Test fun worksOnNestedPathsAndMissingArrays() {
        val doc = json.parseToJsonElement("""{"a":{"list":[{"k":1}]}}""") as JsonObject
        val out = JsonTree.add(doc, listOf("a", "list")) as JsonObject
        assertEquals(listOf(1, 0), out.o("a")!!.objs("list").map { it.i("k") })
        val created = JsonTree.insert(doc, listOf("a", "new"), JsonPrimitive("x")) as JsonObject
        assertEquals(listOf("x"), created.o("a")!!.strs("new"))
        val first = JsonTree.insert(doc, listOf("a", "list"), jsonOf("k" to 9), 0) as JsonObject
        assertEquals(listOf(9, 1), first.o("a")!!.objs("list").map { it.i("k") })
    }

    @Test fun primitivesTypedForTheirArray() {
        assertEquals(JsonPrimitive(3L), JsonTree.primitiveFor(profile.a("tags")!!, " 3 "))
        assertEquals(JsonPrimitive("abc"), JsonTree.primitiveFor(profile.a("tags")!!, "abc"))
        assertEquals(JsonPrimitive("1080"), JsonTree.primitiveFor(profile.a("required")!!, "1080"))
        assertEquals(JsonPrimitive(2.5), JsonTree.primitiveFor(profile.a("ratios")!!, "2.5"))
        assertEquals(JsonPrimitive(false), JsonTree.primitiveFor(profile.a("flags")!!, "false"))
        assertEquals(JsonPrimitive("x"), JsonTree.primitiveFor(JsonArray(emptyList()), "x"))
        assertTrue(JsonTree.isObjectArray(profile.a("primaryAlbumTypes")!!))
        assertFalse(JsonTree.isObjectArray(JsonArray(emptyList())))
    }

    @Test fun normalizeMatchesItemsByIdAfterRemoveAndReorder() {
        val original = json.parseToJsonElement("""{"servers":[{"id":1,"port":7878,"apiKey":"k1"},{"id":2,"port":8989,"apiKey":"k2"}]}""") as JsonObject
        // Reordered, and the user typed then cleared the second one's key: its own stored key comes back.
        var edited = JsonTree.move(original, listOf("servers"), 1, 0) as JsonObject
        edited = edited.setAt(listOf("servers", 0, "apiKey"), JsonPrimitive("")).setAt(listOf("servers", 0, "port"), JsonPrimitive("9000")) as JsonObject
        val out = normalizeGeneric(edited, original) as JsonObject
        assertEquals(listOf(2, 1), out.objs("servers").map { it.i("id") })
        assertEquals("k2", out.objs("servers")[0].s("apiKey"))
        assertEquals(9000L, out.objs("servers")[0].l("port"))
    }

    @Test fun normalizeTypesNewItemsFromASiblingWithoutCopyingSecrets() {
        val original = json.parseToJsonElement("""{"maps":[{"host":"a","port":1,"password":"pw"}]}""") as JsonObject
        var edited = JsonTree.add(original, listOf("maps")) as JsonObject
        edited = edited.setAt(listOf("maps", 1, "port"), JsonPrimitive("42")) as JsonObject
        val out = normalizeGeneric(edited, original) as JsonObject
        val added = out.objs("maps")[1]
        assertEquals(42L, added.l("port"))
        assertEquals("", added.s("password"))
        assertEquals("pw", out.objs("maps")[0].s("password"))
    }

    @Test fun normalizeKeepsNewKeysAndPrimitiveLists() {
        val original = json.parseToJsonElement("""{"tags":[1],"words":[]}""") as JsonObject
        val edited = original.with("tags", listOf(1, 2)).with("words", listOf("a")).with("extra", "v")
        val out = normalizeGeneric(edited, original) as JsonObject
        assertEquals(listOf(1, 2), out.ints("tags"))
        assertEquals(listOf("a"), out.strs("words"))
        assertEquals("v", out.s("extra"))
    }
}
