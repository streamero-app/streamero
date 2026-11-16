import SwiftUI

/// Edits several movies, series or artists at once (`PUT {items}/editor`). Every setting
/// starts as "No change"; only what is picked is sent.
struct ArrBulkEditView: View {
    let client: ArrClient
    let ids: [Int]
    @State private var edit = ArrBulkEdit()
    @State private var changeTags = false
    @State private var tagSelection: [JSONValue] = []
    @State private var profiles: [JSONValue] = []
    @State private var metadataProfiles: [JSONValue] = []
    @State private var rootFolders: [JSONValue] = []
    @State private var tags: [JSONValue] = []
    @State private var error: Error?
    @State private var busy = false
    @Environment(\.dismiss) private var dismiss

    private var flavor: ArrFlavor { ArrFlavor(client.service) }

    var body: some View {
        Form {
            Section {
                Text("manage.bulk_selected \(ids.count)").font(.subheadline).foregroundStyle(Theme.textMuted)
            }
            .listRowBackground(Color.clear)
            Section {
                Picker(selection: $edit.monitored) {
                    Text("manage.no_change").tag(Bool?.none)
                    Text("manage.filter.monitored").tag(Bool?.some(true))
                    Text("manage.filter.unmonitored").tag(Bool?.some(false))
                } label: { Text("manage.monitoring") }
                Picker(selection: $edit.qualityProfileId) {
                    Text("manage.no_change").tag(Int?.none)
                    ForEach(profiles, id: \.rowID) { p in Text(verbatim: p["name"].text).tag(p["id"].int) }
                } label: { Text("manage.quality_profile") }
                if client.service == .lidarr {
                    Picker(selection: $edit.metadataProfileId) {
                        Text("manage.no_change").tag(Int?.none)
                        ForEach(metadataProfiles, id: \.rowID) { p in Text(verbatim: p["name"].text).tag(p["id"].int) }
                    } label: { Text("manage.metadata_profile") }
                }
                if client.service == .radarr {
                    Picker(selection: $edit.minimumAvailability) {
                        Text("manage.no_change").tag(String?.none)
                        ForEach(["announced", "inCinemas", "released"], id: \.self) { v in Text(verbatim: v.humanizedKey).tag(String?.some(v)) }
                    } label: { Text("manage.minimum_availability") }
                }
                if client.service == .sonarr {
                    Picker(selection: $edit.seriesType) {
                        Text("manage.no_change").tag(String?.none)
                        ForEach(["standard", "daily", "anime"], id: \.self) { v in Text(verbatim: v.humanizedKey).tag(String?.some(v)) }
                    } label: { Text("manage.series_type") }
                    Picker(selection: $edit.seasonFolder) {
                        Text("manage.no_change").tag(Bool?.none)
                        Text("manage.yes").tag(Bool?.some(true))
                        Text("manage.no").tag(Bool?.some(false))
                    } label: { Text("manage.season_folder") }
                }
            }
            Section {
                Picker(selection: $edit.rootFolderPath) {
                    Text("manage.no_change").tag(String?.none)
                    ForEach(rootFolders, id: \.rowID) { f in Text(verbatim: f["path"].text).tag(f["path"].string) }
                } label: { Text("manage.root_folder") }
                if edit.rootFolderPath != nil {
                    Toggle(isOn: $edit.moveFiles) { Text("manage.move_files") }
                    if edit.moveFiles {
                        Text("manage.path_moves_files").font(.caption).foregroundStyle(Theme.textFaint)
                    }
                }
            }
            Section {
                Toggle(isOn: $changeTags.animation(.snappy)) { Text("manage.change_tags") }
                if changeTags {
                    Picker(selection: $edit.tagMode) {
                        Text("manage.tags_add").tag(ArrTagMode.add)
                        Text("manage.tags_remove").tag(ArrTagMode.remove)
                        Text("manage.tags_replace").tag(ArrTagMode.replace)
                    } label: { EmptyView() }
                    .pickerStyle(.segmented)
                    MultiSelectRow(title: Loc.string("manage.tags"), options: tagOptions(tags), selection: $tagSelection) {
                        Text("manage.tags")
                    }
                }
            }
            if let error { Section { ArrErrorLine(error: error) } }
        }
        .manageBackground()
        .navigationTitle(Text("manage.bulk_edit"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
            ToolbarItem(placement: .confirmationAction) {
                Button { Task { await save() } } label: { if busy { ProgressView() } else { Text("save") } }
                    .disabled(busy || outgoing.isEmpty)
            }
        }
        .task {
            profiles = (try? await client.get("qualityprofile").list) ?? []
            rootFolders = (try? await client.get("rootfolder").list) ?? []
            tags = (try? await client.get("tag").list) ?? []
            if client.service == .lidarr { metadataProfiles = (try? await client.get("metadataprofile").list) ?? [] }
        }
    }

    private var outgoing: ArrBulkEdit {
        var out = edit
        out.tags = changeTags ? tagSelection.compactMap(\.int) : nil
        return out
    }

    private func save() async {
        busy = true
        defer { busy = false }
        do {
            try await client.send("PUT", "\(flavor.items)/editor", json: outgoing.body(service: client.service, ids: ids))
            dismiss()
        } catch { self.error = error }
    }
}

/// The bar under a library in selection mode.
struct ArrSelectionBar: View {
    let count: Int
    let allSelected: Bool
    let toggleAll: () -> Void
    let edit: () -> Void
    let delete: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Button(action: toggleAll) {
                Text(allSelected ? LocalizedStringKey("manage.select_none") : LocalizedStringKey("manage.select_all"))
                    .font(.subheadline.weight(.semibold))
            }
            Spacer()
            Text("manage.bulk_selected \(count)").font(.subheadline.monospacedDigit()).foregroundStyle(Theme.textMuted)
                .contentTransition(.numericText())
            Spacer()
            Button(action: edit) { Image(systemName: "slider.horizontal.3").font(.title3) }
                .accessibilityLabel(Text("manage.bulk_edit"))
                .disabled(count == 0)
            Button(role: .destructive, action: delete) { Image(systemName: "trash").font(.title3) }
                .accessibilityLabel(Text("manage.delete"))
                .disabled(count == 0)
                .tint(.red)
        }
        .tint(Theme.accent)
        .padding(.horizontal, 18).padding(.vertical, 12)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous).stroke(Theme.border))
        .padding(.horizontal, 12).padding(.bottom, 8)
        .animation(.snappy, value: count)
    }
}
