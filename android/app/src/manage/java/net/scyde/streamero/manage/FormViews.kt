package net.scyde.streamero.manage

import androidx.compose.foundation.clickable
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import net.scyde.streamero.R
import net.scyde.streamero.ui.*

/** One text input of a form, with the field's help or its validation error under it. */
@Composable
fun FormText(
    label: String,
    value: String,
    onChange: (String) -> Unit,
    help: String? = null,
    error: String? = null,
    secret: Boolean = false,
    number: Boolean = false,
    multiline: Boolean = false,
    placeholder: String? = null,
) {
    OutlinedTextField(
        value, onChange, label = { Text(label) }, singleLine = !multiline, minLines = if (multiline) 3 else 1,
        isError = error != null,
        supportingText = (error ?: help)?.let { t -> { Text(t, color = if (error != null) Kino.danger else Kino.faint) } },
        placeholder = (if (secret) stringResource(R.string.m_secret_unchanged) else placeholder)?.let { p -> { Text(p, color = Kino.faint) } },
        visualTransformation = if (secret) PasswordVisualTransformation() else VisualTransformation.None,
        keyboardOptions = KeyboardOptions(keyboardType = when { secret -> KeyboardType.Password; number -> KeyboardType.Number; else -> KeyboardType.Text }),
        modifier = Modifier.fillMaxWidth(), shape = CardShape,
    )
}

/** Several choices from a list, picked in a dialog and shown as a summary line. */
@Composable
fun MultiPick(label: String, options: List<SelectOption>, selected: List<JsonElement>, help: String? = null, error: String? = null, onChange: (List<JsonElement>) -> Unit) {
    var open by remember { mutableStateOf(false) }
    val names = options.filter { o -> selected.any { same(it, o.value) } }.map { it.name }
    FieldRow(label, error ?: help) {
        Text(names.joinToString(", ").ifEmpty { stringResource(R.string.none) }, color = if (error != null) Kino.danger else Kino.muted, fontSize = 14.sp,
            modifier = Modifier.fillMaxWidth().clickable { open = true }.padding(vertical = 6.dp))
        TextButton(onClick = { open = true }) { Text(stringResource(R.string.edit)) }
    }
    if (open) {
        var cur by remember { mutableStateOf(selected) }
        AlertDialog(onDismissRequest = { open = false }, containerColor = Kino.surface,
            title = { Text(label, color = Kino.text) },
            text = {
                Column(Modifier.heightIn(max = 420.dp).verticalScroll(androidx.compose.foundation.rememberScrollState())) {
                    options.forEach { o ->
                        val on = cur.any { same(it, o.value) }
                        Row(Modifier.fillMaxWidth().clickable { cur = if (on) cur.filterNot { same(it, o.value) } else cur + o.value }.padding(vertical = 4.dp),
                            verticalAlignment = Alignment.CenterVertically) {
                            Checkbox(on, null, colors = CheckboxDefaults.colors(checkedColor = Kino.text, checkmarkColor = Kino.bg))
                            Column(Modifier.padding(start = 8.dp)) {
                                Text(o.name, color = Kino.text)
                                o.hint?.let { Text(it, color = Kino.faint, fontSize = 12.sp) }
                            }
                        }
                    }
                }
            },
            confirmButton = { TextButton(onClick = { onChange(cur); open = false }) { Text(stringResource(R.string.m_done), color = Kino.text) } },
            dismissButton = { TextButton(onClick = { open = false }) { Text(stringResource(R.string.cancel), color = Kino.muted) } })
    }
}

internal fun same(a: JsonElement, b: JsonElement): Boolean {
    if (a == b) return true
    val x = (a as? JsonPrimitive)?.contentOrNull
    val y = (b as? JsonPrimitive)?.contentOrNull
    return x != null && x == y
}

/**
 * The one schema-driven form renderer: every provider resource (indexers, download clients,
 * import lists, connections, metadata, applications, custom format specifications) is edited
 * through it. Advanced fields only show with [advanced]; validation errors from the service
 * appear under the field they name.
 */
@Composable
fun SchemaForm(state: SchemaFormState, onChange: (SchemaFormState) -> Unit, error: Throwable? = null, advanced: Boolean = false) {
    val v = error as? ArrException
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        state.fields.forEach { f ->
            if (f.kind == FieldKind.Hidden || (f.advanced && !advanced)) return@forEach
            val err = v?.messageFor(f.name)
            val label = f.label + (f.unit?.let { " ($it)" } ?: "")
            when (f.kind) {
                FieldKind.Checkbox -> {
                    Toggle(label, err ?: f.helpText, (state.value(f.name) as? JsonPrimitive)?.booleanOrNull == true) { onChange(state.set(f.name, JsonPrimitive(it))) }
                }
                FieldKind.Select -> FieldRow(label, err ?: f.helpText) {
                    val cur = state.value(f.name)
                    val idx = f.options.indexOfFirst { same(it.value, cur) }
                    Picker(idx, f.options.mapIndexed { i, o -> i to o.name }, { i -> onChange(state.set(f.name, f.options[i].value)) })
                }
                FieldKind.MultiSelect -> MultiPick(label, f.options, (state.value(f.name) as? JsonArray)?.toList() ?: emptyList(), f.helpText, err) {
                    onChange(state.set(f.name, JsonArray(it)))
                }
                FieldKind.Info -> Notice(Kino.muted) { Text(f.helpText ?: (f.value as? JsonPrimitive)?.contentOrNull ?: f.label, color = Kino.muted, fontSize = 13.sp) }
                FieldKind.Unknown -> FieldRow(label, stringResource(R.string.m_field_unsupported, f.type)) {
                    Text(f.value.toString().take(400), color = Kino.faint, fontSize = 13.sp)
                }
                else -> FormText(
                    label, state.text(f), { onChange(state.setText(f.name, it)) }, help = f.helpText,
                    error = err, secret = f.secret, number = f.kind == FieldKind.Number, multiline = f.kind == FieldKind.TextArea,
                    placeholder = f.placeholder ?: if (f.kind == FieldKind.Tag) stringResource(R.string.m_comma_separated) else null,
                )
            }
        }
    }
}

