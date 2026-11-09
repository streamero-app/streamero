package net.scyde.streamero.api

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.KSerializer
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.serializer
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import java.util.concurrent.TimeUnit

/** An error answered by the server, with its machine-readable code (see the web client's errors.*). */
class ApiException(val status: Int, val code: String, message: String) : Exception(message)

val json = Json { ignoreUnknownKeys = true; explicitNulls = false; encodeDefaults = true }

/**
 * Talks to one Streamero server. Authenticated with the session token from sign-in;
 * stream URLs the server returns are signed and need no token.
 */
class Api(baseUrl: String, @Volatile var token: String? = null) {
    val base: HttpUrl = baseUrl.trimEnd('/').toHttpUrl()

    val http: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .addInterceptor { chain ->
            val t = token
            val req = if (t != null && chain.request().url.host == base.host) {
                chain.request().newBuilder().header("Authorization", "Bearer $t").build()
            } else chain.request()
            chain.proceed(req)
        }
        .build()

    fun url(path: String, query: Map<String, Any?> = emptyMap()): HttpUrl {
        val b = base.newBuilder()
        path.trimStart('/').split('/').forEach { if (it.isNotEmpty()) b.addPathSegment(it) }
        query.forEach { (k, v) -> if (v != null) b.addQueryParameter(k, v.toString()) }
        return b.build()
    }

    private fun api(path: String, query: Map<String, Any?> = emptyMap()) = url("api/v1/$path", query)

    /** Absolute URL for a server-relative one (stream and trickplay URLs). */
    fun resolve(relative: String): String = if (relative.startsWith("http")) relative else base.resolve(relative).toString()

    fun imageUrl(itemId: String, kind: String) = api("items/$itemId/images/$kind").toString()
    fun avatarUrl(ownerId: String) = api("avatars/$ownerId").toString()
    fun collectionImageUrl(id: String, kind: String) = api("collections/$id/images/$kind").toString()

    suspend fun <T> call(method: String, path: String, body: RequestBody?, query: Map<String, Any?>, out: KSerializer<T>?): T? =
        withContext(Dispatchers.IO) {
            val req = Request.Builder().url(api(path, query)).method(method, body).build()
            http.newCall(req).execute().use { resp ->
                val text = resp.body?.string().orEmpty()
                if (!resp.isSuccessful) {
                    val err = runCatching { json.decodeFromString(ApiErrorBody.serializer(), text) }.getOrNull()
                    throw ApiException(resp.code, err?.code ?: "generic", err?.message ?: "HTTP ${resp.code}")
                }
                if (out == null || text.isEmpty()) null else json.decodeFromString(out, text)
            }
        }

    inline fun <reified B> jsonBody(b: B): RequestBody = json.encodeToString(serializer<B>(), b).toRequestBody(JSON)

    suspend inline fun <reified T> get(path: String, query: Map<String, Any?> = emptyMap()): T = call("GET", path, null, query, serializer<T>())!!
    suspend inline fun <reified B, reified T> post(path: String, body: B): T = call("POST", path, jsonBody(body), emptyMap(), serializer<T>())!!
    suspend inline fun <reified B, reified T> put(path: String, body: B): T = call("PUT", path, jsonBody(body), emptyMap(), serializer<T>())!!
    suspend inline fun <reified B> send(method: String, path: String, body: B) { call(method, path, jsonBody(body), emptyMap(), null as KSerializer<Unit>?) }
    suspend fun send(method: String, path: String) { call(method, path, if (method == "GET" || method == "DELETE") null else EMPTY, emptyMap(), null as KSerializer<Unit>?) }

    // System and sign-in
    suspend fun info(): SystemInfo = get("system/info")
    suspend fun login(username: String, password: String, totp: String?, device: DeviceInfo): AuthResult =
        post("auth/login", LoginRequest(username, password, totp?.ifBlank { null }, device))
    suspend fun logout() = send("POST", "auth/logout")
    suspend fun me(): User = get("auth/me")
    suspend fun updateMe(u: UpdateMe): User = put("auth/me", u)
    suspend fun uploadAvatar(bytes: ByteArray, profileId: String? = null) {
        call("PUT", if (profileId == null) "auth/me/avatar" else "profiles/$profileId/avatar",
            bytes.toRequestBody("application/octet-stream".toMediaType()), emptyMap(), null as KSerializer<Unit>?)
    }
    suspend fun removeAvatar(profileId: String? = null) = send("DELETE", if (profileId == null) "auth/me/avatar" else "profiles/$profileId/avatar")
    suspend fun changePassword(current: String, next: String) = send("POST", "auth/password", mapOf("currentPassword" to current, "newPassword" to next))

    // Profiles
    suspend fun profiles(): List<Profile> = get("profiles")
    suspend fun selectProfile(id: String, pin: String?) = send("POST", "profiles/$id/select", mapOf("pin" to pin))
    suspend fun selectMain(password: String) = send("POST", "profiles/main/select", mapOf("password" to password))

