import AVFoundation
import SwiftUI
import UIKit

// Watch together (D-036): the join sheet, the invite sheet after starting a group, and the
// group controls drawn over the video while the player follows a group.

/// Video without AVKit's controls; in a group the overlay's buttons send group commands.
struct PlayerLayerView: UIViewRepresentable {
    let player: AVPlayer

    final class LayerView: UIView {
        override class var layerClass: AnyClass { AVPlayerLayer.self }
        var playerLayer: AVPlayerLayer? { layer as? AVPlayerLayer }
    }

    func makeUIView(context: Context) -> LayerView {
        let view = LayerView()
        view.backgroundColor = .black
        view.playerLayer?.videoGravity = .resizeAspect
        view.playerLayer?.player = player
        return view
    }

    func updateUIView(_ view: LayerView, context: Context) {
        if view.playerLayer?.player !== player { view.playerLayer?.player = player }
    }
}

/// Members, who the group waits for, and transport controls that act for the whole group.
struct TogetherOverlay: View {
    let model: PlayerModel
    let group: SyncGroup
    @State private var scrub: Double?

    private var together: SyncSession { model.together }
    private var duration: Int64 { max(model.duration, group.currentEntry?.durationMs ?? 0) }

    var body: some View {
        VStack(spacing: 0) {
            Spacer().frame(height: 76)
            status
            Spacer()
            controls
        }
    }

    private var status: some View {
        VStack(spacing: 8) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 6) {
                    Image(systemName: "person.2.fill").font(.caption).foregroundStyle(Theme.textMuted)
                    ForEach(group.members) { member in chip(member) }
                }
                .padding(.horizontal, 24)
            }
            let waiting = group.waitingFor
            if !waiting.isEmpty {
                Text("together.waitingFor \(waiting.map(\.name).joined(separator: ", "))")
                    .font(.callout.weight(.semibold)).foregroundStyle(Theme.text)
                    .padding(.horizontal, 14).padding(.vertical, 8)
                    .background(.black.opacity(0.75), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            } else if !together.connected {
                Text("together.reconnecting")
                    .font(.callout).foregroundStyle(Theme.textMuted)
                    .padding(.horizontal, 14).padding(.vertical, 8)
                    .background(.black.opacity(0.75), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            }
        }
    }

    private func chip(_ m: SyncMember) -> some View {
        let isSelf = m.id == group.selfId
        let color: Color = !m.connected ? Theme.textFaint : (m.buffering || !m.ready) ? Theme.accent : Theme.text
        return HStack(spacing: 5) {
            Circle().fill(color).frame(width: 6, height: 6)
            Text(verbatim: m.name).font(.caption.weight(isSelf ? .bold : .medium)).foregroundStyle(Theme.text).lineLimit(1)
        }
        .padding(.horizontal, 9).frame(height: 26)
        .background(.black.opacity(0.6), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
    }

    private var controls: some View {
        let shown = scrub ?? Double(min(model.position, max(duration, model.position)))
        return VStack(spacing: 8) {
            Slider(value: Binding(get: { shown }, set: { scrub = $0 }),
                   in: 0...Double(max(1, duration)),
                   onEditingChanged: { editing in
                       if !editing, let value = scrub {
                           together.seek(Int64(value))
                           scrub = nil
                       }
                   })
                .tint(Theme.text)
            HStack {
                Text(verbatim: clock(Int64(shown)))
                Spacer()
                Text(verbatim: clock(duration))
            }
            .font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
            HStack(spacing: 30) {
                Button { together.previous() } label: { Image(systemName: "backward.end.fill") }
                    .disabled(group.index <= 0)
                    .accessibilityLabel(Text("together.previous"))
                Button { together.seek(max(0, model.position - 10_000)) } label: { Image(systemName: "gobackward.10") }
                    .accessibilityLabel(Text("together.back10"))
                Button {
                    if group.wantsPlay { together.pause() } else { together.play() }
                } label: {
                    Image(systemName: group.wantsPlay ? "pause.fill" : "play.fill").font(.system(size: 34))
                }
                .accessibilityLabel(group.wantsPlay ? Text("together.pause") : Text("player.play"))
                Button { together.seek(model.position + 10_000) } label: { Image(systemName: "goforward.10") }
                    .accessibilityLabel(Text("together.forward10"))
                Button { together.next() } label: { Image(systemName: "forward.end.fill") }
                    .disabled(!together.hasNext)
                    .accessibilityLabel(Text("together.next"))
            }
            .font(.title2)
            .foregroundStyle(Theme.text)
        }
        .padding(.horizontal, 24).padding(.top, 28).padding(.bottom, 20)
        .background(LinearGradient(colors: [.clear, .black.opacity(0.8)], startPoint: .top, endPoint: .bottom).ignoresSafeArea())
    }
}

