import SwiftUI

/// Bazarr's subtitle languages: which languages are enabled and the language profiles that
/// series and movies use. Read from `system/languages` and `system/languages/profiles`,
/// saved with the settings form (`languages-enabled`, `languages-profiles`).
struct BazarrLanguagesView: View {
    let client: ArrClient
    @State private var languages: [BazarrLanguage] = []
    @State private var enabled: Set<String> = []
    @State private var originalEnabled: Set<String> = []
    @State private var profiles: [BazarrLanguageProfile] = []
    @State private var originalProfiles: [BazarrLanguageProfile] = []
    @State private var loaded = false
    @State private var error: Error?
    @State private var busy = false
    @State private var notice: ManageNotice?
    @State private var pendingDelete: BazarrLanguageProfile?

    private var hasChanges: Bool { enabled != originalEnabled || profiles != originalProfiles }

    var body: some View {
        Form {
            if loaded {
                Section {
                    NavigationLink {
                        BazarrLanguagePicker(languages: languages, mode: .multiple($enabled))
                    } label: {
                        LabeledContent {
                            Text(verbatim: "\(enabled.count)").foregroundStyle(Theme.textMuted).monospacedDigit()
                        } label: {
                            VStack(alignment: .leading, spacing: 4) {
                                Text("manage.languages_enabled")
                                Text(verbatim: enabledNames.isEmpty ? "—" : enabledNames)
                                    .font(.caption).foregroundStyle(Theme.textFaint).lineLimit(2)
                            }
                        }
                    }
                    if enabled.isEmpty {
                        Text("manage.languages_need_one").font(.caption).foregroundStyle(.orange)
                    }
                } footer: { Text("manage.languages_enabled_hint") }

                Section {
                    ForEach($profiles) { $profile in
                        NavigationLink {
                            BazarrProfileEditor(profile: $profile, languages: languages, enabled: $enabled)
                        } label: {
                            VStack(alignment: .leading, spacing: 4) {
                                Text(verbatim: profile.name.nilIfBlank ?? "#\(profile.id)").font(.body.weight(.semibold))
                                Text(verbatim: summary(profile)).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(2)
                            }
                        }
                        .swipeActions {
                            Button(role: .destructive) { pendingDelete = profile } label: { Label("manage.delete", systemImage: "trash") }
                        }
                    }
                    if profiles.isEmpty { EmptyRow(text: "manage.empty") }
                    Button {
                        withAnimation(.snappy) {
                            profiles.append(.new(name: Loc.string("manage.new_profile"), after: profiles))
                        }
                    } label: { Label("manage.add_profile", systemImage: "plus.circle.fill") }
                } header: { Text("manage.language_profiles") }

                Section {
                    ArrErrorLine(error: error)
                    Button { Task { await save() } } label: {
                        HStack { Text("save"); Spacer(); if busy { ProgressView() } }
                    }
                    .disabled(busy || !hasChanges || enabled.isEmpty)
                }
            } else if let error {
                ArrErrorState(error: error) { await load() }
            } else {
                LoadingView()
            }
        }
        .manageBackground()
        .navigationTitle(Text("manage.languages"))
        .navigationBarTitleDisplayMode(.inline)
        .task { if !loaded { await load() } }
        .manageNotice($notice)
        .confirmationDialog("manage.delete_confirm", isPresented: Binding(get: { pendingDelete != nil }, set: { if !$0 { pendingDelete = nil } }), titleVisibility: .visible) {
            Button("manage.delete", role: .destructive) {
                if let p = pendingDelete { withAnimation { profiles.removeAll { $0.id == p.id } } }
            }
        } message: {
            Text(verbatim: pendingDelete?.name ?? "")
        }
    }

    private var enabledNames: String {
        languages.filter { enabled.contains($0.code2) }.map(\.name).joined(separator: ", ")
    }

    private func summary(_ p: BazarrLanguageProfile) -> String {
        p.items.map { item in
            let code = item["language"].text
            let name = languages.first { $0.code2 == code }?.name ?? code
            switch BazarrLanguageProfile.kind(of: item) {
            case .forced: return name + " (" + Loc.string("manage.forced") + ")"
            case .hearingImpaired: return name + " (HI)"
            case .normal: return name
            }
        }.joined(separator: ", ")
    }

