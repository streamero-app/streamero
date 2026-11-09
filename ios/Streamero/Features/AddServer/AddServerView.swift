import SwiftUI

struct AddServerView: View {
    @Environment(ServerStore.self) private var store
    @Environment(SessionStore.self) private var session
    @State private var address = ""
    @State private var checking = false
    @State private var error: LocalizedStringKey?

    var body: some View {
        VStack(spacing: 24) {
            Spacer()
            VStack(spacing: 8) {
                Image("AppMark").resizable().frame(width: 76, height: 76)
                    .clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
                    .shadow(color: Theme.accent.opacity(0.4), radius: 24, y: 10)
                    .padding(.bottom, 8)
                Text("app.name").font(Theme.display(36)).textCase(.uppercase).minimumScaleFactor(0.6)
                Text("addServer.subtitle").foregroundStyle(Theme.textMuted)
            }
            VStack(alignment: .leading, spacing: 8) {
                Text("addServer.address").font(.subheadline).foregroundStyle(Theme.textMuted)
                TextField("addServer.placeholder", text: $address)
                    .textContentType(.URL)
                    .keyboardType(.URL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .padding(12)
                    .background(Theme.surface, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).stroke(Theme.border))
                    .onSubmit { Task { await connect() } }
                if let error {
                    Text(error).font(.footnote).foregroundStyle(.red)
                }
            }
            Button {
                Task { await connect() }
            } label: {
                Group {
                    if checking { ProgressView() } else { Text("addServer.connect").bold() }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(checking || address.isEmpty)
            Spacer()
            Spacer()
        }
        .padding(24)
        .frame(maxWidth: 480)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background {
            ZStack {
                Theme.background
                RadialGradient(colors: [Theme.accent.opacity(0.18), .clear], center: .bottom, startRadius: 0, endRadius: 420)
            }
            .ignoresSafeArea()
        }
    }

    private func connect() async {
        guard let url = ServerURL.normalize(address) else {
            error = "addServer.error.invalid"
            return
        }
        checking = true
        error = nil
        defer { checking = false }
        do {
            let info = try await SystemClient(baseURL: url).info()
            session.forget(serverID: info.id)
            store.add(SavedServer(id: info.id, name: info.name, url: url, version: info.version))
        } catch ServerCheckError.starting {
            error = "addServer.error.starting"
        } catch ServerCheckError.notStreamero {
            error = "addServer.error.notStreamero"
        } catch {
            self.error = "addServer.error.unreachable"
        }
    }
}
