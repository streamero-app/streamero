import AVFoundation
import AVKit
import SwiftUI

/// Live TV: channel list with what is on now and next, favourites, tap to watch (D-041).
/// The guide grid is web-only; the app shows a programme list per channel instead.
struct LiveTVView: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var channels: [LiveTvChannel] = []
    @State private var loaded = false
    @State private var error: Error?
    @State private var favoritesOnly = false
    @State private var watching: LiveTvChannel?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                PageTitle(title: "livetv.title").padding(.top, 8)
                Picker("livetv.filter", selection: $favoritesOnly) {
                    Text("livetv.all").tag(false)
                    Text("livetv.favorites").tag(true)
                }
                .pickerStyle(.segmented)
                if !loaded && error == nil { LoadingView() }
                ErrorLine(error: error)
                let shown = LiveTV.filter(channels, favoritesOnly: favoritesOnly)
                if loaded && shown.isEmpty && error == nil {
                    Text(LocalizedStringKey(favoritesOnly ? "livetv.noFavorites" : "livetv.empty"))
                        .foregroundStyle(Theme.textMuted).padding(.vertical, 24)
                }
                // TimelineView keeps the progress bars moving while the list is open.
                TimelineView(.periodic(from: .now, by: 30)) { context in
                    LazyVStack(spacing: 10) {
                        ForEach(shown) { ch in
                            LiveChannelRow(channel: ch, client: client, now: context.date,
                                           onPlay: { watching = ch },
                                           onFavorite: { Task { await toggleFavorite(ch) } })
                        }
                    }
                }
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .fullScreenCover(item: $watching, onDismiss: { Task { await load() } }) { ch in
            LivePlayerView(channel: ch, client: client)
                .environment(app)
        }
    }

    private func load() async {
        do {
            channels = try await client.liveChannels()
            error = nil
        } catch { self.error = error }
        loaded = true
    }

    private func toggleFavorite(_ ch: LiveTvChannel) async {
        let on = !ch.favorite
        if let i = channels.firstIndex(where: { $0.id == ch.id }) { channels[i].favorite = on }
        do {
            try await client.setLiveFavorite(channel: ch.id, on)
        } catch {
            if let i = channels.firstIndex(where: { $0.id == ch.id }) { channels[i].favorite = !on }
            self.error = error
        }
    }
}

/// Logo of a channel, or its number when it has none.
struct ChannelLogo: View {
    let channel: LiveTvChannel
    let client: MediaClient
    var size: CGFloat = 52

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: Theme.cornerRadius, style: .continuous).fill(Theme.surface2)
            if let logo = channel.logoUrl, let url = client.absolute(logo) {
                AuthImage(url: url, token: client.token, fit: true).padding(6)
            } else {
                Text(verbatim: channel.number).font(Theme.head(15)).foregroundStyle(Theme.textMuted)
                    .lineLimit(1).minimumScaleFactor(0.5).padding(4)
            }
        }
        .frame(width: size, height: size)
    }
}

/// Thin progress bar of the programme on now.
struct ProgramProgress: View {
    let value: Double
    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(Theme.text.opacity(0.12))
                Capsule().fill(Theme.accent).frame(width: geo.size.width * value)
            }
        }
        .frame(height: 3)
    }
}

struct LiveChannelRow: View {
    let channel: LiveTvChannel
    let client: MediaClient
    let now: Date
    let onPlay: () -> Void
    let onFavorite: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Button(action: onPlay) {
                HStack(spacing: 12) {
                    ChannelLogo(channel: channel, client: client)
                    VStack(alignment: .leading, spacing: 4) {
                        HStack(spacing: 6) {
                            Text(verbatim: channel.number).font(.caption.monospacedDigit()).foregroundStyle(Theme.textFaint)
                            Text(verbatim: channel.name).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
                        }
                        if let p = channel.now {
                            Text(verbatim: p.title).font(.subheadline).foregroundStyle(Theme.text).lineLimit(1)
                            ProgramProgress(value: LiveTV.progress(p, now: now))
                        } else {
                            Text("livetv.noGuide").font(.subheadline).foregroundStyle(Theme.textFaint)
                        }
                        if let n = channel.next {
                            (Text("livetv.next") + Text(verbatim: " · \(LiveTV.timeRange(n)) \(n.title)"))
                                .font(.caption).foregroundStyle(Theme.textMuted).lineLimit(1)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(CardPressStyle())
            VStack(spacing: 14) {
                Button(action: onFavorite) {
                    Image(systemName: channel.favorite ? "star.fill" : "star")
                        .foregroundStyle(channel.favorite ? Theme.accent : Theme.textMuted)
                }
                .accessibilityLabel(Text(LocalizedStringKey(channel.favorite ? "livetv.unfavorite" : "livetv.favorite")))
                NavigationLink(value: Route.liveChannel(channel)) {
                    Image(systemName: "list.bullet").foregroundStyle(Theme.textMuted)
                }
                .accessibilityLabel(Text("livetv.programs"))
            }
            .buttonStyle(.plain)
        }
        .padding(10)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.cornerRadius, style: .continuous))
    }
}