/** Errors no field claimed (or a non-validation failure) above a form. */
@Composable
fun FormErrors(error: Throwable?, fields: Collection<String>) {
    val v = error as? ArrException
    if (v != null && v.validation.isNotEmpty()) {
        v.unmatched(fields).forEach { Text(listOf(it.property, it.message).filter { s -> s.isNotBlank() }.joinToString(": "), color = if (it.warning) Kino.accent else Kino.danger, fontSize = 14.sp) }
    } else ArrError(error)
}

/**
 * A generic editor for a plain JSON settings object (the *arr `config` resources, Bazarr's
 * settings, Seerr's settings): booleans as switches, numbers and text as inputs, secrets
 * write-only, lists of values as comma-separated text and nested objects as sections.
 * [choices] turns known keys into dropdowns; [hide] leaves keys out (ids).
 */
@Composable
fun JsonForm(
    value: JsonObject,
    onChange: (JsonObject) -> Unit,
    error: Throwable? = null,
    hide: Set<String> = setOf("id"),
    choices: Map<String, List<Pair<JsonElement, String>>> = emptyMap(),
    labels: Map<String, String> = emptyMap(),
) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        JsonNodes(value, emptyList(), value, { onChange(it as JsonObject) }, error as? ArrException, hide, choices, labels, 0)
    }
}

@Composable
private fun JsonNodes(
    obj: JsonObject, path: List<Any>, root: JsonElement, onRoot: (JsonElement) -> Unit,
    err: ArrException?, hide: Set<String>, choices: Map<String, List<Pair<JsonElement, String>>>, labels: Map<String, String>, depth: Int,
) {
    // Simple values first, then lists and sections, so a page reads top-down.
    val keys = obj.keys.filter { it !in hide }.sortedBy { k -> when (obj[k]) { is JsonObject -> 2; is JsonArray -> 1; else -> 0 } }
    keys.forEach { k ->
        val el = obj[k] ?: JsonNull
        val p = path + k
        val label = labels[k] ?: humanize(k)
        val fieldErr = err?.messageFor(k)
        fun set(v: JsonElement) = onRoot(root.setAt(p, v))
        val ch = choices[k]
        when {
            ch != null && el is JsonPrimitive && ch.any { same(it.first, el) } -> FieldRow(label, fieldErr) {
                val idx = ch.indexOfFirst { same(it.first, el) }
                Picker(idx, ch.mapIndexed { i, c -> i to c.second }, { i -> set(ch[i].first) })
            }
            el is JsonObject -> if (depth >= 4) Unit else Section(label) { JsonNodes(el, p, root, onRoot, err, hide, choices, labels, depth + 1) }
            el is JsonArray && el.all { it is JsonObject } && el.isNotEmpty() -> Section("$label (${el.size})") {
                el.forEachIndexed { i, item ->
                    val o = item as JsonObject
                    val title = o.s("name") ?: o.s("label") ?: o.s("title") ?: "#${i + 1}"
                    Section(title) { JsonNodes(o, p + i, root, onRoot, err, hide, choices, labels, depth + 2) }
                }
            }
            el is JsonArray -> FormText(label, el.joinToString(", ") { (it as? JsonPrimitive)?.contentOrNull ?: it.toString() }, { t ->
                set(JsonArray(t.split(',').map { it.trim() }.filter { it.isNotEmpty() }.map { JsonPrimitive(it) }))
            }, error = fieldErr, placeholder = stringResource(R.string.m_comma_separated))
            el is JsonPrimitive && el !is JsonNull && el.booleanOrNull != null && !el.isString ->
                Toggle(label, fieldErr, el.booleanOrNull == true) { set(JsonPrimitive(it)) }
            else -> {
                val secret = isSecretKey(k)
                val text = if (secret) "" else (el as? JsonPrimitive)?.contentOrNull.orEmpty()
                var typed by remember(p.joinToString("/")) { mutableStateOf(text) }
                FormText(label, if (secret) typed else text, { t -> typed = t; set(JsonPrimitive(t)) }, error = fieldErr, secret = secret,
                    number = el is JsonPrimitive && !el.isString && el !is JsonNull)
            }
        }
    }
}

/** A collapsible group inside a form. */
@Composable
fun Section(title: String, initiallyOpen: Boolean = false, content: @Composable ColumnScope.() -> Unit) {
    var open by rememberSaveable(title) { mutableStateOf(initiallyOpen) }
    Column(Modifier.fillMaxWidth()) {
        Row(Modifier.fillMaxWidth().clickable { open = !open }.padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(title, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 15.sp, modifier = Modifier.weight(1f))
            Icon(if (open) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, null, tint = Kino.muted)
        }
        if (open) Column(Modifier.padding(start = 10.dp), verticalArrangement = Arrangement.spacedBy(6.dp), content = content)
        HorizontalDivider(color = Kino.border)
    }
}

@Composable
private fun <T> rememberSaveable(key: String, init: () -> MutableState<T>): MutableState<T> = remember(key) { init() }
