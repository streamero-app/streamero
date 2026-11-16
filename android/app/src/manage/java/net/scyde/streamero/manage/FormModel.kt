package net.scyde.streamero.manage

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

/**
 * How a schema field is edited. The *arr provider resources (indexers, download clients,
 * import lists, notifications, metadata, Prowlarr applications) describe their settings as
 * `fields[]` with a `type`; unknown types are kept as they are and shown read-only.
 */
enum class FieldKind { Text, TextArea, Number, Password, Checkbox, Select, MultiSelect, Tag, Path, Url, Info, Hidden, Unknown }

data class SelectOption(val value: JsonElement, val name: String, val hint: String? = null)

data class SchemaField(
    val name: String,
    val label: String,
    val kind: FieldKind,
    val type: String,
    val value: JsonElement,
    val options: List<SelectOption> = emptyList(),
    val helpText: String? = null,
    val helpLink: String? = null,
    val advanced: Boolean = false,
    /** Passwords and API keys: write-only, the service returns them masked. */
    val secret: Boolean = false,
    val unit: String? = null,
    val placeholder: String? = null,
    val isFloat: Boolean = false,
    val order: Int = 0,
) {
    companion object {
        fun parse(o: JsonObject): SchemaField? {
            val name = o.s("name") ?: return null
            val type = o.s("type").orEmpty()
            val value = o["value"] ?: JsonNull
            val privacy = o.s("privacy").orEmpty()
            val options = o.objs("selectOptions").map { opt ->
                SelectOption(opt["value"] ?: JsonNull, opt.s("name") ?: opt.s("value").orEmpty(), opt.s("hint"))
            }
            val secretByPrivacy = privacy.equals("password", true) || privacy.equals("apiKey", true)
            var kind = when (type.lowercase()) {
                "textbox", "" -> FieldKind.Text
                "textarea" -> FieldKind.TextArea
                "number" -> FieldKind.Number
                "password" -> FieldKind.Password
                "checkbox" -> FieldKind.Checkbox
                "select" -> if (value is JsonArray) FieldKind.MultiSelect else FieldKind.Select
                "tagselect" -> FieldKind.MultiSelect
                "tag" -> if (options.isNotEmpty()) FieldKind.MultiSelect else FieldKind.Tag
                "path", "filepath" -> FieldKind.Path
                "url" -> FieldKind.Url
                "info" -> FieldKind.Info
                "hidden" -> FieldKind.Hidden
                else -> FieldKind.Unknown
            }
            // A select whose options come from a provider action has none here: typed instead.
            if (kind == FieldKind.Select && options.isEmpty()) kind = if (value is JsonPrimitive && !value.isString) FieldKind.Number else FieldKind.Text
            if (kind == FieldKind.MultiSelect && options.isEmpty()) kind = FieldKind.Tag
            if (o.s("hidden").equals("hidden", true)) kind = FieldKind.Hidden
            return SchemaField(
                name = name, label = o.s("label")?.ifBlank { null } ?: humanize(name.substringAfterLast('.')), kind = kind, type = type, value = value,
                options = options, helpText = o.s("helpText")?.ifBlank { null }, helpLink = o.s("helpLink")?.ifBlank { null },
                advanced = o.b("advanced") == true, secret = kind == FieldKind.Password || secretByPrivacy,
                unit = o.s("unit")?.ifBlank { null }, placeholder = o.s("placeholder")?.ifBlank { null }, isFloat = o.b("isFloat") == true,
                order = o.i("order") ?: 0,
            )
        }

        fun parseAll(fields: JsonArray?): List<SchemaField> =
            fields.orEmpty().mapNotNull { (it as? JsonObject)?.let(::parse) }.sortedBy { it.order }
    }
}

/**
 * The edits made to a schema form. Values are read through [value]; [toFields] writes them
 * back into the resource's original `fields[]` (all other field properties untouched).
 * Secrets are write-only: an untouched or blanked secret keeps the value the service sent
 * (its mask, which it reads as "unchanged"), so a stored password is never overwritten.
 */
data class SchemaFormState(val fields: List<SchemaField>, val edits: Map<String, JsonElement> = emptyMap()) {
    fun value(name: String): JsonElement = edits[name] ?: fields.firstOrNull { it.name == name }?.value ?: JsonNull

    /** What a text box shows: secrets start empty. */
    fun text(f: SchemaField): String = when {
        f.secret -> (edits[f.name] as? JsonPrimitive)?.contentOrNull.orEmpty()
        else -> when (val v = value(f.name)) {
            is JsonPrimitive -> v.contentOrNull.orEmpty()
            is JsonArray -> v.joinToString(", ") { (it as? JsonPrimitive)?.contentOrNull ?: it.toString() }
            else -> ""
        }
    }

    fun set(name: String, v: JsonElement): SchemaFormState = copy(edits = edits + (name to v))
    fun setText(name: String, text: String): SchemaFormState = set(name, JsonPrimitive(text))

    val changed: Boolean get() = edits.isNotEmpty()

    /** The value to send for a field, coerced to the field's type. */
    fun output(f: SchemaField): JsonElement {
        val e = edits[f.name] ?: return f.value
        if (f.secret) {
            val t = (e as? JsonPrimitive)?.contentOrNull
            if (t.isNullOrEmpty()) return f.value
        }
        return coerce(f, e)
    }

