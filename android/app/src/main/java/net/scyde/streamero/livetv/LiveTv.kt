package net.scyde.streamero.livetv

import net.scyde.streamero.api.LiveTvChannel
import net.scyde.streamero.api.LiveTvGuide
import net.scyde.streamero.api.LiveTvProgram
import net.scyde.streamero.api.User
import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** Watching needs the liveTv permission (administrators always may). Kid profiles keep the account's permission. */
fun canWatchLiveTv(me: User?): Boolean = me != null && (me.isAdmin || me.permissions.liveTv)

/** Recording and series rules need manageLiveTv (administrators always may, but not from a kid profile). */
fun canManageLiveTv(me: User?): Boolean =
    me != null && me.currentProfile?.isKid != true && (me.isAdmin || me.permissions.manageLiveTv)

/** Parses an API date-time; null when it is missing or malformed. */
fun parseTime(s: String?): Instant? = s?.let { runCatching { OffsetDateTime.parse(it).toInstant() }.getOrNull() }

/** How far a programme has run at [now], 0..1 (0 before it starts or when its times are unusable). */
fun programProgress(p: LiveTvProgram?, now: Instant): Float {
    val start = parseTime(p?.start) ?: return 0f
    val end = parseTime(p?.end) ?: return 0f
    val total = end.toEpochMilli() - start.toEpochMilli()
    if (total <= 0) return 0f
    return ((now.toEpochMilli() - start.toEpochMilli()).toFloat() / total).coerceIn(0f, 1f)
}

/** The programme airing at [now] and the one after it, from a channel's programmes in any order. */
fun nowAndNext(programs: List<LiveTvProgram>, now: Instant): Pair<LiveTvProgram?, LiveTvProgram?> {
    val sorted = programs.mapNotNull { p -> parseTime(p.start)?.let { it to p } }.sortedBy { it.first }
    val current = sorted.lastOrNull { (start, p) -> !start.isAfter(now) && parseTime(p.end)?.isAfter(now) == true }?.second
    val next = sorted.firstOrNull { (start, _) -> start.isAfter(now) }?.second
    return current to next
}

/** Now/next of a channel: what the server sent, or computed from [programs] when it sent nothing. */
fun channelNowNext(c: LiveTvChannel, programs: List<LiveTvProgram>, now: Instant): Pair<LiveTvProgram?, LiveTvProgram?> =
    if (c.now != null || c.next != null) c.now to c.next else nowAndNext(programs, now)

/** Programmes of one channel from a guide, from the one airing now onwards, in time order. */
fun channelSchedule(guide: LiveTvGuide?, channelId: String, now: Instant): List<LiveTvProgram> =
    guide?.rows?.firstOrNull { it.channel.id == channelId }?.programs.orEmpty()
        .filter { p -> parseTime(p.end)?.isAfter(now) ?: true }
        .sortedBy { parseTime(it.start) ?: Instant.MAX }

/** "20:00–21:30" in the given zone. */
fun timeRange(p: LiveTvProgram, zone: ZoneId = ZoneId.systemDefault()): String {
    val f = DateTimeFormatter.ofPattern("HH:mm").withZone(zone)
    val s = parseTime(p.start)?.let(f::format) ?: "?"
    val e = parseTime(p.end)?.let(f::format) ?: "?"
    return "$s–$e"
}

/** "20:00" in the given zone. */
fun startTime(p: LiveTvProgram, zone: ZoneId = ZoneId.systemDefault()): String =
    parseTime(p.start)?.let { DateTimeFormatter.ofPattern("HH:mm").withZone(zone).format(it) } ?: "?"

/** A recording of this programme can still be scheduled (it has not ended and none is set yet). */
fun canRecord(p: LiveTvProgram, now: Instant): Boolean =
    p.recordingStatus.let { it == null || it == "cancelled" || it == "failed" } && (parseTime(p.end)?.isAfter(now) ?: false)

/** Channels with the viewer's favourite flag changed, keeping order. */
fun withFavorite(channels: List<LiveTvChannel>, id: String, favorite: Boolean): List<LiveTvChannel> =
    channels.map { if (it.id == id) it.copy(favorite = favorite) else it }
