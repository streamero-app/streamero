import SwiftUI

/// The audiobook player: cover, chapter, scrubber over the whole book, skip, speed, sleep timer
/// and the chapter list.
struct AudiobookPlayerView: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var scrubbing: Double?
    @State private var chaptersOpen = false

    var body: some View {
        let p = app.audiobook
        VStack(spacing: 18) {
            Capsule().fill(Theme.textFaint).frame(width: 40, height: 5).padding(.top, 8)
            if let book = p.item {
                Spacer(minLength: 0)
                BookCover(item: book, client: client)
                    .frame(maxWidth: 300)
                    .shadow(color: .black.opacity(0.6), radius: 24, y: 14)
                VStack(spacing: 6) {
                    Text(verbatim: book.title).font(Theme.display(22)).foregroundStyle(Theme.text)
                        .multilineTextAlignment(.center).lineLimit(2).minimumScaleFactor(0.7)
                    if let authors = book.authors, !authors.isEmpty {
                        Text(verbatim: authors.joined(separator: ", ")).font(.subheadline).foregroundStyle(Theme.textMuted).lineLimit(1)
                    }
                    if let chapter = p.chapterTitle {
                        Button { chaptersOpen = true } label: {
                            Label { Text(verbatim: chapter).lineLimit(1) } icon: { Image(systemName: "list.bullet") }
                                .font(.footnote.weight(.semibold)).foregroundStyle(Theme.accent)
                        }
                    }
                }
                if p.failed {
                    Text("player.error").font(.subheadline).foregroundStyle(.red)
                }
                scrubber(p)
                transport(p)
                extras(p)
                Spacer(minLength: 0)
            } else {
                Spacer()
                Text("nothing_here").foregroundStyle(Theme.textMuted)
                Spacer()
            }
        }
        .padding(.horizontal, 24).padding(.bottom, 20)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(AmbientBackground(colors: p.item?.colors))
        .sheet(isPresented: $chaptersOpen) { AudiobookChaptersSheet().environment(app) }
    }

    private func scrubber(_ p: AudiobookPlayer) -> some View {
        let total = Double(max(1, p.duration))
        let shown = Int64(scrubbing ?? Double(p.position))
        return VStack(spacing: 4) {
            Slider(value: Binding(get: { scrubbing ?? Double(p.position) }, set: { scrubbing = $0 }), in: 0...total, onEditingChanged: { editing in
                if !editing, let value = scrubbing {
                    p.seek(Int64(value))
                    scrubbing = nil
                }
            })
            .tint(Theme.accent)
            HStack {
                Text(verbatim: clock(shown))
                Spacer()
                Text(verbatim: "-" + clock(max(0, p.duration - shown)))
            }
            .font(.caption.monospacedDigit()).foregroundStyle(Theme.textFaint)
        }
    }

    private func transport(_ p: AudiobookPlayer) -> some View {
        HStack {
            Button { p.previousChapter() } label: { Image(systemName: "backward.end.fill").font(.title3) }
                .accessibilityLabel(Text("books.previousChapter"))
            Spacer()
            Button { p.skip(seconds: -15) } label: { Image(systemName: "gobackward.15").font(.title) }
                .accessibilityLabel(Text("skip_back"))
            Spacer()
            Button { p.toggle() } label: {
                Group {
                    if p.loading {
                        ProgressView().tint(Theme.background)
                    } else {
                        Image(systemName: p.playing ? "pause.fill" : "play.fill").font(.title)
                    }
                }
                .foregroundStyle(Theme.background)
                .frame(width: 68, height: 68)
                .background(Theme.text, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            }
            .accessibilityLabel(Text("player.play"))
            Spacer()
            Button { p.skip(seconds: 30) } label: { Image(systemName: "goforward.30").font(.title) }
                .accessibilityLabel(Text("skip_forward"))
            Spacer()
            Button { p.nextChapter() } label: { Image(systemName: "forward.end.fill").font(.title3) }
                .accessibilityLabel(Text("books.nextChapter"))
        }
        .foregroundStyle(Theme.text)
    }

    private func extras(_ p: AudiobookPlayer) -> some View {
        HStack(spacing: 10) {
            Menu {
                Picker(selection: Binding(get: { p.rate }, set: { p.setRate($0) })) {
                    ForEach(AudiobookPlayer.speeds, id: \.self) { s in
                        Text(verbatim: speedLabel(s)).tag(s)
                    }
                } label: { EmptyView() }
            } label: {
                Label { Text(verbatim: speedLabel(p.rate)) } icon: { Image(systemName: "gauge.with.dots.needle.67percent") }
                    .modifier(PillLabel(active: p.rate != 1))
            }
            .accessibilityLabel(Text("books.speed"))
            Menu {
                Button("off") { p.setSleep(.off) }
                Button("books.sleepEndOfChapter") { p.setSleep(.endOfChapter) }
                ForEach(AudiobookPlayer.sleepMinutes, id: \.self) { m in
                    Button("books.sleepMinutes \(m)") { p.setSleep(.minutes(m)) }
                }
            } label: {
                Group {
                    if let deadline = p.sleepPlan.deadline {
                        Label { Text(timerInterval: Date()...max(Date(), deadline), countsDown: true).monospacedDigit() } icon: { Image(systemName: "moon.fill") }
                    } else if p.sleep == .endOfChapter {
                        Label("books.sleepEndOfChapter", systemImage: "moon.fill")
                    } else {
                        Label("books.sleep", systemImage: "moon")
                    }
                }
                .lineLimit(1)
                .modifier(PillLabel(active: p.sleep != .off))
            }
            Button { chaptersOpen = true } label: { Image(systemName: "list.bullet") }
                .buttonStyle(ToggleCapsuleStyle())
                .accessibilityLabel(Text("chapters"))
                .disabled((p.timeline?.chapters.count ?? 0) < 2)
        }
    }

    private func speedLabel(_ rate: Float) -> String {
        let text = rate == rate.rounded() ? String(Int(rate)) : String(format: "%g", Double(rate))
        return text + "×"
    }
}

