package net.scyde.streamero.api

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

// Mirrors api/openapi.yaml (the source of truth). Unknown fields are ignored, so a
// newer server never breaks an older app.

@Serializable data class SystemInfo(
    val id: String, val name: String, val product: String, val version: String,
    val setupCompleted: Boolean = true, val loginDisclaimer: String? = null,
)

@Serializable data class DeviceInfo(val name: String, val client: String)
@Serializable data class LoginRequest(val username: String, val password: String, val totpCode: String? = null, val device: DeviceInfo)
@Serializable data class QuickConnectStart(val code: String, val secret: String, val expiresAt: String)
@Serializable data class QuickConnectPoll(val secret: String)
@Serializable data class QuickConnectCode(val code: String)
@Serializable data class QuickConnectApproved(val deviceName: String)
@Serializable data class AuthResult(val token: String? = null, val sessionId: String, val user: User)
@Serializable data class OidcProviderButton(val id: String, val label: String, val authorizeUrl: String)
@Serializable data class AuthProviders(val passwordLogin: Boolean = true, val ldap: Boolean = false, val oidc: List<OidcProviderButton> = emptyList())
@Serializable data class OidcExchangeRequest(val code: String, val codeVerifier: String, val device: DeviceInfo)

@Serializable data class UserPermissions(
    val allLibraries: Boolean = true, val download: Boolean = false, val videoTranscoding: Boolean = true,
    val audioTranscoding: Boolean = true, val remux: Boolean = true, val liveTv: Boolean = false,
    val manageLiveTv: Boolean = false, val syncPlay: Boolean = false, val deleteMedia: Boolean = false,
    val manageCollections: Boolean = false, val editSubtitles: Boolean = false, val remoteControl: Boolean = false,
    val maxSessions: Int = 0,
)

@Serializable data class Profile(
    val id: String, val name: String, val color: String, val hasPin: Boolean = false,
    val isKid: Boolean = false, val maxRating: Int? = null, val hasAvatar: Boolean = false,
)

@Serializable data class User(
    val id: String, val username: String, val isAdmin: Boolean = false, val disabled: Boolean = false,
    val totpEnabled: Boolean = false, val permissions: UserPermissions = UserPermissions(),
    val currentProfile: Profile? = null, val displayName: String? = null, val email: String? = null,
    val language: String? = null, val hasAvatar: Boolean = false, val createdAt: String = "", val lastLoginAt: String? = null,
)

@Serializable data class UpdateMe(val displayName: String? = null, val email: String? = null, val language: String? = null)

@Serializable data class ScanStatus(val state: String, val lastScanAt: String? = null, val error: String? = null, val filesSeen: Int = 0)
@Serializable data class Library(val id: String, val name: String, val kind: String, val paths: List<String> = emptyList(), val itemCount: Int = 0, val scan: ScanStatus = ScanStatus("idle"))
@Serializable data class LibraryInput(val name: String, val kind: String, val paths: List<String>)

@Serializable data class ItemSummary(
    val id: String, val kind: String, val title: String, val year: Int? = null,
    val seasonNumber: Int? = null, val episodeNumber: Int? = null, val addedAt: String = "",
    val hasPoster: Boolean = false, val hasBackdrop: Boolean = false, val hasLogo: Boolean = false,
    val seriesId: String? = null, val seriesTitle: String? = null, val parentId: String? = null,
    val artist: String? = null, val album: String? = null, val rating: Double? = null,
    val positionMs: Long = 0, val durationMs: Long? = null, val played: Boolean = false, val favorite: Boolean = false,
    val colors: List<String>? = null, val videoHeight: Int? = null, val hdr: String? = null,
) {
    val progress: Float get() = if (durationMs != null && durationMs > 0 && positionMs > 0) (positionMs.toFloat() / durationMs).coerceIn(0f, 1f) else 0f
}

@Serializable data class ItemPage(val items: List<ItemSummary>, val total: Int)

