import PhotosUI
import SwiftUI

/// Profiles that share this account: add, edit (name, colour, picture, PIN, kids limit), delete.
/// Kids profiles cannot get here (SettingsPageView) and the server refuses them too.
struct ProfilesPage: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var profiles: [Profile]?
    @State private var error: Error?
    @State private var editing: ProfileEditTarget?
    @State private var deleting: Profile?
    /// Bumped after a picture change so the avatars reload.
    @State private var pictureVersion = 0

    var body: some View {
        List {
            Section { IntroText(text: "profiles.intro") }
            if let error { Section { ErrorLine(error: error) } }
            if profiles == nil && error == nil { LoadingView().listRowBackground(Color.clear) }
            if let profiles {
                Section {
                    if profiles.isEmpty {
                        Text("profiles.none").foregroundStyle(Theme.textMuted)
                    }
                    ForEach(profiles) { p in
                        Button { editing = ProfileEditTarget(profile: p) } label: { row(p) }
                            .swipeActions {
                                Button("delete", role: .destructive) { deleting = p }
                            }
                            .contextMenu {
                                Button("profiles.edit", systemImage: "pencil") { editing = ProfileEditTarget(profile: p) }
                                Button("delete", systemImage: "trash", role: .destructive) { deleting = p }
                            }
                    }
                } footer: {
                    if !ProfileForm.canAdd(existing: profiles.count) {
                        Text("profiles.limitReached \(ProfileForm.maxProfiles)")
                    }
                }
                if ProfileForm.canAdd(existing: profiles.count) {
                    Section {
                        Button { editing = ProfileEditTarget(profile: nil) } label: {
                            Label("profiles.add", systemImage: "plus")
                        }
                    }
                }
            }
        }
        .sheet(item: $editing) { target in
            ProfileEditor(client: client, profile: target.profile) { changedPicture in
                if changedPicture { pictureVersion += 1 }
                Task { await load() }
            }
            .environment(\.locale, app.locale ?? Locale.current)
        }
        .confirmationDialog(Text(verbatim: deleting.map { String(format: Loc.string("profiles.confirmDelete %@"), $0.name) } ?? ""),
                            isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            titleVisibility: .visible, presenting: deleting) { p in
            Button("delete", role: .destructive) {
                Task {
                    do {
                        try await client.deleteProfile(p.id)
                        await load()
                    } catch { self.error = error }
                }
            }
        }
        .refreshable { await load() }
        .task { await load() }
    }

    private func row(_ p: Profile) -> some View {
        HStack(spacing: 14) {
            AvatarView(name: p.name, color: p.color, url: avatarURL(p), token: client.token, size: 52)
            VStack(alignment: .leading, spacing: 6) {
                Text(verbatim: p.name).font(.body.weight(.semibold)).foregroundStyle(Theme.text)
                HStack(spacing: 6) {
                    if p.isKid { TagBadge(text: "profiles.kid", tone: .accent) }
                    if p.hasPin { TagBadge(text: "profiles.pinProtected") }
                    if let r = p.maxRating { TagBadge(text: "profiles.upTo \(r)") }
                }
            }
            Spacer()
            Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(Theme.textFaint)
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }

    private func avatarURL(_ p: Profile) -> URL? {
        guard p.hasAvatar == true, let base = client.avatarURL(owner: p.id) else { return nil }
        return pictureVersion == 0 ? base : URL(string: base.absoluteString + "?v=\(pictureVersion)")
    }

    private func load() async {
        do {
            let list = try await client.profiles()
            profiles = list
            app.profiles = list
            error = nil
        } catch is CancellationError { return } catch { self.error = error }
    }
}

/// Which profile the editor sheet is for (nil = a new one).
struct ProfileEditTarget: Identifiable {
    let profile: Profile?
    var id: String { profile?.id ?? "new" }
}

private struct ProfileEditor: View {
    let client: MediaClient
    let profile: Profile?
    let onSaved: (_ changedPicture: Bool) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var form = ProfileForm()
    @State private var busy = false
    @State private var error: Error?
    @State private var photo: PhotosPickerItem?
    @State private var changedPicture = false
    @State private var hasAvatar = false
    @State private var pictureVersion = 0

