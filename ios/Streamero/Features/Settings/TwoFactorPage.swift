import SwiftUI

/// Two-factor sign-in (TOTP): turn on with a QR code / authenticator link / manual key,
/// confirm with a code, save the one-time recovery codes; turn off with the password.
struct TwoFactorPage: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var setup: TOTPSetup?
    @State private var code = ""
    @State private var password = ""
    @State private var recovery: [String]?
    @State private var busy = false
    @State private var error: Error?

    private var enabled: Bool { app.me?.totpEnabled == true }

    var body: some View {
        Form {
            Section {
                HStack {
                    Text("twofa.title").font(.headline)
                    Spacer()
                    TagBadge(text: enabled ? "twofa.on" : "twofa.off", tone: enabled ? .success : .neutral)
                }
            }
            if let recovery { recoverySection(recovery) }
            if enabled {
                disableSection
            } else if let setup {
                setupSections(setup)
            } else {
                Section {
                    Text("twofa.intro").font(.subheadline).foregroundStyle(Theme.textMuted)
                    Button { Task { await start() } } label: {
                        HStack {
                            Text("twofa.enable").bold()
                            if busy { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(busy)
                }
            }
            if let error { Section { errorText(error) } }
        }
    }

    // MARK: Sections

    private func recoverySection(_ codes: [String]) -> some View {
        Section {
            Text("twofa.recoveryText").font(.footnote).foregroundStyle(Theme.textMuted)
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], alignment: .leading, spacing: 8) {
                ForEach(codes, id: \.self) { c in
                    Text(verbatim: c).font(.callout.monospaced()).textSelection(.enabled)
                }
            }
            .padding(.vertical, 6)
            CopyButton(text: TwoFactor.recoveryText(codes))
            ShareLink(item: TwoFactor.recoveryText(codes)) { Label("common.share", systemImage: "square.and.arrow.up") }
            Button("twofa.savedCodes") { recovery = nil }
        } header: {
            Text("twofa.recoveryTitle")
        }
        .listRowBackground(Color.green.opacity(0.08))
    }

    @ViewBuilder private func setupSections(_ setup: TOTPSetup) -> some View {
        Section {
            Text("twofa.step1").font(.subheadline)
            HStack {
                Spacer()
                QRCodeView(uri: setup.otpauthUri, fallbackPNG: setup.qrPng)
                    .accessibilityLabel(Text("twofa.qrAlt"))
                Spacer()
            }
            .listRowBackground(Color.clear)
            if let link = URL(string: setup.otpauthUri) {
                Link(destination: link) {
                    Label("twofa.openApp", systemImage: "arrow.up.forward.app")
                }
            }
        }
        Section {
            Text(verbatim: setup.secret).font(.callout.monospaced()).textSelection(.enabled)
            CopyButton(text: setup.secret)
        } header: {
            Text("twofa.manual")
        }
        Section {
            TextField("twofa.code", text: $code)
                .keyboardType(.numberPad)
                .textContentType(.oneTimeCode)
                .font(.title3.monospaced())
            Button { Task { await enable() } } label: {
                HStack {
                    Text("twofa.confirm").bold()
                    if busy { Spacer(); ProgressView() }
                }
            }
            .disabled(busy || !TwoFactor.isValidCode(code))
            Button("common.cancel", role: .cancel) {
                self.setup = nil
                code = ""
                error = nil
            }
        }
    }

    private var disableSection: some View {
        Section {
            Text("twofa.enabledText").font(.subheadline).foregroundStyle(Theme.textMuted)
            SecureField("login.password", text: $password).textContentType(.password)
            Button(role: .destructive) { Task { await disable() } } label: {
                HStack {
                    Text("twofa.disable")
                    if busy { Spacer(); ProgressView() }
                }
            }
            .disabled(busy || password.isEmpty)
        } footer: {
            Text("twofa.regenerateHint")
        }
    }

    private func errorText(_ error: Error) -> some View {
        Group {
            if let api = error as? APIError, api.code == "invalid_totp" {
                Text("login.error.totp")
            } else if let api = error as? APIError, api.code == "invalid_credentials" || api.code == "invalid_password" {
                Text("err_credentials")
            } else {
                errorMessage(error)
            }
        }
        .font(.subheadline).foregroundStyle(.red)
    }

    // MARK: Actions

    private func start() async {
        busy = true
        defer { busy = false }
        do {
            setup = try await client.startTOTPSetup()
            error = nil
        } catch { self.error = error }
    }

    private func enable() async {
        busy = true
        defer { busy = false }
        do {
            let result = try await client.enableTOTP(code: TwoFactor.normalizedCode(code))
            recovery = result.recoveryCodes
            setup = nil
            code = ""
            error = nil
            await app.refreshMe()
        } catch { self.error = error }
    }

    private func disable() async {
        busy = true
        defer { busy = false }
        do {
            try await client.disableTOTP(password: password)
            password = ""
            error = nil
            await app.refreshMe()
        } catch { self.error = error }
    }
}

/// The otpauth URI as a QR code, drawn on the phone; the server's PNG is the fallback.
struct QRCodeView: View {
    let uri: String
    var fallbackPNG: String? = nil

    var body: some View {
        Group {
            if let image = TwoFactor.qrImage(for: uri) ?? fallbackPNG.flatMap({ Data(base64Encoded: $0) }).flatMap(UIImage.init(data:)) {
                Image(uiImage: image)
                    .interpolation(.none)
                    .resizable()
                    .scaledToFit()
            } else {
                Image(systemName: "qrcode").resizable().scaledToFit().foregroundStyle(.black.opacity(0.3))
            }
        }
        .frame(width: 200, height: 200)
        .padding(12)
        .background(.white, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }
}
