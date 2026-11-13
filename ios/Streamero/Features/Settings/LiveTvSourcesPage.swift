import SwiftUI

/// Live TV sources (admin): tuners (M3U playlists, HDHomeRun devices) and XMLTV guides.
/// Channel-to-guide mapping and DVR settings stay on the web (a wide table, not a phone form).
struct LiveTvSourcesPage: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var tuners: [LiveTvTuner]?
    @State private var guides: [LiveTvGuideSource]?
    @State private var error: Error?
    @State private var editingTuner: TunerEditTarget?
    @State private var editingGuide: GuideEditTarget?
    @State private var deletingTuner: LiveTvTuner?
    @State private var deletingGuide: LiveTvGuideSource?
    @State private var refreshing: String?
    @State private var guideQueued = false

    var body: some View {
        AdminGuarded {
            List {
                if let error { Section { ErrorLine(error: error) } }
                tunersSection
                guidesSection
                Section {
                    Label("livetvAdmin.mappingOnWeb", systemImage: "rectangle.on.rectangle")
                        .font(.footnote).foregroundStyle(Theme.textMuted)
                }
            }
            .sheet(item: $editingTuner) { target in
                TunerEditor(client: client, tuner: target.tuner) { Task { await load() } }
                    .environment(\.locale, app.locale ?? Locale.current)
            }
            .sheet(item: $editingGuide) { target in
                GuideEditor(client: client, guide: target.guide) { Task { await load() } }
                    .environment(\.locale, app.locale ?? Locale.current)
            }
            .confirmationDialog(Text(verbatim: deletingTuner.map { String(format: Loc.string("livetvAdmin.deleteTunerConfirm %@"), $0.name) } ?? ""),
                                isPresented: Binding(get: { deletingTuner != nil }, set: { if !$0 { deletingTuner = nil } }),
                                titleVisibility: .visible, presenting: deletingTuner) { t in
                Button("delete", role: .destructive) { perform { try await client.deleteTuner(t.id) } }
            }
            .confirmationDialog(Text(verbatim: deletingGuide.map { String(format: Loc.string("livetvAdmin.deleteGuideConfirm %@"), $0.name) } ?? ""),
                                isPresented: Binding(get: { deletingGuide != nil }, set: { if !$0 { deletingGuide = nil } }),
                                titleVisibility: .visible, presenting: deletingGuide) { g in
                Button("delete", role: .destructive) { perform { try await client.deleteGuide(g.id) } }
            }
            .refreshable { await load() }
            .task { await load() }
        }
    }

    private var tunersSection: some View {
        Section {
            if tuners == nil && error == nil { LoadingView() }
            if tuners?.isEmpty == true { Text("livetvAdmin.noTuners").foregroundStyle(Theme.textMuted) }
            ForEach(tuners ?? []) { t in
                Button { editingTuner = TunerEditTarget(tuner: t) } label: { tunerRow(t) }
                    .swipeActions {
                        Button("delete", role: .destructive) { deletingTuner = t }
                        Button("livetvAdmin.refreshChannels") { refreshTuner(t) }.tint(Theme.accent)
                    }
                    .contextMenu {
                        Button("livetvAdmin.refreshChannels", systemImage: "arrow.clockwise") { refreshTuner(t) }
                        Button("common.edit", systemImage: "pencil") { editingTuner = TunerEditTarget(tuner: t) }
                        Button("delete", systemImage: "trash", role: .destructive) { deletingTuner = t }
                    }
            }
            Button { editingTuner = TunerEditTarget(tuner: nil) } label: { Label("livetvAdmin.addTuner", systemImage: "plus") }
        } header: {
            Text("livetvAdmin.tuners")
        } footer: {
            Text("livetvAdmin.tunersIntro")
        }
    }

    private var guidesSection: some View {
        Section {
            if guides?.isEmpty == true { Text("livetvAdmin.noGuides").foregroundStyle(Theme.textMuted) }
            ForEach(guides ?? []) { g in
                Button { editingGuide = GuideEditTarget(guide: g) } label: { guideRow(g) }
                    .swipeActions {
                        Button("delete", role: .destructive) { deletingGuide = g }
                        Button("livetvAdmin.refreshNow") { refreshGuide(g) }.tint(Theme.accent)
                    }
                    .contextMenu {
                        Button("livetvAdmin.refreshNow", systemImage: "arrow.clockwise") { refreshGuide(g) }
                        Button("common.edit", systemImage: "pencil") { editingGuide = GuideEditTarget(guide: g) }
                        Button("delete", systemImage: "trash", role: .destructive) { deletingGuide = g }
                    }
            }
            if guideQueued { Text("livetvAdmin.refreshQueued").font(.footnote).foregroundStyle(.green) }
            Button { editingGuide = GuideEditTarget(guide: nil) } label: { Label("livetvAdmin.addGuide", systemImage: "plus") }
        } header: {
            Text("livetvAdmin.guides")
        } footer: {
            Text("livetvAdmin.guidesIntro")
        }
    }

    private func tunerRow(_ t: LiveTvTuner) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 6) {
                Text(verbatim: t.name).font(.body.weight(.semibold)).foregroundStyle(Theme.text)
                TagBadge(text: t.kind == "hdhomerun" ? "livetvAdmin.kind_hdhomerun" : "livetvAdmin.kind_m3u")
                if !t.enabled { TagBadge(text: "livetvAdmin.disabled") }
                if refreshing == t.id { ProgressView().controlSize(.small) }
            }
            dotJoined([
                plain(t.model), plain(t.url ?? t.filePath),
                Text("livetvAdmin.channelsN \(t.channelCount)"),
                t.tunerCount > 0 ? Text("livetvAdmin.tunersN \(t.tunerCount)") : Text("livetvAdmin.unlimited"),
                plain(t.lastRefreshAt.map { shortTime($0) }),
            ])
            .font(.caption).foregroundStyle(Theme.textMuted).lineLimit(3)
            if let e = t.error, !e.isEmpty { Text(verbatim: e).font(.caption).foregroundStyle(.red) }
        }
        .padding(.vertical, 2)
    }

    private func guideRow(_ g: LiveTvGuideSource) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 6) {
                Text(verbatim: g.name).font(.body.weight(.semibold)).foregroundStyle(Theme.text)
                if !g.enabled { TagBadge(text: "livetvAdmin.disabled") }
            }
            dotJoined([
                plain(g.url ?? g.filePath),
                Text("livetvAdmin.channelsN \(g.channelCount)"),
                Text("livetvAdmin.programsN \(g.programCount)"),
                Text("livetvAdmin.everyHours \(g.refreshHours)"),
                g.lastRefreshAt.map { Text(verbatim: shortTime($0)) } ?? Text("livetvAdmin.notLoaded"),
            ])
            .font(.caption).foregroundStyle(Theme.textMuted).lineLimit(3)
            if let r = g.lastResult, !r.isEmpty { Text(verbatim: r).font(.caption).foregroundStyle(Theme.textFaint) }
            if let e = g.error, !e.isEmpty { Text(verbatim: e).font(.caption).foregroundStyle(.red) }
        }
        .padding(.vertical, 2)
    }

    private func refreshTuner(_ t: LiveTvTuner) {
        refreshing = t.id
        perform {
            defer { refreshing = nil }
            _ = try await client.refreshTuner(t.id)
        }
    }

    private func refreshGuide(_ g: LiveTvGuideSource) {
        perform {
            try await client.refreshGuide(g.id)
            guideQueued = true
        }
    }

    private func load() async {
        do {
            async let t = client.liveTvTuners()
            async let g = client.liveTvGuides()
            tuners = try await t
            guides = try await g
            error = nil
        } catch is CancellationError { return } catch { self.error = error }
    }

    private func perform(_ action: @MainActor @escaping () async throws -> Void) {
        Task {
            do {
                try await action()
                await load()
            } catch { self.error = error }
        }
    }
}

