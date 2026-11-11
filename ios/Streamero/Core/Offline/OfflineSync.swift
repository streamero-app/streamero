import Foundation

/// What a sync answer changes on the device. Pure, so the rules can be tested.
struct OfflineSyncPlan: Equatable, Sendable {
    /// Items the profile may no longer keep: delete their files.
    var delete: [String] = []
    /// Server states that are newer than the device's: they replace the local playback.
    var replace: [String: OfflinePlayback] = [:]
    /// Items whose reported (dirty) state the server now has.
    var cleared: [String] = []
}

/// Offline progress sync (D-040): newest `updatedAt` wins, in both directions.
enum OfflineSync {
    /// The request for the device's library; `sent` keeps what was reported, per item.
    static func request(for library: OfflineLibrary) -> (request: OfflineSyncRequest, sent: [String: OfflinePlayback]) {
        var progress: [OfflineProgress] = []
        var sent: [String: OfflinePlayback] = [:]
        for entry in library.entries {
            guard let p = entry.playback, p.dirty else { continue }
            progress.append(OfflineProgress(itemId: entry.itemId, positionMs: max(0, p.positionMs), durationMs: p.durationMs,
                                            played: p.played, updatedAt: formatDate(p.updatedAt)))
            sent[entry.itemId] = p
        }
        return (OfflineSyncRequest(itemIds: library.entries.map(\.itemId), progress: progress), sent)
    }

    /// Decides what the answer changes, against the entries as they are now (playback may have
    /// moved on while the request was in flight; such newer local states stay dirty).
    static func plan(entries: [OfflineEntry], sent: [String: OfflinePlayback], response: OfflineSyncResponse) -> OfflineSyncPlan {
        var plan = OfflineSyncPlan()
        let revoked = Set(response.revoked)
        plan.delete = entries.map(\.itemId).filter { revoked.contains($0) }
        var server: [String: OfflineItemState] = [:]
        for state in response.items { server[state.itemId] = state }

        for entry in entries where !revoked.contains(entry.itemId) {
            let local = entry.playback
            let reported = sent[entry.itemId]
            // The report still describes the local state only if nothing was recorded since.
            let reportIsCurrent = reported != nil && local != nil && local!.updatedAt == reported!.updatedAt

            guard let state = server[entry.itemId], let serverDate = parseDate(state.updatedAt) else { continue }

            if let local {
                if serverDate > local.updatedAt.addingTimeInterval(0.001) {
                    // The server's state is newer (another device, or the web): take it.
                    plan.replace[entry.itemId] = OfflinePlayback(positionMs: state.positionMs, durationMs: local.durationMs,
                                                                 played: state.played, updatedAt: serverDate, dirty: false)
                } else if reportIsCurrent, let reported {
                    let sameState = state.positionMs == reported.positionMs && state.played == reported.played
                    // Saved (same or later time), or the server clamped a future device time to "now".
                    if serverDate >= reported.updatedAt.addingTimeInterval(-0.001) || sameState {
                        plan.cleared.append(entry.itemId)
                    }
                }
            } else {
                plan.replace[entry.itemId] = OfflinePlayback(positionMs: state.positionMs, durationMs: entry.durationMs,
                                                             played: state.played, updatedAt: serverDate, dirty: false)
            }
        }
        return plan
    }

    /// Applies the playback part of a plan; deletions need file I/O and are left to the caller.
    static func applyPlayback(_ plan: OfflineSyncPlan, to library: inout OfflineLibrary) {
        for i in library.entries.indices {
            let id = library.entries[i].itemId
            if let replacement = plan.replace[id] {
                library.entries[i].playback = replacement
            } else if plan.cleared.contains(id) {
                library.entries[i].playback?.dirty = false
            }
        }
    }

    /// Episodes a subscription fetched that are now watched: they make room for the next ones.
    static func watchedSubscriptionEpisodes(_ library: OfflineLibrary) -> [String] {
        let subscribed = Set(library.subscriptions.map(\.seriesId))
        return library.entries.filter { entry in
            guard let series = entry.subscriptionSeriesId, subscribed.contains(series) else { return false }
            return entry.state == .done && entry.isPlayed
        }.map(\.itemId)
    }

    /// Episodes of a series already on the device (or on their way), for `skipItemIds`.
    static func localEpisodes(of seriesId: String, in library: OfflineLibrary) -> [String] {
        library.entries.filter { $0.seriesId == seriesId && $0.state != .failed }.map(\.itemId)
    }

    /// Local playback after watching to `positionMs`; past 90% counts as watched.
    static func playback(positionMs: Int64, durationMs: Int64?, now: Date) -> OfflinePlayback {
        let played = (durationMs ?? 0) > 0 && Double(positionMs) >= 0.9 * Double(durationMs ?? 0)
        // Whole milliseconds, so the time survives the ISO 8601 round trip unchanged.
        let ms = (now.timeIntervalSince1970 * 1000).rounded(.down)
        return OfflinePlayback(positionMs: max(0, positionMs), durationMs: durationMs, played: played,
                               updatedAt: Date(timeIntervalSince1970: ms / 1000), dirty: true)
    }

    // MARK: Dates

    /// ISO 8601 with milliseconds ("2026-11-11T10:00:00.123Z").
    static func formatDate(_ date: Date) -> String {
        // Milliseconds are written by hand so they are rounded, never truncated.
        let ms = Int64((date.timeIntervalSince1970 * 1000).rounded())
        let whole = ms >= 0 ? ms / 1000 : (ms - 999) / 1000
        let fraction = ms - whole * 1000
        var text = Date(timeIntervalSince1970: TimeInterval(whole)).formatted(Date.ISO8601FormatStyle())
        if text.hasSuffix("Z") {
            text.insert(contentsOf: String(format: ".%03lld", fraction), at: text.index(before: text.endIndex))
        }
        return text
    }

    /// RFC 3339 with any number of fractional digits (Go writes up to nine) and any offset.
    static func parseDate(_ s: String) -> Date? {
        var text = s.trimmingCharacters(in: .whitespaces)
        var fraction = 0.0
        if let dot = text.firstIndex(of: ".") {
            var end = text.index(after: dot)
            while end < text.endIndex, text[end].isNumber { end = text.index(after: end) }
            let digits = String(text[text.index(after: dot)..<end])
            fraction = Double("0." + (digits.isEmpty ? "0" : digits)) ?? 0
            text.removeSubrange(dot..<end)
        }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        guard let base = formatter.date(from: text) else { return nil }
        return base.addingTimeInterval(fraction)
    }
}

/// Bytes the offline library takes on the device.
enum OfflineStorage {
    /// Bytes of one entry: the finished file and its extras, or what was fetched so far.
    static func bytes(_ entry: OfflineEntry) -> Int64 {
        entry.state == .done ? entry.videoBytes + entry.extraBytes : max(0, entry.bytesWritten) + entry.extraBytes
    }

    static func totalBytes(_ entries: [OfflineEntry]) -> Int64 {
        entries.reduce(0) { $0 + bytes($1) }
    }

    /// Bytes per series (movies under their own id).
    static func bytesByTitle(_ entries: [OfflineEntry]) -> [String: Int64] {
        var out: [String: Int64] = [:]
        for entry in entries { out[entry.seriesId ?? entry.itemId, default: 0] += bytes(entry) }
        return out
    }

    static func format(_ bytes: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: max(0, bytes), countStyle: .file)
    }
}
