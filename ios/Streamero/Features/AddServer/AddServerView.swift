import SwiftUI

struct AddServerView: View {
    @Environment(ServerStore.self) private var store
    @State private var address = ""
    @State private var checking = false
    @State private var error: LocalizedStringKey?

    var body: some View {
        VStack(spacing: 24) {
            Spacer()
            VStack(spacing: 8) {
                Image(systemName: "play.rectangle.fill")
                    .font(.system(size: 56))
                    .foregroundStyle(Theme.accent)
                Text("app.name").font(.largeTitle.bold())
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
                    .background(Theme.surface, in: RoundedRectangle(cornerRadius: 10))
                    .overlay(RoundedRectangle(cornerRadius: 10).stroke(Theme.border))
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
                .padding(.vertical, 12)
            }
            .buttonStyle(.borderedProminent)
            .disabled(checking || address.isEmpty)
            Spacer()
            Spacer()
        }
        .padding(24)
        .frame(maxWidth: 480)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.background)
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