/// Join a group from the list or with an invite link; shows the group this device is in.
struct TogetherView: View {
    /// Called with the item and position to open once the sheet is gone.
    let onOpen: (String, Int64) -> Void
    @Environment(AppState.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var groups: [SyncGroup]?
    @State private var invite = ""
    @State private var badInvite = false
    @State private var error: Error?
    @State private var busy = false

    var body: some View {
        NavigationStack {
            Form {
                if let current = app.together.group { currentSection(current) }
                Section {
                    TextField("together.invite", text: $invite)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                    Button {
                        Task { await joinInvite() }
                    } label: {
                        if busy { ProgressView() } else { Text("together.join").bold() }
                    }
                    .disabled(busy || invite.trimmingCharacters(in: .whitespaces).isEmpty)
                    if badInvite { Text("together.badInvite").foregroundStyle(.red) }
                    ErrorLine(error: error)
                } footer: {
                    Text("together.inviteHint")
                }
                Section {
                    if let groups {
                        if groups.isEmpty {
                            Text("together.noGroups").foregroundStyle(Theme.textMuted)
                        }
                        ForEach(groups) { g in row(g) }
                    } else {
                        ProgressView().frame(maxWidth: .infinity)
                    }
                } header: {
                    Text("together.groups")
                }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background)
            .navigationTitle(Text("together.title"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("close") { dismiss() } } }
            .task { await load() }
            .refreshable { await load() }
        }
    }

    @ViewBuilder private func currentSection(_ g: SyncGroup) -> some View {
        Section {
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: g.currentEntry?.title ?? g.name).font(.headline).foregroundStyle(Theme.text)
                Text("together.members \(g.members.count)").font(.footnote).foregroundStyle(Theme.textMuted)
            }
            if g.currentEntry != nil {
                Button { open(g) } label: { Label("together.openPlayer", systemImage: "play.fill") }
            } else {
                Text("together.noTitle").foregroundStyle(Theme.textMuted)
            }
            Button(role: .destructive) {
                Task {
                    await app.together.leave()
                    await load()
                }
            } label: {
                Text("together.leave")
            }
        } header: {
            Text("together.current")
        }
    }

    private func row(_ g: SyncGroup) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: g.currentEntry?.title ?? g.name).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
                Text(verbatim: g.members.map(\.name).joined(separator: ", ")).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(1)
            }
            Spacer()
            if g.id == app.together.group?.id {
                Button { open(g) } label: { Image(systemName: "play.fill") }
                    .disabled(g.currentEntry == nil)
                    .accessibilityLabel(Text("together.openPlayer"))
            } else {
                Button { Task { await join(g.id) } } label: { Text("together.join") }
                    .disabled(busy)
            }
        }
    }

    private func load() async {
        do {
            groups = try await app.together.groups()
            error = nil
        } catch is CancellationError {
            return
        } catch {
            if groups == nil { groups = [] }
            self.error = error
        }
    }

    private func joinInvite() async {
        guard let id = SyncInvite.groupId(from: invite) else {
            badInvite = true
            return
        }
        badInvite = false
        await join(id)
    }

    private func join(_ id: String) async {
        busy = true
        defer { busy = false }
        do {
            let g = try await app.together.join(id)
            error = nil
            invite = ""
            if g.currentEntry != nil { open(g) } else { await load() }
        } catch {
            self.error = error
        }
    }

    private func open(_ g: SyncGroup) {
        guard let entry = (app.together.group ?? g).currentEntry else { return }
        onOpen(entry.itemId, app.together.sharedPosition ?? g.positionMs)
        dismiss()
    }
}

/// After starting a group from an item: the invite link to share, then start watching.
struct TogetherInviteSheet: View {
    let link: URL
    let onStart: () -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: 18) {
                Text("together.created").font(Theme.display(26)).foregroundStyle(Theme.text)
                Text("together.createdHint").font(.callout).foregroundStyle(Theme.textMuted)
                Text(verbatim: link.absoluteString)
                    .font(.footnote.monospaced()).foregroundStyle(Theme.text)
                    .textSelection(.enabled)
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Theme.surface2, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                ShareLink(item: link) {
                    Label("together.share", systemImage: "square.and.arrow.up").frame(maxWidth: .infinity)
                }
                .buttonStyle(MutedButtonStyle())
                Button {
                    onStart()
                    dismiss()
                } label: {
                    Label("together.startWatching", systemImage: "play.fill").frame(maxWidth: .infinity)
                }
                .buttonStyle(PrimaryButtonStyle())
                Spacer()
            }
            .padding(20)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Theme.background)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("close") { dismiss() } } }
        }
        .presentationDetents([.medium, .large])
    }
}