    private func load() async {
        do {
            let list = try await client.get("system/languages").list.map(BazarrLanguage.init(json:))
            let profs = try await client.get("system/languages/profiles").list.map(BazarrLanguageProfile.init)
            languages = list.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
            enabled = Set(BazarrLanguageSettings.enabledCodes(list))
            originalEnabled = enabled
            profiles = profs
            originalProfiles = profs
            loaded = true
            error = nil
        } catch { self.error = error }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        // Languages used by a profile must be enabled, or Bazarr ignores those items.
        let used = Set(profiles.flatMap { $0.items.map { $0["language"].text } }).filter { !$0.isEmpty }
        let allEnabled = enabled.union(used)
        let order = languages.map(\.code2)
        let codes = allEnabled.sorted { (order.firstIndex(of: $0) ?? .max) < (order.firstIndex(of: $1) ?? .max) }
        let fields = BazarrLanguageSettings.formFields(
            enabled: allEnabled != originalEnabled ? codes : nil,
            profiles: profiles != originalProfiles ? profiles : nil)
        do {
            try await client.sendForm("POST", "system/settings", fields: fields)
            notice = ManageNotice(text: "saved")
            await load()
        } catch { self.error = error }
    }
}

/// One language profile: name, languages in order of preference, cutoff and release filters.
struct BazarrProfileEditor: View {
    @Binding var profile: BazarrLanguageProfile
    let languages: [BazarrLanguage]
    @Binding var enabled: Set<String>

    var body: some View {
        Form {
            Section {
                VStack(alignment: .leading, spacing: 4) {
                    Text("manage.name").font(.caption).foregroundStyle(Theme.textMuted)
                    TextField(text: $profile.name) { Text("manage.name") }
                }
            }
            Section {
                ForEach(Array(profile.items.enumerated()), id: \.offset) { index, item in
                    itemRow(index, item)
                }
                .onDelete { profile.removeItems(at: $0) }
                .onMove { profile.moveItems(from: $0, to: $1) }
                NavigationLink {
                    BazarrLanguagePicker(languages: languages, mode: .single({ code in
                        profile.addItem(language: code)
                        enabled.insert(code)
                    }), enabledFirst: enabled)
                } label: { Label("manage.add_language", systemImage: "plus.circle.fill") }
            } header: {
                Text("manage.profile_languages")
            } footer: { Text("manage.profile_languages_hint") }
            Section {
                Picker(selection: $profile.cutoff) {
                    Text("manage.cutoff_none").tag(Int?.none)
                    ForEach(profile.items.compactMap { $0["id"].int }, id: \.self) { id in
                        Text(verbatim: itemLabel(profile.items.first { $0["id"].int == id } ?? .null)).tag(Int?.some(id))
                    }
                } label: { Text("manage.cutoff") }
                Toggle(isOn: $profile.originalFormat) { Text("manage.original_format") }
            }
            Section {
                StringListRow(title: Loc.string("manage.must_contain"), values: $profile.mustContain)
                StringListRow(title: Loc.string("manage.must_not_contain"), values: $profile.mustNotContain)
                VStack(alignment: .leading, spacing: 4) {
                    Text("manage.profile_tag").font(.caption).foregroundStyle(Theme.textMuted)
                    TextField(text: $profile.tag) { Text("manage.profile_tag") }
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                }
            } header: { Text("manage.release_filters") }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: profile.name.nilIfBlank ?? Loc.string("manage.language_profile")))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { if profile.items.count > 1 { ToolbarItem(placement: .primaryAction) { EditButton() } } }
    }

    private func name(_ code: String) -> String { languages.first { $0.code2 == code }?.name ?? code }

    private func itemLabel(_ item: JSONValue) -> String {
        let base = name(item["language"].text)
        switch BazarrLanguageProfile.kind(of: item) {
        case .forced: return base + " (" + Loc.string("manage.forced") + ")"
        case .hearingImpaired: return base + " (HI)"
        case .normal: return base
        }
    }

    private func itemBinding(_ index: Int) -> Binding<JSONValue> {
        Binding(get: { profile.items.indices.contains(index) ? profile.items[index] : .null }, set: { new in
            var list = profile.items
            guard list.indices.contains(index) else { return }
            list[index] = new
            profile.items = list
        })
    }

    @ViewBuilder private func itemRow(_ index: Int, _ item: JSONValue) -> some View {
        let binding = itemBinding(index)
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(verbatim: name(item["language"].text)).font(.body.weight(.semibold))
                if profile.cutoff != nil, profile.cutoff == item["id"].int {
                    PlainBadge(text: Loc.string("manage.cutoff"), tone: .accent)
                }
                Spacer()
                Picker(selection: Binding(get: { BazarrLanguageProfile.kind(of: binding.wrappedValue) }, set: { kind in
                    var v = binding.wrappedValue
                    BazarrLanguageProfile.setKind(kind, on: &v)
                    binding.wrappedValue = v
                })) {
                    Text("manage.subs.normal").tag(BazarrSubtitleKind.normal)
                    Text("manage.subs.hi").tag(BazarrSubtitleKind.hearingImpaired)
                    Text("manage.subs.forced").tag(BazarrSubtitleKind.forced)
                } label: { EmptyView() }
                .pickerStyle(.menu)
                .tint(Theme.accent)
            }
            Toggle(isOn: Binding(get: { BazarrFlag.isOn(binding.wrappedValue["audio_exclude"]) }, set: { on in
                var v = binding.wrappedValue
                v["audio_exclude"] = BazarrFlag.value(on, like: v["audio_exclude"])
                binding.wrappedValue = v
            })) {
                Text("manage.audio_exclude").font(.subheadline).foregroundStyle(Theme.textMuted)
            }
            if !item["audio_only_include"].isNull {
                Toggle(isOn: Binding(get: { BazarrFlag.isOn(binding.wrappedValue["audio_only_include"]) }, set: { on in
                    var v = binding.wrappedValue
                    v["audio_only_include"] = BazarrFlag.value(on, like: v["audio_only_include"])
                    binding.wrappedValue = v
                })) {
                    Text("manage.audio_only_include").font(.subheadline).foregroundStyle(Theme.textMuted)
                }
            }
        }
        .padding(.vertical, 2)
    }
}