@Serializable data class AudioStream(val codec: String, val language: String? = null, val channels: Int? = null, val title: String? = null)
@Serializable data class SubtitleStream(val codec: String = "", val language: String? = null, val forced: Boolean = false, val external: Boolean = false)
@Serializable data class MediaFile(
    val id: String, val path: String, val label: String? = null, val size: Long = 0, val container: String = "",
    val durationMs: Long? = null, val width: Int? = null, val height: Int? = null, val videoCodec: String? = null,
    val hdr: String? = null, val audio: List<AudioStream> = emptyList(), val subtitles: List<SubtitleStream> = emptyList(),
)
@Serializable data class Person(val name: String, val role: String? = null, val imageUrl: String? = null)
@Serializable data class MediaCollection(val id: String, val name: String, val overview: String? = null, val itemCount: Int = 0, val hasPoster: Boolean = false, val hasBackdrop: Boolean = false, val auto: Boolean = false)
@Serializable data class Chapter(val startMs: Long, val title: String)
@Serializable data class TrickplayInfo(val intervalMs: Int, val width: Int, val height: Int, val columns: Int, val rows: Int, val count: Int, val sheetUrls: List<String>)

@Serializable data class ItemDetail(
    val item: ItemSummary, val libraryId: String, val parentId: String? = null,
    val overview: String? = null, val tagline: String? = null, val originalTitle: String? = null,
    val genres: List<String> = emptyList(), val studios: List<String> = emptyList(), val officialRating: String? = null,
    val premiereDate: String? = null, val cast: List<Person> = emptyList(), val crew: List<Person> = emptyList(),
    val hasBackdrop: Boolean = false, val hasLogo: Boolean = false, val files: List<MediaFile> = emptyList(),
    val children: List<ItemSummary> = emptyList(), val collections: List<MediaCollection> = emptyList(),
    val chapters: List<Chapter> = emptyList(), val trickplay: TrickplayInfo? = null,
    val providerIds: Map<String, String> = emptyMap(), val metadataError: String? = null,
)

@Serializable data class DeviceProfile(
    val containers: List<String>, val videoCodecs: List<String>, val audioCodecs: List<String>, val hls: Boolean,
    val maxHeight: Int? = null, val maxBitrate: Int? = null,
    /** Bitmap subtitle formats drawn by the player itself on direct play; others are burned in when chosen. */
    val subtitleCodecs: List<String>? = null,
)
@Serializable data class PlaybackRequest(
    val device: DeviceProfile, val fileId: String? = null, val audioIndex: Int? = null, val startMs: Long? = null,
    /** The file's subtitle track chosen by the viewer (-1 = none, absent = automatic); a bitmap track is burned in. */
    val subtitleIndex: Int? = null,
    /** Burn [subtitleIndex] in even when it is a text track. */
    val burnSubtitle: Boolean? = null,
)
/** A text subtitle track, offered as WebVTT at [url] (signed, already aligned with the stream's start). */
@Serializable data class PlaybackSubtitle(val index: Int, val language: String? = null, val forced: Boolean = false, val external: Boolean = false, val url: String)
@Serializable data class PlaybackBitmapSubtitle(val index: Int, val codec: String, val language: String? = null, val forced: Boolean = false)
@Serializable data class MediaSegment(val kind: String, val startMs: Long, val endMs: Long)
@Serializable data class PlaybackInfo(
    val fileId: String, val method: String, val url: String, val startMs: Long = 0, val durationMs: Long? = null,
    val audio: List<AudioStream> = emptyList(), val audioIndex: Int? = null, val subtitles: List<PlaybackSubtitle> = emptyList(),
    val subtitleDefault: Int? = null, val chapters: List<Chapter> = emptyList(), val segments: List<MediaSegment> = emptyList(),
    val trickplay: TrickplayInfo? = null, val reason: String? = null,
    /** Bitmap tracks; choosing one restarts playback with it burned in. */
    val bitmapSubtitles: List<PlaybackBitmapSubtitle> = emptyList(),
    /** The file subtitle index burned into this stream, null when none. */
    val burnedSubtitle: Int? = null,
)
@Serializable data class ProgressReport(val positionMs: Long, val durationMs: Long? = null, val played: Boolean? = null)
@Serializable data class UserDataUpdate(val favorite: Boolean? = null, val played: Boolean? = null)

@Serializable data class Preferences(
    val audioLanguage: String? = null, val subtitleLanguage: String? = null, val subtitleMode: String = "default",
    val autoplayNext: Boolean = true, val skipBackSeconds: Int? = null, val skipForwardSeconds: Int? = null,
    val maxBitrateKbps: Int? = null, val maxHeight: Int? = null, val upNextSeconds: Int? = null,
    val skipIntro: String? = null, val skipCredits: String? = null, val subtitleSize: String? = null,
    val subtitleColor: String? = null, val subtitleBackground: String? = null, val subtitlePosition: Int? = null,
    val homeSections: List<String>? = null,
)