/// The look of ToggleCapsuleStyle for menu labels (menus do not take button styles).
struct PillLabel: ViewModifier {
    var active = false

    func body(content: Content) -> some View {
        content
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(active ? Theme.background : Theme.text)
            .padding(.horizontal, 14).frame(maxWidth: .infinity, minHeight: 44)
            .background(active ? Theme.text : Color.white.opacity(0.12), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
    }
}

/// The chapters of the playing book; tap to jump.
struct AudiobookChaptersSheet: View {
    @Environment(AppState.self) private var app
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        let p = app.audiobook
        NavigationStack {
            ScrollViewReader { proxy in
                List {
                    if let timeline = p.timeline {
                        ForEach(Array(timeline.chapters.enumerated()), id: \.offset) { i, c in
                            Button {
                                p.jump(chapter: i)
                                dismiss()
                            } label: {
                                ChapterRow(index: i, chapter: c, end: timeline.chapterEnd(i), current: i == p.chapterIndex)
                            }
                            .buttonStyle(.plain)
                            .listRowBackground(Color.clear)
                            .id(i)
                        }
                    }
                }
                .listStyle(.plain)
                .scrollContentBackground(.hidden)
                .onAppear { if let i = p.chapterIndex { proxy.scrollTo(i, anchor: .center) } }
            }
            .background(Theme.surface.ignoresSafeArea())
            .navigationTitle(Text("chapters"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("close") { dismiss() } } }
        }
        .presentationDetents([.medium, .large])
    }
}

/// The audiobook bar above the tab bar; tap to open the player.
struct AudiobookMiniBar: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var open = false

    var body: some View {
        let p = app.audiobook
        if let book = p.item {
            VStack(spacing: 0) {
                GeometryReader { geo in
                    Rectangle().fill(.white.opacity(0.12))
                    Rectangle().fill(Theme.accent)
                        .frame(width: geo.size.width * (p.duration > 0 ? min(1, CGFloat(p.position) / CGFloat(p.duration)) : 0))
                }
                .frame(height: 2)
                HStack(spacing: 10) {
                    Button { open = true } label: {
                        HStack(spacing: 10) {
                            AuthImage(url: book.hasPoster ? client.imageURL(item: book.id, type: "poster") : nil, token: client.token, title: book.title)
                                .frame(width: 44, height: 44)
                                .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: book.title).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
                                Text(verbatim: p.chapterTitle ?? (book.authors ?? []).joined(separator: ", "))
                                    .font(.caption).foregroundStyle(Theme.textFaint).lineLimit(1)
                            }
                            Spacer(minLength: 0)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    Button { p.skip(seconds: -15) } label: { Image(systemName: "gobackward.15") }.accessibilityLabel(Text("skip_back"))
                    Button { p.toggle() } label: { Image(systemName: p.playing ? "pause.fill" : "play.fill") }
                        .frame(width: 36).accessibilityLabel(Text("player.play"))
                    Button { p.stop() } label: { Image(systemName: "xmark") }.accessibilityLabel(Text("stop"))
                }
                .padding(.horizontal, 12).padding(.vertical, 8)
            }
            .background(Theme.surface)
            .foregroundStyle(Theme.text)
            .sheet(isPresented: $open) { AudiobookPlayerView(client: client).environment(app) }
        }
    }
}