    private var pictureURL: URL? {
        guard hasAvatar, let profile, let base = client.avatarURL(owner: profile.id) else { return nil }
        return pictureVersion == 0 ? base : URL(string: base.absoluteString + "?v=\(pictureVersion)")
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    HStack {
                        Spacer()
                        AvatarView(name: form.name.isEmpty ? "?" : form.name, color: form.color,
                                   url: pictureURL,
                                   token: client.token, size: 88)
                        Spacer()
                    }
                    .listRowBackground(Color.clear)
                    if profile != nil {
                        HStack {
                            PhotosPicker(selection: $photo, matching: .images) { Text("change_picture") }
                            Spacer()
                            if hasAvatar {
                                Button("remove_picture", role: .destructive) { Task { await removePicture() } }
                            }
                        }
                        .buttonStyle(.borderless)
                    }
                }
                Section {
                    TextField("profiles.name", text: $form.name)
                    if form.problem == .name && !form.name.isEmpty {
                        FieldHint(text: "profiles.nameHint")
                    }
                    VStack(alignment: .leading, spacing: 10) {
                        Text("profiles.color").font(.caption).foregroundStyle(Theme.textMuted)
                        HStack(spacing: 10) {
                            ForEach(ProfileForm.colors, id: \.self) { c in
                                Button { form.color = c } label: {
                                    RoundedRectangle(cornerRadius: 6, style: .continuous)
                                        .fill(Color(hex: c))
                                        .frame(width: 30, height: 30)
                                        .overlay {
                                            if form.color.lowercased() == c {
                                                RoundedRectangle(cornerRadius: 6, style: .continuous).stroke(Theme.text, lineWidth: 2).padding(-3)
                                            }
                                        }
                                }
                                .buttonStyle(.plain)
                                .accessibilityLabel(Text(verbatim: c))
                                .accessibilityAddTraits(form.color.lowercased() == c ? .isSelected : [])
                            }
                        }
                    }
                    .padding(.vertical, 4)
                }
                Section {
                    Toggle("profiles.kidProfile", isOn: $form.isKid)
                    Picker(selection: $form.maxRating) {
                        Text("profiles.noLimit").tag(Int?.none)
                        ForEach(ProfileForm.ratings, id: \.self) { r in Text("profiles.upTo \(r)").tag(Int?.some(r)) }
                    } label: { Text("profiles.maxRating") }
                }
                Section {
                    SecureField("profiles.pinOptional", text: $form.pin)
                        .keyboardType(.numberPad)
                        .textContentType(.oneTimeCode)
                        .disabled(form.removePin)
                        .onChange(of: form.pin) { _, v in
                            let digits = String(v.filter(\.isNumber).prefix(6))
                            if digits != v { form.pin = digits }
                        }
                    if profile?.hasPin == true {
                        Toggle("profiles.removePin", isOn: $form.removePin)
                            .onChange(of: form.removePin) { _, on in if on { form.pin = "" } }
                    }
                } footer: {
                    Text(form.problem == .pin ? LocalizedStringKey("profiles.pinInvalid") : LocalizedStringKey("profiles.pinHint"))
                        .foregroundStyle(form.problem == .pin ? .red : Theme.textFaint)
                }
                if let error { Section { ErrorLine(error: error) } }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background)
            .navigationTitle(Text(profile == nil ? LocalizedStringKey("profiles.add") : LocalizedStringKey("profiles.edit")))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if busy { ProgressView() } else {
                        Button("save") { Task { await save() } }.disabled(form.problem != nil)
                    }
                }
            }
            .onChange(of: photo) { _, item in
                guard let item else { return }
                Task { await upload(item) }
            }
        }
        .presentationBackground(Theme.background)
        .onAppear {
            if let profile {
                form = ProfileForm(profile)
                hasAvatar = profile.hasAvatar == true
            }
        }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        do {
            if let profile {
                _ = try await client.updateProfile(profile.id, form.input)
            } else {
                _ = try await client.createProfile(form.input)
            }
            onSaved(changedPicture)
            dismiss()
        } catch { self.error = error }
    }

    private func upload(_ item: PhotosPickerItem) async {
        guard let profile else { return }
        do {
            guard let data = try await item.loadTransferable(type: Data.self), let jpeg = AvatarImage.jpeg(from: data) else { return }
            try await client.uploadProfileAvatar(profile.id, jpeg: jpeg)
            hasAvatar = true
            pictureVersion += 1
            changedPicture = true
            onSaved(true)
            error = nil
        } catch { self.error = error }
    }

    private func removePicture() async {
        guard let profile else { return }
        do {
            try await client.removeProfileAvatar(profile.id)
            hasAvatar = false
            changedPicture = true
            onSaved(true)
            error = nil
        } catch { self.error = error }
    }
}