@Serializable data class DiscoverTitle(
    val tmdbId: Int, val mediaType: String, val title: String, val year: Int? = null, val overview: String? = null,
    val posterUrl: String? = null, val backdropUrl: String? = null, val rating: Double? = null, val status: String = "none", val itemId: String? = null,
)
@Serializable data class DiscoverPage(val configured: Boolean, val canRequest: Boolean, val titles: List<DiscoverTitle>, val page: Int = 1, val totalPages: Int = 1)
@Serializable data class TitleRequest(val tmdbId: Int, val mediaType: String)
@Serializable data class TitleRequestResult(val status: String)
@Serializable data class UpcomingRelease(val kind: String, val title: String, val seriesTitle: String? = null, val season: Int? = null, val episode: Int? = null, val date: String, val overview: String? = null, val posterUrl: String? = null, val downloaded: Boolean = false)

@Serializable data class Enhancements(
    val qualityTags: Boolean = true, val randomButton: Boolean = true, val pauseScreen: Boolean = true, val arrLinks: Boolean = true,
    val discoverInSearch: Boolean = true, val removeFromContinue: Boolean = true, val shortcutsHelp: Boolean = true, val autoPip: Boolean = false,
)

// Administration
@Serializable data class NowPlaying(
    val key: String, val user: String, val profile: String? = null, val device: String, val client: String? = null,
    val itemId: String, val title: String, val method: String, val reason: String? = null, val positionMs: Long = 0, val durationMs: Long? = null,
)
@Serializable data class StorageUsage(val kind: String, val path: String, val totalBytes: Long, val freeBytes: Long)
@Serializable data class ActivityEntry(val at: String, val user: String? = null, val action: String, val detail: Map<String, JsonElement>? = null)
@Serializable data class DashboardServer(val name: String, val version: String, val os: String = "", val startedAt: String = "")
@Serializable data class Dashboard(val server: DashboardServer, val storage: List<StorageUsage>, val nowPlaying: List<NowPlaying>, val activeTranscodes: Int, val activity: List<ActivityEntry>)
@Serializable data class SessionCommand(val kind: String, val text: String? = null)
@Serializable data class TaskInfo(val key: String, val running: Boolean, val lastRunAt: String? = null, val lastState: String? = null, val interval: String? = null)
@Serializable data class LogEntry(val time: String, val level: String, val message: String, val source: String, val attrs: String? = null)
@Serializable data class ActivityPage(val entries: List<ActivityEntry>, val total: Int)
@Serializable data class CreateUser(val username: String, val password: String, val isAdmin: Boolean = false)
@Serializable data class UpdateUser(val isAdmin: Boolean? = null, val disabled: Boolean? = null, val password: String? = null, val resetTwoFactor: Boolean? = null)
@Serializable data class SessionInfo(val id: String, val userId: String, val username: String, val deviceName: String, val client: String, val createdAt: String, val lastSeenAt: String, val current: Boolean)
@Serializable data class ApiKey(val id: String, val name: String, val prefix: String, val createdBy: String, val createdAt: String, val lastUsedAt: String? = null)
@Serializable data class CreatedApiKey(val key: String, val apiKey: ApiKey)
@Serializable data class CreateApiKey(val name: String)
@Serializable data class CreateInvite(val expiresInHours: Int, val maxUses: Int, val note: String? = null)
@Serializable data class CreatedInvite(val token: String, val invite: Invite)
@Serializable data class CollectionDetail(val collection: MediaCollection, val items: List<ItemSummary>)
@Serializable data class Playlist(val id: String, val name: String, val itemCount: Int = 0, val durationMs: Long = 0, val public: Boolean = false, val mine: Boolean = true, val coverItemIds: List<String> = emptyList())
@Serializable data class PlaylistDetail(val playlist: Playlist, val items: List<ItemSummary>)
@Serializable data class Invite(val id: String, val note: String = "", val createdBy: String = "", val createdAt: String = "", val expiresAt: String = "", val maxUses: Int = 1, val uses: Int = 0)
@Serializable data class ServerSettings(val serverName: String)
@Serializable data class MetadataSettings(val tmdbConfigured: Boolean, val language: String, val country: String)
@Serializable data class MetadataSettingsInput(val language: String, val country: String, val tmdbApiKey: String? = null, val removeTmdbKey: Boolean? = null)
@Serializable data class EncodingSettings(
    val hardwareAcceleration: String = "none", val vaapiDevice: String? = null, val preset: String = "veryfast", val quality: Int = 22,
    val audioBitrateKbps: Int = 192, val segmentSeconds: Int = 6, val maxSessions: Int = 0, val toneMapping: Boolean = true,
)
@Serializable data class ResumeSettings(val minResumePercent: Int = 5, val maxResumePercent: Int = 90, val minResumeDurationSeconds: Int = 300)
@Serializable data class BrandingSettings(val loginDisclaimer: String = "", val customCss: String = "")
@Serializable data class RequestSettings(
    val seerrUrl: String? = null, val seerrConfigured: Boolean = false, val usersCanRequest: Boolean = true,
    val radarrUrl: String? = null, val radarrConfigured: Boolean = false, val sonarrUrl: String? = null, val sonarrConfigured: Boolean = false,
    val bazarrUrl: String? = null, val bazarrConfigured: Boolean = false,
)
@Serializable data class RequestSettingsInput(
    val usersCanRequest: Boolean, val seerrUrl: String? = null, val seerrApiKey: String? = null,
    val radarrUrl: String? = null, val radarrApiKey: String? = null, val sonarrUrl: String? = null, val sonarrApiKey: String? = null,
    val bazarrUrl: String? = null, val bazarrApiKey: String? = null,
)
@Serializable data class SubtitleOffer(
    val provider: String, val language: String, val forced: Boolean = false, val hearingImpaired: Boolean = false, val score: Double = 0.0,
    val release: List<String>? = null, val uploader: String? = null, val subtitle: String, val originalFormat: Boolean = false,
)
@Serializable data class SubtitleSearch(val configured: Boolean = false, val offers: List<SubtitleOffer> = emptyList())
@Serializable data class ConnectionCheck(val app: String, val ok: Boolean, val version: String? = null, val error: String? = null)
@Serializable data class ApiErrorBody(val code: String = "generic", val message: String = "")