    // Browse
    suspend fun libraries(): List<Library> = get("libraries")
    suspend fun items(libraryId: String, query: Map<String, Any?>): ItemPage = get("libraries/$libraryId/items", query)
    suspend fun genres(libraryId: String): List<String> = get("libraries/$libraryId/genres")
    suspend fun item(id: String): ItemDetail = get("items/$id")
    suspend fun latest(): List<ItemSummary> = get("home/latest")
    suspend fun continueWatching(): List<ItemSummary> = get("home/continue")
    suspend fun nextUp(): List<ItemSummary> = get("home/next-up")
    suspend fun favorites(): List<ItemSummary> = get("home/favorites")
    suspend fun search(q: String): List<ItemSummary> = get("search", mapOf("q" to q))
    suspend fun nextEpisode(id: String): ItemSummary? = runCatching { get<ItemSummary>("items/$id/next") }.getOrNull()
    suspend fun setUserData(id: String, d: UserDataUpdate) = send("PUT", "items/$id/user-data", d)
    suspend fun collections(): List<MediaCollection> = get("collections")
    suspend fun collection(id: String): CollectionDetail = get("collections/$id")
    suspend fun playlists(): List<Playlist> = get("playlists")
    suspend fun playlist(id: String): PlaylistDetail = get("playlists/$id")

    // Playback
    suspend fun playback(id: String, r: PlaybackRequest): PlaybackInfo = post("items/$id/playback", r)
    suspend fun progress(id: String, p: ProgressReport) = send("PUT", "items/$id/progress", p)
    suspend fun preferences(): Preferences = get("me/preferences")
    suspend fun savePreferences(p: Preferences): Preferences = put("me/preferences", p)
    suspend fun commands(): List<SessionCommand> = get("me/commands")

    // Discover
    suspend fun discover(q: String?, page: Int = 1): DiscoverPage = get("discover", mapOf("q" to q?.ifBlank { null }, "page" to page))
    suspend fun request(t: DiscoverTitle): TitleRequestResult = post("discover/request", TitleRequest(t.tmdbId, t.mediaType))
    suspend fun upcoming(): List<UpcomingRelease> = get("upcoming", mapOf("days" to 30))
    suspend fun enhancements(): Enhancements = get("enhancements")

    // Administration
    suspend fun dashboard(): Dashboard = get("dashboard")
    suspend fun sendCommand(sessionKey: String, c: SessionCommand) = send("POST", "sessions/$sessionKey/command", c)
    suspend fun tasks(): List<TaskInfo> = get("tasks")
    suspend fun runTask(key: String) = send("POST", "tasks/$key/run")
    suspend fun logs(level: String, q: String?): List<LogEntry> = get("admin/logs", mapOf("level" to level, "q" to q?.ifBlank { null }, "limit" to 300))
    suspend fun activity(offset: Int): ActivityPage = get("admin/activity", mapOf("limit" to 50, "offset" to offset))
    suspend fun users(): List<User> = get("users")
    suspend fun createUser(u: CreateUser): User = post("users", u)
    suspend fun updateUser(id: String, u: UpdateUser): User = call("PATCH", "users/$id", jsonBody(u), emptyMap(), serializer<User>())!!
    suspend fun deleteUser(id: String) = send("DELETE", "users/$id")
    suspend fun sessions(): List<SessionInfo> = get("sessions")
    suspend fun revokeSession(id: String) = send("DELETE", "sessions/$id")
    suspend fun apiKeys(): List<ApiKey> = get("apikeys")
    suspend fun createApiKey(name: String): CreatedApiKey = post("apikeys", CreateApiKey(name))
    suspend fun deleteApiKey(id: String) = send("DELETE", "apikeys/$id")
    suspend fun invites(): List<Invite> = get("invites")
    suspend fun createInvite(c: CreateInvite): CreatedInvite = post("invites", c)
    suspend fun deleteInvite(id: String) = send("DELETE", "invites/$id")
    suspend fun createLibrary(l: LibraryInput): Library = post("libraries", l)
    suspend fun deleteLibrary(id: String) = send("DELETE", "libraries/$id")
    suspend fun scanLibrary(id: String) = send("POST", "libraries/$id/scan")
    suspend fun refreshLibraryMetadata(id: String) = send("POST", "libraries/$id/refresh-metadata")
    suspend fun serverSettings(): ServerSettings = get("settings/server")
    suspend fun saveServerSettings(s: ServerSettings): ServerSettings = put("settings/server", s)
    suspend fun metadataSettings(): MetadataSettings = get("settings/metadata")
    suspend fun saveMetadataSettings(s: MetadataSettingsInput): MetadataSettings = put("settings/metadata", s)
    suspend fun encoding(): EncodingSettings = get("settings/encoding")
    suspend fun saveEncoding(s: EncodingSettings): EncodingSettings = put("settings/encoding", s)
    suspend fun resume(): ResumeSettings = get("settings/resume")
    suspend fun saveResume(s: ResumeSettings): ResumeSettings = put("settings/resume", s)
    suspend fun branding(): BrandingSettings = get("settings/branding")
    suspend fun saveBranding(s: BrandingSettings): BrandingSettings = put("settings/branding", s)
    suspend fun requestSettings(): RequestSettings = get("settings/requests")
    suspend fun saveRequestSettings(s: RequestSettingsInput): RequestSettings = put("settings/requests", s)
    suspend fun testRequestSettings(): List<ConnectionCheck> = call("POST", "settings/requests/test", EMPTY, emptyMap(), ListSerializer(ConnectionCheck.serializer()))!!
    suspend fun searchSubtitles(itemId: String): SubtitleSearch = get("items/$itemId/subtitles/search")
    suspend fun downloadSubtitle(itemId: String, o: SubtitleOffer) = send("POST", "items/$itemId/subtitles/download", o)
    suspend fun searchMissingSubtitles(itemId: String) = send("POST", "items/$itemId/subtitles/search-missing")
    suspend fun saveEnhancements(e: Enhancements): Enhancements = put("enhancements", e)

    companion object {
        val JSON = "application/json".toMediaType()
        val EMPTY: RequestBody = ByteArray(0).toRequestBody(null)
        @Suppress("unused") private val keep = String.serializer()
    }
}
