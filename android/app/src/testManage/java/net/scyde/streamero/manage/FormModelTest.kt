package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import net.scyde.streamero.api.json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class FormModelTest {
    // A download client as the *arr provider APIs describe it (shape from the public API docs).
    private val client = json.parseToJsonElement("""
        {"id":3,"name":"qBit","enable":true,"implementation":"QBittorrent","configContract":"QBittorrentSettings","tags":[],
         "fields":[
          {"order":0,"name":"host","label":"Host","value":"localhost","type":"textbox","advanced":false,"privacy":"normal"},
          {"order":1,"name":"port","label":"Port","value":8080,"type":"number","advanced":false},
          {"order":2,"name":"useSsl","label":"Use SSL","value":false,"type":"checkbox","helpText":"Use a secure connection"},
          {"order":3,"name":"password","label":"Password","value":"********","type":"password","privacy":"password"},
          {"order":4,"name":"apiKey","label":"API Key","value":"********","type":"textbox","privacy":"apiKey"},
          {"order":5,"name":"movieCategory","label":"Category","value":"radarr","type":"textbox","advanced":true},
          {"order":6,"name":"initialState","label":"Initial State","value":0,"type":"select","selectOptions":[{"value":0,"name":"Start"},{"value":1,"name":"Pause"}]},
          {"order":7,"name":"categories","label":"Categories","value":[2000,2010],"type":"select","selectOptions":[{"value":2000,"name":"Movies"},{"value":2010,"name":"Movies/Foreign"},{"value":2040,"name":"Movies/HD"}]},
          {"order":8,"name":"extraTags","label":"Tags","value":["a"],"type":"tag"},
          {"order":9,"name":"seedCriteria.seedRatio","label":"Seed Ratio","value":1.5,"type":"number","isFloat":true,"unit":"ratio"},
          {"order":10,"name":"oauth","label":"Sign in","value":null,"type":"oAuth"},
          {"order":11,"name":"secretThing","label":"x","value":"y","type":"textbox","hidden":"hidden"},
          {"order":12,"name":"notice","label":"Note","type":"info","helpText":"Read this"}
         ]}
    """).let { it as JsonObject }

    private fun field(s: SchemaFormState, n: String) = s.fields.first { it.name == n }

    @Test fun parsesFieldTypes() {
        val s = SchemaFormState.of(client)
        assertEquals(FieldKind.Text, field(s, "host").kind)
        assertEquals(FieldKind.Number, field(s, "port").kind)
        assertEquals(FieldKind.Checkbox, field(s, "useSsl").kind)
        assertEquals("Use a secure connection", field(s, "useSsl").helpText)
        assertEquals(FieldKind.Password, field(s, "password").kind)
        assertTrue(field(s, "password").secret)
        assertTrue("privacy apiKey is write-only", field(s, "apiKey").secret)
        assertTrue(field(s, "movieCategory").advanced)
        assertEquals(FieldKind.Select, field(s, "initialState").kind)
        assertEquals(listOf("Start", "Pause"), field(s, "initialState").options.map { it.name })
        assertEquals("a select with a list value is a multi-select", FieldKind.MultiSelect, field(s, "categories").kind)
        assertEquals(FieldKind.Tag, field(s, "extraTags").kind)
        assertTrue(field(s, "seedCriteria.seedRatio").isFloat)
        assertEquals("ratio", field(s, "seedCriteria.seedRatio").unit)
        assertEquals("unknown types are kept, shown read-only", FieldKind.Unknown, field(s, "oauth").kind)
        assertEquals(FieldKind.Hidden, field(s, "secretThing").kind)
        assertEquals(FieldKind.Info, field(s, "notice").kind)
        assertEquals((0..12).toList(), s.fields.map { it.order })
    }

    @Test fun missingLabelFallsBackToTheName() {
        val f = SchemaField.parse(JsonObject(mapOf("name" to JsonPrimitive("seedCriteria.seedTime"), "type" to JsonPrimitive("number"))))!!
        assertEquals("Seed time", f.label)
    }

    @Test fun untouchedFormRoundTripsExactly() {
        val s = SchemaFormState.of(client)
        assertFalse(s.changed)
        assertEquals(client, s.applyTo(client))
    }

    @Test fun editsRoundTripWithTypes() {
        var s = SchemaFormState.of(client)
        s = s.setText("host", "qbit.lan").setText("port", "9091").setText("seedCriteria.seedRatio", "2.25")
            .set("useSsl", JsonPrimitive(true)).set("initialState", JsonPrimitive(1))
            .set("categories", JsonArray(listOf(JsonPrimitive(2040)))).setText("extraTags", "x, y ,,z")
        val out = s.applyTo(client)
        val fields = out.objs("fields").associateBy { it.s("name") }
        assertEquals("qbit.lan", fields["host"]!!.s("value"))
        assertEquals(9091L, fields["port"]!!.l("value"))
        assertEquals(2.25, fields["seedCriteria.seedRatio"]!!.d("value")!!, 0.0)
        assertEquals(true, fields["useSsl"]!!.b("value"))
        assertEquals(1, fields["initialState"]!!.i("value"))
        assertEquals(listOf(2040), fields["categories"]!!.ints("value"))
        assertEquals(listOf("x", "y", "z"), fields["extraTags"]!!.strs("value"))
        // Everything else of the field and of the resource stays as the service sent it.
        assertEquals("Port", fields["port"]!!.s("label"))
        assertEquals("QBittorrentSettings", out.s("configContract"))
        assertEquals(client.objs("fields").size, out.objs("fields").size)
    }

    @Test fun secretsAreWriteOnly() {
        var s = SchemaFormState.of(client)
        assertEquals("a secret shows empty, never the stored value", "", s.text(field(s, "password")))
        // Not touched: the masked value goes back, which the service reads as "unchanged".
        assertEquals("********", s.applyTo(client).objs("fields").first { it.s("name") == "password" }.s("value"))
        // Typed and then cleared: still unchanged.
        s = s.setText("password", "abc").setText("password", "")
        assertEquals("********", s.applyTo(client).objs("fields").first { it.s("name") == "password" }.s("value"))
        // A new value is sent.
        s = s.setText("apiKey", "new-key")
        assertEquals("new-key", s.applyTo(client).objs("fields").first { it.s("name") == "apiKey" }.s("value"))
    }

    @Test fun invalidNumbersAreLeftForTheServiceToReject() {
        val s = SchemaFormState.of(client).setText("port", "80a")
        assertEquals("80a", s.applyTo(client).objs("fields").first { it.s("name") == "port" }.s("value"))
    }

    @Test fun numericTagsStayNumbers() {
        val o = json.parseToJsonElement("""{"fields":[{"name":"categories","value":[5000],"type":"tag"}]}""") as JsonObject
        val s = SchemaFormState.of(o).setText("categories", "5000, 5040")
        assertEquals(listOf(5000, 5040), s.applyTo(o).objs("fields").single().ints("value"))
    }

    @Test fun genericSettingsKeepSecretsAndNumbers() {
        val original = json.parseToJsonElement("""{"id":1,"port":7878,"apiKey":"abc","password":"********","bindAddress":"*","nested":{"retries":3,"token":"t"},"list":[1,2]}""") as JsonObject
        val edited = original.with("port", "8080").with("apiKey", "").with("bindAddress", "0.0.0.0")
            .with("nested", original.o("nested")!!.with("retries", "5")).with("list", listOf("1", "2", "3"))
        val out = normalizeGeneric(edited, original) as JsonObject
        assertEquals(8080L, out.l("port"))
        assertEquals("abc", out.s("apiKey"))
        assertEquals("********", out.s("password"))
        assertEquals("0.0.0.0", out.s("bindAddress"))
        assertEquals(5L, out.o("nested")!!.l("retries"))
        assertEquals("t", out.o("nested")!!.s("token"))
        assertEquals(listOf(1, 2, 3), out.ints("list"))
        assertTrue(isSecretKey("proxyPassword"))
        assertFalse(isSecretKey("enableApiKeyAuth"))
    }

    @Test fun bazarrSendsOnlyChangedKeysAsFormFields() {
        val original = json.parseToJsonElement("""{"general":{"ip":"0.0.0.0","port":6767,"enabled_providers":["opensubtitlescom"],"use_sonarr":true},"opensubtitlescom":{"username":"u","password":"pw"}}""") as JsonObject
        val edited = original.with("general", original.o("general")!!.with("port", "6868").with("enabled_providers", listOf("opensubtitlescom", "podnapisi")).with("use_sonarr", false))
            .with("opensubtitlescom", original.o("opensubtitlescom")!!.with("password", ""))
        val form = bazarrSettingsForm(original, edited)
        assertEquals(listOf(
            "settings-general-port" to "6868",
            "settings-general-enabled_providers" to "opensubtitlescom",
            "settings-general-enabled_providers" to "podnapisi",
            "settings-general-use_sonarr" to "false",
        ), form)
    }

    @Test fun humanizesKeys() {
        assertEquals("Enable color impaired mode", humanize("enableColorImpairedMode"))
        assertEquals("Use SSL", humanize("useSSL"))
        assertEquals("Enabled providers", humanize("enabled_providers"))
        assertEquals("1.5 GB", bytes(1610612736))
    }
}