/// What a channel shows now and in the next hours, with "Record" / "Record series".
struct LiveChannelView: View {
    let channel: LiveTvChannel
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var programs: [LiveTvProgram] = []
    @State private var loaded = false
    @State private var error: Error?
    @State private var watching: LiveTvChannel?
    @State private var busy: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                HStack(spacing: 14) {
                    ChannelLogo(channel: channel, client: client, size: 64)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: channel.number).font(.caption.monospacedDigit()).foregroundStyle(Theme.textFaint)
                        Text(verbatim: channel.name).font(Theme.head(22)).foregroundStyle(Theme.text)
                    }
                }
                .padding(.top, 8)
                Button { watching = channel } label: {
                    Label("livetv.watch", systemImage: "play.fill").frame(maxWidth: .infinity)
                }
                .buttonStyle(PrimaryButtonStyle())
                if !loaded && error == nil { LoadingView() }
                ErrorLine(error: error)
                if loaded && programs.isEmpty && error == nil {
                    Text("livetv.noGuide").foregroundStyle(Theme.textMuted)
                }
                let now = Date()
                ForEach(LiveTV.upcoming(programs, now: now)) { p in
                    programRow(p, now: now)
                }
            }
            .padding(.horizontal, 20).padding(.bottom, 20)
        }
        .background(Theme.background)
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .fullScreenCover(item: $watching) { ch in
            LivePlayerView(channel: ch, client: client).environment(app)
        }
    }

    private func programRow(_ p: LiveTvProgram, now: Date) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(verbatim: LiveTV.timeRange(p)).font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
                if p.isNew { Text("livetv.new").font(.caption2.weight(.bold)).foregroundStyle(Theme.accent) }
                Spacer()
                if let status = p.recordingStatus {
                    Label(LocalizedStringKey(LiveTV.statusKey(status)), systemImage: "record.circle")
                        .font(.caption).foregroundStyle(status == "conflict" || status == "failed" ? .red : Theme.accent)
                }
            }
            Text(verbatim: p.title).font(.headline).foregroundStyle(Theme.text)
            if let sub = p.subtitle, !sub.isEmpty {
                Text(verbatim: sub).font(.subheadline).foregroundStyle(Theme.textMuted)
            }
            if (LiveTV.date(p.start) ?? .distantFuture) <= now {
                ProgramProgress(value: LiveTV.progress(p, now: now))
            }
            if let d = p.description, !d.isEmpty {
                Text(verbatim: d).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(3)
            }
            if LiveTV.canRecord(app.me) && LiveTV.canSchedule(p, now: now) {
                HStack(spacing: 10) {
                    Button { Task { await record(p, series: false) } } label: {
                        Label("livetv.record", systemImage: "record.circle").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(MutedButtonStyle())
                    if p.seriesRuleId == nil {
                        Button { Task { await record(p, series: true) } } label: {
                            Label("livetv.recordSeries", systemImage: "square.stack").frame(maxWidth: .infinity)
                        }
                        .buttonStyle(MutedButtonStyle())
                    }
                }
                .disabled(busy == p.id)
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.cornerRadius, style: .continuous))
    }

    private func load() async {
        do {
            let guide = try await client.liveGuide(hours: 12)
            programs = guide.rows.first { $0.channel.id == channel.id }?.programs ?? []
            error = nil
        } catch { self.error = error }
        loaded = true
    }

    private func record(_ p: LiveTvProgram, series: Bool) async {
        busy = p.id
        defer { busy = nil }
        do {
            if series {
                _ = try await client.recordSeries(p.id, channel: channel.id)
            } else {
                _ = try await client.recordProgram(p.id, channel: channel.id)
            }
            await load()
        } catch { self.error = error }
    }
}

/// Full-screen live player. The playlist is a sliding window, so AVKit's own live controls
/// pause and rewind within the server's time-shift buffer. No resume position is saved.
struct LivePlayerView: View {
    let channel: LiveTvChannel
    let client: MediaClient
    @Environment(\.dismiss) private var dismiss
    @Environment(AppState.self) private var app
    @State private var player: AVPlayer?
    @State private var error: Error?
    @State private var chrome = PlayerChrome()

    var body: some View {
        ZStack(alignment: .topLeading) {
            Color.black.ignoresSafeArea()
            if let player {
                SystemVideoPlayer(player: player, autoPip: app.enhancements.autoPip) { point, size in chrome.tap(at: point, in: size) }
                    .ignoresSafeArea()
            } else if let error {
                errorMessage(error).foregroundStyle(.white).padding().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ProgressView().tint(.white).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            Button { dismiss() } label: {
                Image(systemName: "xmark").font(.headline).foregroundStyle(.white)
                    .padding(12).background(.black.opacity(0.4), in: Circle())
            }
            .accessibilityLabel(Text("player.back"))
            // Below AVKit's own top row; fades with its controls.
            .padding(.horizontal, 16).padding(.top, player == nil ? 16 : 56)
            .opacity(player == nil || chrome.visible ? 1 : 0)
            .allowsHitTesting(player == nil || chrome.visible)
        }
        .videoAudioFocus(app.music)
        .task { await start() }
        .task {
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(1))
                chrome.sync(playing: player?.timeControlStatus == .playing)
            }
        }
        .onDisappear {
            player?.pause()
            player = nil
        }
    }

    private func start() async {
        do {
            try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
            try? AVAudioSession.sharedInstance().setActive(true)
            let prefs = try? await client.preferences()
            let pb = try await client.playLive(channel: channel.id, profile: MediaClient.device(preferences: prefs))
            guard let url = client.absolute(pb.url) else { throw APIError(status: 0, code: "bad_url") }
            let p = AVPlayer(url: url)
            p.play()
            player = p
        } catch {
            if let api = error as? APIError, api.code == "tuners_busy" {
                self.error = APIError(status: api.status, code: api.code, message: Loc.string("livetv.tunersBusy"))
            } else {
                self.error = error
            }
        }
    }
}
