import AVFoundation
import AVKit
import SwiftUI

/// Plays a downloaded file from the device: local position, local WebVTT sidecars, progress
/// recorded on the device (and sent to the server too while online).
@MainActor
@Observable
final class OfflinePlayerModel {
    let entry: OfflineEntry
    private(set) var player: AVPlayer?
    private(set) var missing = false
    private(set) var subtitleIndex = -1
    private(set) var cue: SubtitleCue?
    private var cues: [SubtitleCue] = []
    private var ticks = 0
    private var closed = false

    init(entry: OfflineEntry) {
        self.entry = entry
    }

    var titleLine: String {
        let it = entry.item
        if it.kind == "episode" {
            return (entry.series?.title ?? it.seriesTitle).map { "\($0) · " + episodeTitle(it) } ?? episodeTitle(it)
        }
        return it.title
    }

    func begin() async {
        do {
            try AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
            try AVAudioSession.sharedInstance().setActive(true)
        } catch {}
        let manager = DownloadManager.shared
        // The latest state (a sync may have moved the position since the list was drawn).
        let current = manager.entry(entry.itemId) ?? entry
        guard let url = manager.fileURL(for: current), FileManager.default.fileExists(atPath: url.path) else {
            missing = true
            return
        }
        let p = AVPlayer(url: url)
        let start = current.resumeMs
        if start > 0 {
            await p.seek(to: CMTime(value: start, timescale: 1000))
        }
        player = p
        p.play()
        // Forced subtitles (foreign dialogue) start on by themselves.
        if let forced = current.subtitles.firstIndex(where: { $0.forced }) {
            selectSubtitle(forced)
        }
    }

    func selectSubtitle(_ index: Int) {
        subtitleIndex = index
        cues = []
        cue = nil
        guard entry.subtitles.indices.contains(index),
              let url = DownloadManager.shared.localURL(entry.subtitles[index].file),
              let text = try? String(contentsOf: url, encoding: .utf8) else { return }
        cues = WebVTT.parse(text)
    }

    private var positionMs: Int64 {
        guard let player else { return 0 }
        let seconds = player.currentTime().seconds
        return seconds.isFinite ? Int64(seconds * 1000) : 0
    }

    private var durationMs: Int64? {
        if let item = player?.currentItem, item.duration.isNumeric {
            let seconds = item.duration.seconds
            if seconds.isFinite && seconds > 0 { return Int64(seconds * 1000) }
        }
        return entry.durationMs ?? entry.item.durationMs
    }

    /// One tick per second: subtitles; every 10 s the position is recorded.
    func tick() async {
        guard let player else { return }
        cue = WebVTT.cue(in: cues, at: positionMs)
        ticks += 1
        if ticks % 10 == 0 && player.timeControlStatus == .playing { await record() }
    }

    private func record() async {
        let pos = positionMs
        guard pos > 0 else { return }
        let manager = DownloadManager.shared
        manager.recordProgress(itemId: entry.itemId, positionMs: pos, durationMs: durationMs)
        // Online too: the server hears it now; the sync later confirms the device's record.
        if manager.online, let client = manager.client {
            await client.reportProgress(item: entry.itemId, positionMs: pos, durationMs: durationMs)
        }
    }

    func close() async {
        guard !closed else { return }
        closed = true
        await record()
        player?.pause()
        player = nil
        await DownloadManager.shared.sync()
    }
}

/// Full-screen player for a downloaded item; works without any connection.
struct OfflinePlayerView: View {
    @State private var model: OfflinePlayerModel
    @Environment(\.dismiss) private var dismiss

    init(entry: OfflineEntry) {
        _model = State(initialValue: OfflinePlayerModel(entry: entry))
    }

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if let player = model.player {
                VideoPlayer(player: player).ignoresSafeArea()
            } else if model.missing {
                Text("downloads.missing").foregroundStyle(.white).multilineTextAlignment(.center).padding()
            } else {
                ProgressView().tint(.white).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            if let cue = model.cue {
                SubtitleStyle(prefs: nil).view(cue.text)
                    .padding(.horizontal, 24).padding(.bottom, 56)
                    .frame(maxHeight: .infinity, alignment: .bottom)
                    .allowsHitTesting(false)
            }
            VStack {
                topBar
                Spacer()
            }
        }
        .task {
            UIApplication.shared.isIdleTimerDisabled = true
            await model.begin()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(1))
                await model.tick()
            }
        }
        .onDisappear {
            UIApplication.shared.isIdleTimerDisabled = false
            Task { await model.close() }
        }
    }

    private var topBar: some View {
        HStack {
            Button {
                Task {
                    await model.close()
                    dismiss()
                }
            } label: {
                Image(systemName: "xmark.circle.fill").font(.title).foregroundStyle(.white.opacity(0.8)).padding()
            }
            .accessibilityLabel(Text("player.back"))
            Text(verbatim: model.titleLine).font(.subheadline.weight(.semibold)).foregroundStyle(.white).lineLimit(1)
            Spacer()
            Image(systemName: "arrow.down.circle.fill").font(.footnote).foregroundStyle(.white.opacity(0.5))
                .accessibilityLabel(Text("downloads.offlineCopy"))
            if !model.entry.subtitles.isEmpty {
                Menu {
                    Button { model.selectSubtitle(-1) } label: { Text("off") }
                    ForEach(Array(model.entry.subtitles.enumerated()), id: \.offset) { idx, s in
                        Button { model.selectSubtitle(idx) } label: {
                            Text(verbatim: [s.language.uppercased(), s.forced ? Loc.string("forced") : nil].compactMap { $0 }.joined(separator: " · "))
                        }
                    }
                } label: {
                    Image(systemName: "captions.bubble.fill").font(.title).foregroundStyle(.white.opacity(0.8)).padding()
                }
                .accessibilityLabel(Text("subtitles"))
            }
        }
    }
}