    fun toFields(original: JsonArray?): JsonArray {
        val byName = fields.associateBy { it.name }
        return JsonArray(original.orEmpty().map { el ->
            val o = el as? JsonObject ?: return@map el
            val f = byName[o.s("name")] ?: return@map el
            if (!edits.containsKey(f.name)) return@map el
            JsonObject(o + ("value" to output(f)))
        })
    }

    /** The resource with its fields replaced by the edited ones. */
    fun applyTo(resource: JsonObject): JsonObject = resource.with("fields", toFields(resource.a("fields")))

    companion object {
        fun of(resource: JsonObject) = SchemaFormState(SchemaField.parseAll(resource.a("fields")))

        fun coerce(f: SchemaField, v: JsonElement): JsonElement {
            val text = (v as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull ?: return v
            return when (f.kind) {
                FieldKind.Number -> {
                    val t = text.trim()
                    when {
                        t.isEmpty() -> JsonNull
                        f.isFloat || t.contains('.') -> t.toDoubleOrNull()?.let { JsonPrimitive(it) } ?: v
                        else -> t.toLongOrNull()?.let { JsonPrimitive(it) } ?: v
                    }
                }
                FieldKind.Tag -> {
                    val parts = text.split(',').map { it.trim() }.filter { it.isNotEmpty() }
                    // Tags whose original values were numbers (category ids) stay numbers.
                    val numeric = (f.value as? JsonArray)?.any { it is JsonPrimitive && !it.isString } == true
                    JsonArray(parts.map { p -> if (numeric) p.toLongOrNull()?.let { JsonPrimitive(it) } ?: JsonPrimitive(p) else JsonPrimitive(p) })
                }
                else -> v
            }
        }
    }
}

private fun JsonArray?.orEmpty(): List<JsonElement> = this ?: emptyList()

/** Keys of a plain JSON settings object that hold secrets (write-only in the generic editor). */
fun isSecretKey(key: String): Boolean {
    val k = key.lowercase()
    return listOf("password", "apikey", "api_key", "secret", "token", "passkey").any { k.contains(it) } && !k.endsWith("enabled") && !k.startsWith("use") && !k.startsWith("enable")
}

/**
 * The value to save for a key of a generic settings object: a secret only when the user typed
 * a new one, otherwise the original; numbers typed as text become numbers again when the
 * original was a number.
 */
fun mergeGeneric(original: JsonElement?, edited: JsonElement?, secret: Boolean): JsonElement? {
    if (edited == null) return original
    if (secret) {
        val t = (edited as? JsonPrimitive)?.contentOrNull
        if (t.isNullOrEmpty()) return original
    }
    val o = original as? JsonPrimitive
    val e = edited as? JsonPrimitive
    if (o != null && !o.isString && o !is JsonNull && e != null && e.isString) {
        val t = e.content.trim()
        t.toLongOrNull()?.let { return JsonPrimitive(it) }
        t.toDoubleOrNull()?.let { return JsonPrimitive(it) }
    }
    return edited
}

/**
 * Before saving a generic settings object: every leaf goes through [mergeGeneric] against the
 * original (untyped secrets keep their stored value, numbers typed as text become numbers).
 * Items of arrays of objects are matched to their original by `id` when they carry one, by
 * position when the array kept its length and has no ids, and otherwise (added, removed or
 * moved items) against a sibling as a [fresh] shape template: types are taken from it, but no
 * stored secret is copied into another item.
 */
fun normalizeGeneric(edited: JsonElement, original: JsonElement?, key: String = "", fresh: Boolean = false): JsonElement = when {
    edited is JsonObject -> JsonObject(edited.mapValues { (k, v) -> normalizeGeneric(v, (original as? JsonObject)?.get(k), k, fresh) })
    edited is JsonArray && original is JsonArray && edited.all { it is JsonObject } && original.isNotEmpty() && original.all { it is JsonObject } -> {
        val byId = original.filterIsInstance<JsonObject>().filter { it["id"] != null && it["id"] !is JsonNull }.associateBy { it["id"].toString() }
        val positional = byId.isEmpty() && edited.size == original.size
        JsonArray(edited.mapIndexed { i, v ->
            val o = v as JsonObject
            val match = o["id"]?.takeIf { it !is JsonNull }?.let { byId[it.toString()] } ?: if (positional) original[i] else null
            if (match != null) normalizeGeneric(o, match, key, fresh) else normalizeGeneric(o, original.first(), key, fresh = true)
        })
    }
    edited is JsonArray && original is JsonArray && original.isNotEmpty() && original.all { it is JsonPrimitive && !it.isString } ->
        JsonArray(edited.map { e ->
            val t = (e as? JsonPrimitive)?.takeIf { it.isString }?.content?.trim()
            if (t == null) e else t.toLongOrNull()?.let { JsonPrimitive(it) } ?: t.toDoubleOrNull()?.let { JsonPrimitive(it) } ?: e
        })
    edited is JsonArray -> edited
    // A key the original did not have (added with a new item, or by the user): kept as typed.
    original == null -> edited
    else -> mergeGeneric(original, edited, !fresh && isSecretKey(key)) ?: JsonNull
}