struct TunerEditTarget: Identifiable {
    let tuner: LiveTvTuner?
    var id: String { tuner?.id ?? "new" }
}

struct GuideEditTarget: Identifiable {
    let guide: LiveTvGuideSource?
    var id: String { guide?.id ?? "new" }
}

/// The result line of a "Test" (the server's own message).
private struct TestOutcome: View {
    let result: LiveTvTestResult?
    var body: some View {
        if let result {
            Label { Text(verbatim: result.message) } icon: { Image(systemName: result.ok ? "checkmark.circle.fill" : "xmark.octagon.fill") }
                .font(.footnote).foregroundStyle(result.ok ? .green : .red)
        }
    }
}

private struct TunerEditor: View {
    let client: MediaClient
    let tuner: LiveTvTuner?
    let onSaved: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var form = TunerForm()
    @State private var result: LiveTvTestResult?
    @State private var testing = false
    @State private var busy = false
    @State private var error: Error?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker(selection: $form.kind) {
                        Text("livetvAdmin.kind_m3u").tag("m3u")
                        Text("livetvAdmin.kind_hdhomerun").tag("hdhomerun")
                    } label: { Text("livetvAdmin.kind") }
                    TextField("livetvAdmin.name", text: $form.name)
                }
                Section {
                    if form.kind == "m3u" {
                        Picker(selection: $form.fromFile) {
                            Text("livetvAdmin.fromUrl").tag(false)
                            Text("livetvAdmin.fromFile").tag(true)
                        } label: { Text("livetvAdmin.source") }
                            .pickerStyle(.segmented)
                    }
                    if form.kind == "m3u" && form.fromFile {
                        LabeledField(title: "livetvAdmin.playlistFile", text: $form.filePath, prompt: "/config/livetv/channels.m3u")
                        FieldHint(text: "livetvAdmin.fileHint")
                    } else {
                        LabeledField(title: form.kind == "m3u" ? "livetvAdmin.playlistUrl" : "livetvAdmin.deviceAddress",
                                     text: $form.url, prompt: form.kind == "m3u" ? "https://example.com/playlist.m3u" : "http://192.168.1.50",
                                     keyboard: .URL)
                        if form.kind == "hdhomerun" { FieldHint(text: "livetvAdmin.deviceAddressHint") }
                    }
                }
                Section {
                    LabeledField(title: "livetvAdmin.tunerCount", text: $form.count, keyboard: .numberPad)
                    FieldHint(text: form.kind == "m3u" ? "livetvAdmin.tunerCountM3u" : "livetvAdmin.tunerCountHdhr")
                    if form.kind == "m3u" {
                        LabeledField(title: "livetvAdmin.userAgent", text: $form.userAgent)
                        FieldHint(text: "livetvAdmin.userAgentHint")
                    }
                    Toggle("livetvAdmin.enabled", isOn: $form.enabled)
                }
                Section {
                    Button { Task { await test() } } label: {
                        HStack {
                            Text("livetvAdmin.test")
                            if testing { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(!form.isValid || testing)
                    TestOutcome(result: result)
                    ErrorLine(error: error)
                }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background)
            .navigationTitle(Text(tuner == nil ? LocalizedStringKey("livetvAdmin.addTuner") : LocalizedStringKey("common.edit")))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if busy { ProgressView() } else { Button("save") { Task { await save() } }.disabled(!form.isValid) }
                }
            }
        }
        .presentationBackground(Theme.background)
        .onAppear { if let tuner, form == TunerForm() { form = TunerForm(tuner) } }
    }

    private func test() async {
        testing = true
        defer { testing = false }
        do {
            result = try await client.testTuner(form.input)
            error = nil
        } catch { self.error = error }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        do {
            if let tuner { _ = try await client.updateTuner(tuner.id, form.input) } else { _ = try await client.createTuner(form.input) }
            onSaved()
            dismiss()
        } catch { self.error = error }
    }
}

