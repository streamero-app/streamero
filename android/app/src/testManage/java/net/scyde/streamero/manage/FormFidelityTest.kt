package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import net.scyde.streamero.api.json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

/** What the schema form and the generic editor send back. */
class FormFidelityTest {
    private val provider = json.parseToJsonElement("""
        {"id":4,"name":"Box","implementation":"X","futureFlag":{"a":[1,null]},"nullable":null,
         "fields":[
          {"order":0,"name":"port","label":"Port","value":8080,"type":"number"},
          {"order":1,"name":"timeout","label":"Timeout","value":"30","type":"number"},
          {"order":2,"name":"bigId","label":"Big","value":9007199254740993,"type":"number"},
          {"order":3,"name":"ratio","label":"Ratio","value":1.0,"type":"number","isFloat":true},
          {"order":4,"name":"authToken","label":"Token","value":"********","type":"textbox"},
          {"order":5,"name":"noValue","label":"No value","type":"textbox"},
          {"order":6,"name":"weird","label":"Weird","value":{"x":1},"type":"somethingNew"}
         ]}""") as JsonObject

    private fun field(o: JsonObject, name: String) = o.objs("fields").first { it.s("name") == name }

    @Test fun untouchedFieldsGoBackByteForByte() {
        val s = SchemaFormState.of(provider).setText("port", "9090")
        val out = s.applyChecked(provider)
        assertEquals(provider.without("fields"), out.without("fields"))
        // Missing "value" stays missing; unknown types and big numbers are not re-encoded.
        assertFalse(field(out, "noValue").containsKey("value"))
        assertEquals(field(provider, "weird"), field(out, "weird"))
        assertEquals("9007199254740993", (field(out, "bigId")["value"] as JsonPrimitive).content)
        assertEquals(JsonPrimitive(9090L), field(out, "port")["value"])
    }

    @Test fun numbersKeepTheirShape() {
        var s = SchemaFormState.of(provider).setText("bigId", "9007199254740995").setText("timeout", "45").setText("ratio", "2")
        val out = s.applyChecked(provider)
        assertEquals("a long is not routed through a double", "9007199254740995", (field(out, "bigId")["value"] as JsonPrimitive).content)
        val timeout = field(out, "timeout")["value"] as JsonPrimitive
        assertTrue("a number the service sent as text goes back as text", timeout.isString)
        assertEquals("45", timeout.content)
        assertEquals(2.0, (field(out, "ratio")["value"] as JsonPrimitive).content.toDouble(), 0.0)
        s = s.setText("port", "")
        assertEquals(JsonNull, field(s.applyChecked(provider), "port")["value"])
    }

    @Test fun textInANumberFieldIsRefusedWithItsLabel() {
        val s = SchemaFormState.of(provider).setText("port", "80a")
        assertEquals(listOf("Port"), s.invalidNumbers())
        try { s.applyChecked(provider); fail("expected InvalidNumber") } catch (e: InvalidNumber) { assertEquals(listOf("Port"), e.fields) }
    }

    @Test fun secretsWithoutPrivacyAreStillWriteOnly() {
        val s = SchemaFormState.of(provider)
        val token = s.fields.first { it.name == "authToken" }
        assertTrue(token.secret)
        assertEquals("", s.text(token))
        assertEquals("********", field(s.setText("authToken", "").applyTo(provider), "authToken").s("value"))
        assertEquals("new", field(s.setText("authToken", "new").applyTo(provider), "authToken").s("value"))
    }

    @Test fun genericEditorRefusesTextInNumbers() {
        val original = json.parseToJsonElement("""{"port":7878,"name":"a","nested":{"retries":3},"list":[{"n":1},{"n":2}],"ids":[1,2],"flag":true,"none":null}""") as JsonObject
        val ok = original.with("port", "8080").with("none", "x")
        assertEquals(JsonPrimitive(8080L), (normalizeForSave(ok, original) as JsonObject)["port"])
        val bad = original.with("port", "80 80").with("nested", JsonObject(mapOf("retries" to JsonPrimitive("three"))))
            .with("list", JsonArray(listOf(jsonOf("n" to "x"), jsonOf("n" to 2), jsonOf("n" to "y"))))
        try {
            normalizeForSave(bad, original); fail("expected InvalidNumber")
        } catch (e: InvalidNumber) {
            assertEquals(listOf("Port", "Retries", "N"), e.fields)
        }
        assertTrue(invalidNumbers(original.with("flag", "true"), original).isEmpty())
        assertEquals(listOf("Ids"), invalidNumbers(original.with("ids", JsonArray(listOf(JsonPrimitive("1"), JsonPrimitive("two")))), original))
    }

    @Test fun duplicatedItemsDoNotCarryStoredSecrets() {
        val doc = json.parseToJsonElement("""{"servers":[{"id":1,"name":"a","apiKey":"real-key","port":7878,"passwordRequired":true,"auth":{"token":"t"}}]}""") as JsonObject
        val out = JsonTree.duplicate(doc, listOf("servers"), 0) as JsonObject
        val copy = out.objs("servers")[1]
        assertFalse(copy.containsKey("id"))
        assertEquals("", copy.s("apiKey"))
        assertEquals("", copy.o("auth")!!.s("token"))
        assertEquals("a non-text value is not blanked", true, copy.b("passwordRequired"))
        assertEquals(7878, copy.i("port"))
        // Saving sends the empty secret for the copy, never the original's key.
        val saved = normalizeForSave(out, doc) as JsonObject
        assertEquals("", saved.objs("servers")[1].s("apiKey"))
        assertEquals("real-key", saved.objs("servers")[0].s("apiKey"))
    }

    @Test fun secretDocumentsAreRecognised() {
        assertTrue(hasSecrets(json.parseToJsonElement("""{"main":{"apiKey":"abc"}}""")))
        assertTrue(hasSecrets(json.parseToJsonElement("""{"list":[{"password":"x"}]}""")))
        assertFalse(hasSecrets(json.parseToJsonElement("""{"apiKey":"","enableTokenAuth":"yes","port":1}""")))
        assertFalse(hasSecrets(null))
    }
}
