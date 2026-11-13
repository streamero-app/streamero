package net.scyde.streamero.ui

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*

/** What the profile form holds; [id] null for a new profile. */
private data class ProfileForm(
    val id: String?, val name: String = "", val color: String = PROFILE_COLORS.first(), val isKid: Boolean = false,
    val maxRating: Int? = null, val pin: String = "", val removePin: Boolean = false, val hadPin: Boolean = false,
)

/** Extra profiles of this account (web: settings → Profiles). Not reachable from a kids profile. */
@Composable
fun ProfilesPage(state: AppState) {
    val api = state.api ?: return
    if (state.isKid) { Text(stringResource(R.string.err_kid_profile), color = Kino.danger); return }
    var tick by remember { mutableIntStateOf(0) }
    val (profiles, loadError) = rememberReload(tick) { api.profiles() }
    val scope = rememberCoroutineScope()
    val ctx = LocalContext.current
    val tv = LocalTv.current
    var error by remember { mutableStateOf<Throwable?>(null) }
    var editing by remember { mutableStateOf<ProfileForm?>(null) }
    var deleting by remember { mutableStateOf<Profile?>(null) }
    var avatarFor by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val pick = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        val id = avatarFor
        if (uri != null && id != null) scope.launch {
            runCatching { api.uploadAvatar(ctx.contentResolver.openInputStream(uri)!!.use { it.readBytes() }, id) }.onFailure { error = it }
            tick++
        }
    }
    Intro(stringResource(R.string.profiles_intro))
    ErrorLine(loadError ?: error)
    deleting?.let { p ->
        ConfirmDialog(stringResource(R.string.profile_delete_confirm, p.name), stringResource(R.string.delete), { deleting = null }) {
            scope.launch { runCatching { api.deleteProfile(p.id) }.onFailure { error = it }; tick++; state.refreshMe() }
        }
    }
    if (profiles == null && loadError == null) Loading()
    profiles?.forEach { p ->
        Card(null) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                Avatar(p.name, p.color, if (p.hasAvatar) api.avatarUrl(p.id) + "?v=$tick" else null, 56.dp)
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(p.name, color = Kino.text, fontSize = 17.sp, fontWeight = FontWeight.SemiBold)
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        if (p.isKid) Pill(stringResource(R.string.profile_kid), Kino.accent)
                        if (p.hasPin) Pill(stringResource(R.string.pin))
                        p.maxRating?.let { Pill(stringResource(R.string.profile_up_to, it)) }
                    }
                }
            }
            Actions {
                GlassButton(stringResource(R.string.edit), {
                    error = null
                    editing = ProfileForm(p.id, p.name, p.color, p.isKid, p.maxRating, hadPin = p.hasPin)
                })
                // A TV has no photo picker: pictures are changed from a phone or the web.
                if (!tv) GlassButton(stringResource(R.string.change_picture), { avatarFor = p.id; pick.launch("image/*") })
                if (!tv && p.hasAvatar) DangerAction(stringResource(R.string.remove_picture)) {
                    scope.launch { runCatching { api.removeAvatar(p.id) }.onFailure { error = it }; tick++ }
                }
                DangerAction(stringResource(R.string.delete)) { deleting = p }
            }
            if (editing?.id == p.id) ProfileEditor(editing!!, busy, { editing = it }, { editing = null }) { f ->
                busy = true; error = null
                scope.launch {
                    runCatching { api.updateProfile(p.id, profileInput(f.name, f.color, f.isKid, f.maxRating, f.pin, f.removePin)) }
                        .onSuccess { editing = null; tick++; state.refreshMe() }.onFailure { error = it }
                    busy = false
                }
            }
        }
    }
    val count = profiles?.size ?: MAX_PROFILES
    val form = editing
    if (form != null && form.id == null) {
        Card(stringResource(R.string.profile_add)) {
            ProfileEditor(form, busy, { editing = it }, { editing = null }) { f ->
                busy = true; error = null
                scope.launch {
                    runCatching { api.createProfile(profileInput(f.name, f.color, f.isKid, f.maxRating, f.pin, false)) }
                        .onSuccess { editing = null; tick++ }.onFailure { error = it }
                    busy = false
                }
            }
        }
    } else if (count < MAX_PROFILES) {
        PrimaryButton(stringResource(R.string.profile_add), { error = null; editing = ProfileForm(null) }, Modifier.fillMaxWidth())
    } else if (profiles != null) {
        Text(stringResource(R.string.err_too_many_profiles), color = Kino.faint, fontSize = 13.sp)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ProfileEditor(f: ProfileForm, busy: Boolean, onChange: (ProfileForm) -> Unit, onCancel: () -> Unit, onSave: (ProfileForm) -> Unit) {
    Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Field(stringResource(R.string.name), f.name, { onChange(f.copy(name = it.take(40))) })
        Text(stringResource(R.string.colour), color = Kino.text, fontSize = 15.sp, fontWeight = FontWeight.Medium)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            PROFILE_COLORS.forEach { c ->
                val selected = c.equals(f.color, ignoreCase = true)
                Box(Modifier.size(40.dp).focusClickable(RoundedCornerShape(8.dp), scale = 1.12f) { onChange(f.copy(color = c)) }
                    .then(if (selected) Modifier.border(2.dp, Kino.text, RoundedCornerShape(8.dp)).padding(4.dp) else Modifier)
                    .background(hexColor(c), RoundedCornerShape(6.dp)))
            }
        }
        Toggle(stringResource(R.string.profile_kid_profile), null, f.isKid) { onChange(f.copy(isKid = it)) }
        FieldRow(stringResource(R.string.profile_max_rating)) {
            Picker(f.maxRating ?: -1, listOf(-1 to stringResource(R.string.no_limit)) + PROFILE_RATINGS.map { it to stringResource(R.string.profile_up_to, it) },
                { onChange(f.copy(maxRating = it.takeIf { r -> r >= 0 })) })
        }
        OutlinedTextField(f.pin, { v -> onChange(f.copy(pin = v.filter { it.isDigit() }.take(6), removePin = false)) },
            label = { Text(stringResource(R.string.profile_pin_optional)) }, supportingText = { Text(stringResource(R.string.profile_pin_hint)) },
            singleLine = true, visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
            isError = f.pin.isNotEmpty() && !isValidPin(f.pin), modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
        if (f.id != null && f.hadPin) Toggle(stringResource(R.string.profile_remove_pin), null, f.removePin) { onChange(f.copy(removePin = it, pin = if (it) "" else f.pin)) }
        Actions {
            PrimaryButton(stringResource(R.string.save), { onSave(f) }, enabled = !busy && profileFormValid(f.name, f.color, f.pin))
            GlassButton(stringResource(R.string.cancel), onCancel)
        }
    }
}
