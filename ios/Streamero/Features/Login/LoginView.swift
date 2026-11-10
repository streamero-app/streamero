import SwiftUI

/// Maps API error codes to localized messages (same codes as the web client).
enum LoginMessage {
    static func key(for code: String) -> LocalizedStringKey {
        switch code {
        case "invalid_credentials": "login.error.credentials"
        case "account_locked": "login.error.locked"
        case "invalid_totp": "login.error.totp"
        case "rate_limited": "login.error.rateLimited"
        case "not_found": "quickConnect.expired"
        default: "login.error.generic"
        }
    }
}

struct LoginView: View {
    let server: SavedServer
    @Environment(SessionStore.self) private var session
    @Environment(ServerStore.self) private var servers
    @State private var username = ""
    @State private var password = ""
    @State private var totpCode = ""
    @State private var needsCode = false
    @State private var busy = false
    @State private var error: LocalizedStringKey?
    @State private var quickConnect = false

    var body: some View {
        ScrollView {
            VStack(spacing: 20) {
                VStack(spacing: 6) {
                    Image("AppMark").resizable().frame(width: 60, height: 60)
                        .clipShape(RoundedRectangle(cornerRadius: 15, style: .continuous))
                        .shadow(color: Theme.accent.opacity(0.35), radius: 20, y: 8)
                        .padding(.bottom, 10)
                    Text(server.name).font(Theme.display(32)).lineLimit(2).minimumScaleFactor(0.5)
                    Text(server.url.host() ?? server.url.absoluteString).foregroundStyle(Theme.textMuted)
                }
                .padding(.top, 48)

                if quickConnect {
                    QuickConnectView(server: server) { quickConnect = false }
                } else {
                    form
                }

                Button("server.remove", role: .destructive) { servers.remove(server) }
                    .font(.footnote)
                    .padding(.top, 24)
            }
            .padding(24)
            .frame(maxWidth: 480)
            .frame(maxWidth: .infinity)
        }
        .background {
            ZStack {
                Theme.background
                RadialGradient(colors: [Theme.accent.opacity(0.16), .clear], center: .bottom, startRadius: 0, endRadius: 420)
            }
            .ignoresSafeArea()
        }
    }

    private var form: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("login.title").font(Theme.display(24))
            field("login.username", text: $username, content: .username)
            SecureField("login.password", text: $password)
                .textContentType(.password)
                .modifier(FieldStyle())
                .onSubmit { Task { await signIn() } }
            if needsCode {
                Text("login.totpHint").font(.footnote).foregroundStyle(Theme.textMuted)
                field("login.totp", text: $totpCode, content: .oneTimeCode)
            }
            if let error {
                Text(error).font(.footnote).foregroundStyle(.red)
            }
            Button {
                Task { await signIn() }
            } label: {
                Group {
                    if busy { ProgressView() } else { Text("login.submit").bold() }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(busy || username.isEmpty || password.isEmpty)
            Button("quickConnect.use") { quickConnect = true }
                .frame(maxWidth: .infinity)
                .padding(.top, 4)
        }
    }

    private func field(_ title: LocalizedStringKey, text: Binding<String>, content: UITextContentType) -> some View {
        TextField(title, text: text)
            .textContentType(content)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .modifier(FieldStyle())
    }

    private func signIn() async {
        busy = true
        error = nil
        defer { busy = false }
        do {
            let (token, user) = try await AuthClient(baseURL: server.url)
                .login(username: username, password: password, totpCode: needsCode ? totpCode : nil)
            session.signedIn(server: server, token: token, user: user)
        } catch let apiError as APIError where apiError.code == "totp_required" {
            needsCode = true
        } catch let apiError as APIError {
            error = LoginMessage.key(for: apiError.code)
        } catch {
            self.error = "addServer.error.unreachable"
        }
    }
}

struct FieldStyle: ViewModifier {
    func body(content: Content) -> some View {
        content
            .padding(12)
            .background(Theme.surface, in: RoundedRectangle(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).stroke(Theme.border))
    }
}

/// Shows a code to approve on a signed-in device and polls until approved.
struct QuickConnectView: View {
    let server: SavedServer
    let onCancel: () -> Void
    @Environment(SessionStore.self) private var session
    @State private var code: String?
    @State private var error: LocalizedStringKey?

    var body: some View {
        VStack(spacing: 16) {
            Text("quickConnect.intro").multilineTextAlignment(.center).foregroundStyle(Theme.textMuted)
            if let code {
                Text(code)
                    .font(.system(size: 44, weight: .semibold, design: .monospaced))
                    .tracking(8)
                    .accessibilityLabel(Text("quickConnect.code"))
                ProgressView("quickConnect.waiting")
            }
            if let error {
                Text(error).font(.footnote).foregroundStyle(.red)
                Button("quickConnect.newCode") { Task { await run() } }
            }
            Button("quickConnect.usePassword", action: onCancel)
        }
        .task { await run() }
    }

    private func run() async {
        error = nil
        let client = AuthClient(baseURL: server.url)
        do {
            let start = try await client.initiateQuickConnect()
            code = start.code
            while !Task.isCancelled {
                try await Task.sleep(for: .seconds(3))
                if case .signedIn(let token, let user) = try await client.pollQuickConnect(secret: start.secret) {
                    session.signedIn(server: server, token: token, user: user)
                    return
                }
            }
        } catch is CancellationError {
            return
        } catch let apiError as APIError {
            code = nil
            error = LoginMessage.key(for: apiError.code)
        } catch {
            code = nil
            self.error = "addServer.error.unreachable"
        }
    }
}
