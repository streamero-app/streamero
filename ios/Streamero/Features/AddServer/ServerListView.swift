import SwiftUI

/// Every server this device knows: switch between them (each keeps its own sign-in in the
/// Keychain), add another, or forget one.
struct ServerListView: View {
    @Environment(ServerStore.self) private var store
    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var adding = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(store.servers) { server in
                        Button {
                            store.select(server)
                            dismiss()
                        } label: {
                            row(server)
                        }
                        .listRowBackground(Theme.surface)
                        .swipeActions(edge: .trailing) {
                            Button(role: .destructive) { remove(server) } label: {
                                Label("server.remove", systemImage: "trash")
                            }
                        }
                    }
                } footer: {
                    Text("servers.hint")
                }
                Section {
                    Button { adding = true } label: {
                        Label { Text("servers.add").foregroundStyle(Theme.text) } icon: { Image(systemName: "plus") }
                    }
                    .listRowBackground(Theme.surface)
                }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background.ignoresSafeArea())
            .navigationTitle(Text("servers.title"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("player.back") { dismiss() } }
            }
            .fullScreenCover(isPresented: $adding) {
                AddServerView {
                    adding = false
                    dismiss()
                }
            }
        }
    }

    private func row(_ server: SavedServer) -> some View {
        HStack(spacing: 14) {
            Image(systemName: "server.rack").font(.title3).foregroundStyle(Theme.accent.opacity(0.85)).frame(width: 28)
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: server.name).font(.body.weight(.semibold)).foregroundStyle(Theme.text).lineLimit(1)
                Text(verbatim: server.url.host() ?? server.url.absoluteString).font(.footnote).foregroundStyle(Theme.textMuted).lineLimit(1)
            }
            Spacer(minLength: 0)
            if server.id == store.current?.id {
                Image(systemName: "checkmark").font(.body.weight(.semibold)).foregroundStyle(Theme.text)
                    .accessibilityLabel(Text("servers.current"))
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }

    /// Forgetting a server also drops its saved sign-in.
    private func remove(_ server: SavedServer) {
        session.forget(serverID: server.id)
        store.remove(server)
    }
}
