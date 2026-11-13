import SwiftUI
import UIKit

/// "I have an invite": paste an invite link (or token), check it, then create an account and
/// sign in. The server is added like any other and the session is stored like a normal sign-in.
struct InviteView: View {
    /// A link opened from outside (streamero://invite…) or typed earlier.
    var initial: InviteLink.Parsed? = nil
    /// The server on screen (sign-in page), used when only a token is pasted.
    var fallbackServer: URL? = nil
    @Environment(\.dismiss) private var dismiss
    @Environment(ServerStore.self) private var servers
    @Environment(SessionStore.self) private var session
    @State private var link = ""
    @State private var address = ""
    @State private var checking = false
    @State private var problem: LocalizedStringKey?
    @State private var invite: CheckedInvite?
    @State private var form = InviteSignUpForm()
    @State private var showProblems = false
    @State private var busy = false
    @State private var signUpError: Error?

    /// A verified invite and the server it belongs to.
    private struct CheckedInvite {
        let server: SavedServer
        let token: String
        let serverName: String
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    if let invite { signUp(invite) } else { enter }
                }
                .padding(24)
                .frame(maxWidth: 480)
                .frame(maxWidth: .infinity)
            }
            .scrollDismissesKeyboard(.interactively)
            .background {
                ZStack {
                    Theme.background
                    RadialGradient(colors: [Theme.accent.opacity(0.16), .clear], center: .bottom, startRadius: 0, endRadius: 420)
                }
                .ignoresSafeArea()
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
            }
        }
        .task {
            if let initial, link.isEmpty {
                if let server = initial.server {
                    link = server.absoluteString + "/invite/" + initial.token
                } else {
                    link = initial.token
                }
                await check()
            }
        }
    }

    // MARK: Step 1: the link

    private var parsed: InviteLink.Parsed? { InviteLink.parse(link) }

    /// Only a token was pasted and there is no server on screen: ask for the address.
    private var needsAddress: Bool {
        guard let parsed else { return false }
        return parsed.server == nil && fallbackServer == nil
    }

    private var enter: some View {
        VStack(alignment: .leading, spacing: 14) {
            Image(systemName: "envelope.open").font(.system(size: 34, weight: .light)).foregroundStyle(Theme.accent)
            Text("invite.title").displayFont(26)
            Text("invite.intro").foregroundStyle(Theme.textMuted)
            TextField("invite.linkPlaceholder", text: $link, axis: .vertical)
                .lineLimit(1...3)
                .textContentType(.URL)
                .keyboardType(.URL)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .modifier(FieldStyle())
                .onSubmit { Task { await check() } }
            if !link.isEmpty && parsed == nil {
                Text("invite.badLink").font(.footnote).foregroundStyle(.red)
            }
            if needsAddress {
                Text("addServer.address").font(.subheadline).foregroundStyle(Theme.textMuted)
                TextField("addServer.placeholder", text: $address)
                    .keyboardType(.URL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .modifier(FieldStyle())
            }
            if let problem {
                Text(problem).font(.footnote).foregroundStyle(.red)
            }
            Button { Task { await check() } } label: {
                Group {
                    if checking { ProgressView() } else { Text("invite.continue").bold() }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(checking || parsed == nil || (needsAddress && address.isEmpty))
            Button {
                if let text = UIPasteboard.general.string { link = text }
            } label: {
                Label("invite.paste", systemImage: "doc.on.clipboard")
            }
            .frame(maxWidth: .infinity)
            .foregroundStyle(Theme.textMuted)
        }
    }

    private func check() async {
        guard let parsed else { return }
        let base = parsed.server ?? fallbackServer ?? ServerURL.normalize(address)
        guard let base else {
            problem = "addServer.error.invalid"
            return
        }
        checking = true
        problem = nil
        defer { checking = false }
        let info: Components.Schemas.SystemInfo
        do {
            info = try await SystemClient(baseURL: base).info()
        } catch ServerCheckError.starting {
            problem = "addServer.error.starting"
            return
        } catch ServerCheckError.notStreamero {
            problem = "addServer.error.notStreamero"
            return
        } catch {
            problem = "addServer.error.unreachable"
            return
        }
        do {
            let details = try await AuthClient(baseURL: base).invite(token: parsed.token)
            invite = CheckedInvite(server: SavedServer(id: info.id, name: info.name, url: base, version: info.version),
                                   token: parsed.token, serverName: details.serverName)
        } catch let error as APIError where error.status == 0 || error.status >= 500 {
            problem = "addServer.error.unreachable"
        } catch is APIError {
            problem = "invites.invalidText"
        } catch {
            problem = "addServer.error.unreachable"
        }
    }

    // MARK: Step 2: the account

    private func signUp(_ invite: CheckedInvite) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("invites.joinTitle \(invite.serverName)").displayFont(26).lineLimit(3).minimumScaleFactor(0.6)
            Text(verbatim: invite.server.url.host() ?? invite.server.url.absoluteString).font(.footnote).foregroundStyle(Theme.textFaint)
            Text("invites.joinIntro").foregroundStyle(Theme.textMuted)
            TextField("login.username", text: $form.username)
                .textContentType(.username)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .modifier(FieldStyle())
            SecureField("login.password", text: $form.password)
                .textContentType(.newPassword)
                .modifier(FieldStyle())
            SecureField("invite.confirmPassword", text: $form.confirm)
                .textContentType(.newPassword)
                .modifier(FieldStyle())
                .onSubmit { Task { await create(invite) } }
            Text("password_hint").font(.caption).foregroundStyle(Theme.textFaint)
            if showProblems, let p = form.problem {
                Text(message(for: p)).font(.footnote).foregroundStyle(.red)
            }
            if let signUpError {
                errorMessage(signUpError).font(.footnote).foregroundStyle(.red)
            }
            Button { Task { await create(invite) } } label: {
                Group {
                    if busy { ProgressView() } else { Text("invites.join").bold() }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(busy || form.username.isEmpty || form.password.isEmpty || form.confirm.isEmpty)
            Button("invite.otherLink") {
                self.invite = nil
                signUpError = nil
            }
            .frame(maxWidth: .infinity)
            .foregroundStyle(Theme.textMuted)
        }
    }

    private func message(for problem: InviteSignUpForm.Problem) -> LocalizedStringKey {
        switch problem {
        case .username: "errors.invalid_username"
        case .passwordLength: "password_hint"
        case .mismatch: "errors.password_mismatch"
        }
    }

    private func create(_ invite: CheckedInvite) async {
        showProblems = true
        guard form.problem == nil else { return }
        busy = true
        signUpError = nil
        defer { busy = false }
        do {
            let (token, _) = try await AuthClient(baseURL: invite.server.url)
                .acceptInvite(token: invite.token, username: form.username.trimmingCharacters(in: .whitespaces), password: form.password)
            // Store the session first, then switch to the server: the root view restores it.
            session.signedIn(server: invite.server, token: token)
            servers.add(invite.server)
            dismiss()
        } catch {
            signUpError = error
        }
    }
}