// Watch together (SyncPlay, D-036). Times are the server's clock in Unix milliseconds.
@Serializable data class SyncQueueEntry(val id: String, val itemId: String, val title: String, val durationMs: Long? = null)
@Serializable data class SyncMember(
    val id: String, val name: String, val device: String = "", val ready: Boolean = false, val buffering: Boolean = false,
    val connected: Boolean = false, val lagging: Boolean = false,
)
@Serializable data class SyncGroup(
    val id: String, val name: String, val state: String, val queue: List<SyncQueueEntry> = emptyList(), val index: Int = -1,
    val positionMs: Long = 0, val anchorAt: Long = 0, val speed: Double = 1.0, val version: Long = 0,
    val reason: String? = null, val actor: String? = null, val members: List<SyncMember> = emptyList(),
    val serverTime: Long = 0, val createdAt: String = "", val selfId: String? = null,
) {
    val current: SyncQueueEntry? get() = queue.getOrNull(index)
    val self: SyncMember? get() = selfId?.let { id -> members.firstOrNull { it.id == id } }
}
@Serializable data class CreateSyncGroup(val name: String? = null, val itemIds: List<String>? = null, val index: Int? = null, val startMs: Long? = null)
@Serializable data class SyncCorrection(val mode: String, val rate: Double? = null, val durationMs: Long? = null, val positionMs: Long? = null, val driftMs: Long = 0)
/** A socket message to the server (SyncClientMessage); absent fields are left out. */
@Serializable data class SyncClientMessage(
    val type: String, val clientTime: Long? = null, val positionMs: Long? = null, val speed: Double? = null,
    val itemIds: List<String>? = null, val index: Int? = null, val startMs: Long? = null, val entryId: String? = null,
    val at: Long? = null, val playing: Boolean? = null,
)
/** A socket message from the server (SyncServerMessage). */
@Serializable data class SyncServerMessage(
    val type: String, val serverTime: Long = 0, val clientTime: Long? = null, val group: SyncGroup? = null,
    val reason: String? = null, val correction: SyncCorrection? = null, val code: String? = null, val message: String? = null,
)