/// A searchable list of Bazarr's languages: pick several (enabled languages) or one (a
/// profile item). Enabled languages come first.
struct BazarrLanguagePicker: View {
    enum Mode {
        case multiple(Binding<Set<String>>)
        case single(@MainActor (String) -> Void)
    }

    let languages: [BazarrLanguage]
    let mode: Mode
    var enabledFirst: Set<String> = []
    @State private var query = ""
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        List {
            let top = filtered.filter { highlighted.contains($0.code2) }
            let rest = filtered.filter { !highlighted.contains($0.code2) }
            if !top.isEmpty {
                Section { ForEach(top) { row($0) } } header: { Text("manage.languages_enabled") }
            }
            Section { ForEach(rest) { row($0) } } header: { if !top.isEmpty { Text("manage.languages_all") } }
            if filtered.isEmpty { EmptyRow(text: "manage.no_results") }
        }
        .manageBackground()
        .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always))
        .onAppear {
            guard !didSetHighlight else { return }
            didSetHighlight = true
            switch mode {
            case .multiple(let selection): highlighted = selection.wrappedValue
            case .single: highlighted = enabledFirst
            }
        }
        .navigationTitle(Text("manage.languages"))
        .navigationBarTitleDisplayMode(.inline)
    }

    /// Fixed when the screen opens, so ticking a language does not move it around.
    @State private var highlighted: Set<String> = []
    @State private var didSetHighlight = false

    private var filtered: [BazarrLanguage] {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { return languages }
        return languages.filter {
            $0.name.localizedCaseInsensitiveContains(q) || $0.code2.caseInsensitiveCompare(q) == .orderedSame
                || $0.code3.caseInsensitiveCompare(q) == .orderedSame
        }
    }

    private func row(_ language: BazarrLanguage) -> some View {
        Button {
            switch mode {
            case .multiple(let selection):
                if selection.wrappedValue.contains(language.code2) {
                    selection.wrappedValue.remove(language.code2)
                } else {
                    selection.wrappedValue.insert(language.code2)
                }
            case .single(let pick):
                pick(language.code2)
                dismiss()
            }
        } label: {
            HStack {
                Text(verbatim: language.name).foregroundStyle(Theme.text)
                Text(verbatim: language.code2).font(.caption.monospaced()).foregroundStyle(Theme.textFaint)
                Spacer()
                if case .multiple(let selection) = mode, selection.wrappedValue.contains(language.code2) {
                    Image(systemName: "checkmark").foregroundStyle(Theme.accent).fontWeight(.semibold)
                }
            }
            .contentShape(Rectangle())
        }
        .sensoryFeedback(.selection, trigger: isSelected(language))
    }

    private func isSelected(_ language: BazarrLanguage) -> Bool {
        if case .multiple(let selection) = mode { return selection.wrappedValue.contains(language.code2) }
        return false
    }
}
