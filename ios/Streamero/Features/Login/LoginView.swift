import AuthenticationServices
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
        case "invalid_state": "login.sso.error.state"
        case "provider_error", "internal": "login.sso.error.provider"
        case "no_account", "identity_taken": "login.sso.error.noAccount"
        case "not_allowed": "login.sso.error.notAllowed"
        case "account_disabled": "login.sso.error.disabled"
        case "not_configured", "unknown_provider": "login.sso.error.notConfigured"
        case "password_login_disabled": "login.error.passwordDisabled"
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
    @State private var providers: Components.Schemas.AuthProviders?
    @State private var showServers = false
    @State private var showInvite = false
    @Environment(\.webAuthenticationSession) private var webAuthenticationSession

    var body: some View {
        ScrollView {
            VStack(spacing: 20) {
                VStack(spacing: 6) {
                    Image("AppMark").resizable().frame(width: 60, height: 60)
                        .clipShape(RoundedRectangle(cornerRadius: 15, style: .continuous))
                        .shadow(color: Theme.accent.opacity(0.35), radius: 20, y: 8)
                        .padding(.bottom, 10)
                    Text(server.name).displayFont(32).lineLimit(2).minimumScaleFactor(0.5)
                    Text(server.url.host() ?? server.url.absoluteString).foregroundStyle(Theme.textMuted)
                }
                .padding(.top, 48)

                if quickConnect {
                    QuickConnectView(server: server) { quickConnect = false }
                } else {
                    form
                }

                VStack(spacing: 14) {
                    Button { showServers = true } label: {
                        Label(servers.servers.count > 1 ? LocalizedStringKey("servers.switch") : LocalizedStringKey("servers.add"), systemImage: "server.rack")
                    }
                    .foregroundStyle(Theme.textMuted)
                    Button("server.remove", role: .destructive) {
                        session.forget(serverID: server.id)
                        servers.remove(server)
                    }
                }
                .font(.footnote)
                .padding(.top, 24)
            }
            .padding(24)
            .frame(maxWidth: 480)
            .frame(maxWidth: .infinity)
        }
        .sheet(isPresented: $showServers) { ServerListView() }
        .sheet(isPresented: $showInvite) { InviteView(fallbackServer: server.url) }
        .task(id: server.id) {
            // Older servers have no /auth/providers: they offer passwords only.
            providers = try? await AuthClient(baseURL: server.url).providers()
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
            Text("login.title").displayFont(24)
            ForEach(providers?.oidc ?? [], id: \.id) { provider in
                Button {
                    Task { await signInWithSSO(provider) }
                } label: {
                    Text("login.sso \(provider.label)").bold().frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .controlSize(.large)
                .disabled(busy)
            }
            if providers?.passwordLogin != false {
                field("login.username", text: $username, content: .username)
                SecureField("login.password", text: $password)
                    .textContentType(.password)
                    .modifier(FieldStyle())
                    .onSubmit { Task { await signIn() } }
                if needsCode {
                    Text("login.totpHint").font(.footnote).foregroundStyle(Theme.textMuted)
                    field("login.totp", text: $totpCode, content: .oneTimeCode)
                }
            }
            if let error {
                Text(error).font(.footnote).foregroundStyle(.red)
            }
            if providers?.passwordLogin != false {
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
            }
            Button("quickConnect.use") { quickConnect = true }
                .frame(maxWidth: .infinity)
                .padding(.top, 4)
            Button { showInvite = true } label: { Label("invite.have", systemImage: "envelope.open") }
                .frame(maxWidth: .infinity)
                .foregroundStyle(Theme.textMuted)
        }
    }

    private func field(_ title: LocalizedStringKey, text: Binding<String>, content: UITextContentType) -> some View {
        TextField(title, text: text)
            .textContentType(content)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .modifier(FieldStyle())
    }

    /// Opens the provider in the system web authentication session and redeems the one-time
    /// code it returns. The (default) shared browser session is used on purpose: the provider's own
    /// sign-in (and password managers) carry over, at the cost of the system's
    /// "wants to use … to sign in" prompt. The flow cookie the server sets lives in that
    /// same session, so either choice keeps the sign-in bound to this browser.
    @MainActor
    private func signInWithSSO(_ provider: Components.Schemas.OidcProviderButton) async {
        error = nil
        let verifier = SingleSignOn.makeVerifier()
        guard let url = SingleSignOn.authorizeURL(provider.authorizeUrl, challenge: SingleSignOn.challenge(for: verifier)) else {
            error = "login.sso.error.notConfigured"
            return
        }
        busy = true
        defer { busy = false }
        do {
            let callback = try await webAuthenticationSession.authenticate(
                using: url, callbackURLScheme: SingleSignOn.callbackScheme) // default: shared browser session
            switch SingleSignOn.parseCallback(callback) {
            case .code(let code):
                let (token, _) = try await AuthClient(baseURL: server.url).exchangeOIDC(code: code, verifier: verifier)
                session.signedIn(server: server, token: token)
            case .failed(let code):
                error = LoginMessage.key(for: code)
            case nil:
                error = "login.sso.error.provider"
            }
        } catch let authError as ASWebAuthenticationSessionError where authError.code == .canceledLogin {
            return // the user closed the sheet
        } catch let apiError as APIError {
            error = LoginMessage.key(for: apiError.code)
        } catch {
            self.error = "addServer.error.unreachable"
        }
    }

    private func signIn() async {
        busy = true
        error = nil
        defer { busy = false }
        do {
            let (token, _) = try await AuthClient(baseURL: server.url)
                .login(username: username, password: password, totpCode: needsCode ? totpCode : nil)
            session.signedIn(server: server, token: token)
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
    /// The running request-and-poll; one at a time, stopped when the view goes away.
    @State private var poll: Task<Void, Never>?

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
                Button("quickConnect.newCode") { start() }
            }
            Button("quickConnect.usePassword", action: onCancel)
        }
        .onAppear { start() }
        .onDisappear {
            poll?.cancel()
            poll = nil
        }
    }

    private func start() {
        poll?.cancel()
        poll = Task { await run() }
    }

    private func run() async {
        error = nil
        let client = AuthClient(baseURL: server.url)
        do {
            let started = try await client.initiateQuickConnect()
            code = started.code
            while !Task.isCancelled {
                try await Task.sleep(for: .seconds(3))
                if case .signedIn(let token, _) = try await client.pollQuickConnect(secret: started.secret) {
                    guard !Task.isCancelled else { return }
                    session.signedIn(server: server, token: token)
                    return
                }
            }
        } catch is CancellationError {
            return
        } catch let apiError as APIError {
            if Task.isCancelled { return }
            code = nil
            error = LoginMessage.key(for: apiError.code)
        } catch {
            code = nil
            self.error = "addServer.error.unreachable"
        }
    }
}