// Offline downloads (D-040). The server prepares one file per job; the device keeps its own library.
@Serializable data class DownloadRequest(
    val itemId: String, val quality: String, val fileId: String? = null, val audioIndex: Int? = null,
    val nextEpisodes: Int? = null, val skipItemIds: List<String>? = null,
)
@Serializable data class DownloadSubtitle(val index: Int, val language: String = "", val forced: Boolean = false, val url: String, val vttUrl: String = "")
@Serializable data class Download(
    val id: String, val itemId: String, val fileId: String = "", val quality: String, val state: String, val progress: Double = 0.0,
    val sizeBytes: Long? = null, val error: String? = null, val url: String? = null, val fileName: String? = null,
    val container: String? = null, val durationMs: Long? = null, val audioIndex: Int? = null,
    val subtitles: List<DownloadSubtitle> = emptyList(), val item: ItemSummary, val overview: String? = null,
    val series: ItemSummary? = null, val createdAt: String = "",
)
@Serializable data class OfflineProgress(val itemId: String, val positionMs: Long, val durationMs: Long? = null, val played: Boolean, val updatedAt: String)
@Serializable data class OfflineSyncRequest(val itemIds: List<String>, val progress: List<OfflineProgress>)
@Serializable data class OfflineItemState(val itemId: String, val positionMs: Long = 0, val played: Boolean = false, val updatedAt: String)
@Serializable data class OfflineSyncResponse(val revoked: List<String> = emptyList(), val items: List<OfflineItemState> = emptyList(), val applied: Int = 0)
// Live TV and DVR (D-041). Times are ISO 8601 date-times; logo and image URLs are signed and server-relative.
@Serializable data class LiveTvProgram(
    val id: String, val channelId: String, val title: String, val start: String, val end: String,
    val channelName: String? = null, val channelNumber: String? = null, val subtitle: String? = null, val description: String? = null,
    val categories: List<String> = emptyList(), val category: String? = null, val season: Int? = null, val episode: Int? = null,
    val year: Int? = null, val isNew: Boolean = false, val isPremiere: Boolean = false, val isRepeat: Boolean = false,
    val isLive: Boolean = false, val imageUrl: String? = null, val recordingId: String? = null, val recordingStatus: String? = null,
    val seriesRuleId: String? = null,
)
@Serializable data class LiveTvChannel(
    val id: String, val number: String, val name: String, val tunerId: String, val favorite: Boolean = false,
    val group: String? = null, val category: String? = null, val logoUrl: String? = null, val hd: Boolean = false,
    val now: LiveTvProgram? = null, val next: LiveTvProgram? = null,
)
@Serializable data class LiveTvGuideRow(val channel: LiveTvChannel, val programs: List<LiveTvProgram> = emptyList())
@Serializable data class LiveTvGuide(val start: String, val end: String, val rows: List<LiveTvGuideRow> = emptyList())
@Serializable data class LiveTvPlayRequest(val device: DeviceProfile)
@Serializable data class LiveTvPlayback(
    val channelId: String, val url: String, val method: String, val timeshiftSeconds: Int = 0,
    val reason: String? = null, val now: LiveTvProgram? = null,
)
@Serializable data class LiveTvRecording(
    val id: String, val title: String, val start: String, val end: String, val status: String,
    val prePaddingSeconds: Int = 0, val postPaddingSeconds: Int = 0, val priority: Int = 0, val createdAt: String = "",
    val channelId: String? = null, val channelName: String? = null, val programId: String? = null, val seriesRuleId: String? = null,
    val subtitle: String? = null, val error: String? = null, val itemId: String? = null,
)
@Serializable data class LiveTvRecordingCreate(val programId: String, val channelId: String? = null)
@Serializable data class LiveTvSeriesRuleInput(val programId: String? = null, val channelId: String? = null, val newOnly: Boolean? = null, val title: String? = null)
@Serializable data class LiveTvSeriesRule(
    val id: String, val title: String, val newOnly: Boolean = true, val priority: Int = 0, val keepLast: Int = 0,
    val upcoming: Int = 0, val createdAt: String = "", val channelId: String? = null,
)