private struct GuideEditor: View {
    let client: MediaClient
    let guide: LiveTvGuideSource?
    let onSaved: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var form = GuideForm()
    @State private var result: LiveTvTestResult?
    @State private var testing = false
    @State private var busy = false
    @State private var error: Error?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("livetvAdmin.name", text: $form.name)
                    Picker(selection: $form.fromFile) {
                        Text("livetvAdmin.fromUrl").tag(false)
                        Text("livetvAdmin.fromFile").tag(true)
                    } label: { Text("livetvAdmin.source") }
                        .pickerStyle(.segmented)
                    if form.fromFile {
                        LabeledField(title: "livetvAdmin.guideFile", text: $form.filePath, prompt: "/config/livetv/guide.xml")
                        FieldHint(text: "livetvAdmin.fileHint")
                    } else {
                        LabeledField(title: "livetvAdmin.guideUrl", text: $form.url, prompt: "https://example.com/guide.xml.gz", keyboard: .URL)
                        FieldHint(text: "livetvAdmin.guideUrlHint")
                    }
                }
                Section {
                    LabeledField(title: "livetvAdmin.timezone", text: $form.timezone, prompt: "Europe/Zagreb")
                    FieldHint(text: "livetvAdmin.timezoneHint")
                    Stepper(value: $form.refreshHours, in: 1...168) {
                        Text("livetvAdmin.refreshHours") + Text(verbatim: ": \(form.refreshHours)")
                    }
                    Toggle("livetvAdmin.enabled", isOn: $form.enabled)
                }
                Section {
                    Button { Task { await test() } } label: {
                        HStack {
                            Text("livetvAdmin.test")
                            if testing { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(!form.isValid || testing)
                    TestOutcome(result: result)
                    ErrorLine(error: error)
                }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background)
            .navigationTitle(Text(guide == nil ? LocalizedStringKey("livetvAdmin.addGuide") : LocalizedStringKey("common.edit")))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if busy { ProgressView() } else { Button("save") { Task { await save() } }.disabled(!form.isValid) }
                }
            }
        }
        .presentationBackground(Theme.background)
        .onAppear { if let guide, form == GuideForm() { form = GuideForm(guide) } }
    }

    private func test() async {
        testing = true
        defer { testing = false }
        do {
            result = try await client.testGuide(form.input)
            error = nil
        } catch { self.error = error }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        do {
            if let guide { _ = try await client.updateGuide(guide.id, form.input) } else { _ = try await client.createGuide(form.input) }
            onSaved()
            dismiss()
        } catch { self.error = error }
    }
}
