import SwiftUI

/// Sign-in settings (admin): public address and password sign-in, OpenID Connect providers,
/// LDAP / Active Directory. Client secrets and the bind password are write-only: the page only
/// shows whether one is stored, and an empty field keeps it.
struct SignInSettingsPage: View {
    let client: MediaClient
    @Environment(AppState.self) private var app
    @State private var providers: [OidcProvider]?
    @State private var error: Error?
    @State private var editing: OidcEditTarget?
    @State private var deleting: OidcProvider?
    @State private var showLdap = false

    var body: some View {
        AdminGuarded {
            Form {
                Section { IntroText(text: "sso.intro") }
                SettingsDoc(load: { try await client.authSettings() },
                            save: { s in try await client.saveAuthSettings(AuthSettingsInput(publicUrl: s.publicUrl, disablePasswordLogin: s.disablePasswordLogin)) }) { s in
                    Section {
                        LabeledField(title: "sso.publicUrl", text: s.publicUrl, prompt: client.baseURL.absoluteString, keyboard: .URL)
                        FieldHint(text: "sso.publicUrlHint")
                        Toggle(isOn: s.disablePasswordLogin) {
                            VStack(alignment: .leading, spacing: 2) {
                                Text("sso.disablePassword")
                                FieldHint(text: "sso.disablePasswordHint")
                            }
                        }
                        if s.wrappedValue.recoveryActive {
                            Text("sso.recoveryActive").font(.footnote).foregroundStyle(.red)
                        }
                    } header: { Text("sso.title") }
                }
                Section {
                    if providers?.isEmpty == true { Text("sso.noProviders").foregroundStyle(Theme.textMuted) }
                    ForEach(providers ?? []) { p in
                        Button { editing = OidcEditTarget(provider: p) } label: { providerRow(p) }
                            .swipeActions { Button("delete", role: .destructive) { deleting = p } }
                            .contextMenu {
                                Button("common.edit", systemImage: "pencil") { editing = OidcEditTarget(provider: p) }
                                Button("delete", systemImage: "trash", role: .destructive) { deleting = p }
                            }
                    }
                    Button { editing = OidcEditTarget(provider: nil) } label: { Label("sso.addProvider", systemImage: "plus") }
                    ErrorLine(error: error)
                } header: {
                    Text("sso.oidcTitle")
                } footer: {
                    Text("sso.oidcIntro")
                }
                Section {
                    NavigationLink { LdapSettingsView(client: client) } label: {
                        Label("sso.ldapTitle", systemImage: "building.columns")
                    }
                }
            }
            .sheet(item: $editing) { target in
                OidcEditor(client: client, provider: target.provider) { Task { await load() } }
                    .environment(\.locale, app.locale ?? Locale.current)
            }
            .confirmationDialog(Text(verbatim: deleting.map { String(format: Loc.string("sso.confirmDelete %@"), $0.label) } ?? ""),
                                isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                                titleVisibility: .visible, presenting: deleting) { p in
                Button("delete", role: .destructive) {
                    Task {
                        do {
                            try await client.deleteOidcProvider(p.id)
                            await load()
                        } catch { self.error = error }
                    }
                }
            }
            .task { await load() }
        }
    }

    private func providerRow(_ p: OidcProvider) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 6) {
                Text(verbatim: p.label).font(.body.weight(.semibold)).foregroundStyle(Theme.text)
                TagBadge(text: p.enabled ? "sso.enabled" : "sso.disabled", tone: p.enabled ? .success : .neutral)
            }
            Text(verbatim: p.issuer).font(.caption).foregroundStyle(Theme.textMuted).lineLimit(1).truncationMode(.middle)
            if p.redirectUri.isEmpty {
                Text("sso.needsPublicUrl").font(.caption).foregroundStyle(.red)
            } else {
                Text("sso.redirectUri").font(.caption2).foregroundStyle(Theme.textFaint)
                Text(verbatim: p.redirectUri).font(.caption.monospaced()).foregroundStyle(Theme.textMuted).textSelection(.enabled)
            }
        }
        .padding(.vertical, 2)
    }

    private func load() async {
        do {
            providers = try await client.oidcProviders()
            error = nil
        } catch is CancellationError { return } catch { self.error = error }
    }
}

struct OidcEditTarget: Identifiable {
    let provider: OidcProvider?
    var id: String { provider?.id ?? "new" }
}

private struct OidcEditor: View {
    let client: MediaClient
    let provider: OidcProvider?
    let onSaved: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var form = OidcForm()
    @State private var loaded = false
    @State private var busy = false
    @State private var testing = false
    @State private var testResult: OidcTestResult?
    @State private var error: Error?

    var body: some View {
        NavigationStack {
            Form {
                if provider == nil {
                    Section {
                        Menu {
                            ForEach(OidcForm.Preset.allCases) { p in
                                Button { form.apply(p) } label: { Text(verbatim: p.label) }
                            }
                        } label: {
                            Label("sso.preset", systemImage: "wand.and.stars")
                        }
                    }
                }
                Section {
                    LabeledField(title: "sso.label", text: $form.label)
                    FieldHint(text: "sso.labelHint")
                    LabeledField(title: "sso.id", text: $form.id)
                        .disabled(provider != nil)
                    if !form.id.isEmpty && !OidcForm.isValidID(form.id) {
                        Text("sso.idHint").font(.caption).foregroundStyle(.red)
                    } else {
                        FieldHint(text: "sso.idHint")
                    }
                    LabeledField(title: "sso.issuer", text: $form.issuer, keyboard: .URL)
                    FieldHint(text: "sso.issuerHint")
                }
                Section {
                    LabeledField(title: "sso.clientId", text: $form.clientId)
                    LabeledField(title: provider?.clientSecretSet == true ? "sso.replaceSecret" : "sso.clientSecret",
                                 text: $form.clientSecret, secure: true)
                    if provider?.clientSecretSet == true { TagBadge(text: "sso.secretStored", tone: .success) }
                    FieldHint(text: "sso.secretHint")
                    LabeledField(title: "sso.scopes", text: $form.scopes)
                    FieldHint(text: "sso.scopesHint")
                }
                Section {
                    LabeledField(title: "sso.usernameClaim", text: $form.usernameClaim)
                    LabeledField(title: "sso.emailClaim", text: $form.emailClaim)
                    LabeledField(title: "sso.nameClaim", text: $form.nameClaim)
                    LabeledField(title: "sso.groupsClaim", text: $form.groupsClaim)
                    FieldHint(text: "sso.groupsClaimHint")
                    LabeledField(title: "sso.adminGroup", text: $form.adminGroup)
                    FieldHint(text: "sso.adminGroupHint")
                    LabeledField(title: "sso.allowedGroup", text: $form.allowedGroup)
                    FieldHint(text: "sso.allowedGroupHint")
                }
                Section {
                    hintedToggle("sso.autoCreate", "sso.autoCreateHint", $form.autoCreate)
                    hintedToggle("sso.linkByEmail", "sso.linkByEmailHint", $form.linkByEmail)
                    hintedToggle("sso.trustEmail", "sso.trustEmailHint", $form.trustEmail)
                    hintedToggle("sso.enabledLabel", "sso.enabledHint", $form.enabled)
                }
                Section {
                    Button { Task { await test() } } label: {
                        HStack {
                            Text("sso.testConnection")
                            if testing { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(form.issuer.isEmpty || testing)
                    if let r = testResult {
                        Text("sso.testOk \(r.signingKeys)").font(.footnote).foregroundStyle(.green)
                        if !r.pkceS256 { Text("sso.testNoPkce").font(.footnote).foregroundStyle(Theme.textMuted) }
                    }
                    ErrorLine(error: error)
                }
            }
            .scrollContentBackground(.hidden)
            .background(Theme.background)
            .navigationTitle(Text(provider == nil ? LocalizedStringKey("sso.addProvider") : LocalizedStringKey("common.edit")))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if busy { ProgressView() } else { Button("save") { Task { await save() } }.disabled(!form.isValid) }
                }
            }
        }
        .presentationBackground(Theme.background)
        .onAppear {
            if !loaded, let provider { form = OidcForm(provider) }
            loaded = true
        }
    }

    private func test() async {
        testing = true
        defer { testing = false }
        do {
            testResult = try await client.testOidc(issuer: form.issuer.trimmingCharacters(in: .whitespaces))
            error = nil
        } catch {
            testResult = nil
            self.error = error
        }
    }

    private func save() async {
        busy = true
        defer { busy = false }
        do {
            if let provider {
                _ = try await client.updateOidcProvider(provider.id, form.input)
            } else {
                _ = try await client.createOidcProvider(form.input)
            }
            onSaved()
            dismiss()
        } catch { self.error = error }
    }
}

/// A switch with a one-line explanation.
@MainActor
func hintedToggle(_ title: LocalizedStringKey, _ hint: LocalizedStringKey, _ isOn: Binding<Bool>) -> some View {
    Toggle(isOn: isOn) {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
            FieldHint(text: hint)
        }
    }
}

/// LDAP / Active Directory sign-in with a connection test.
private struct LdapSettingsView: View {
    let client: MediaClient
    @State private var settings: LdapSettings?
    @State private var form: LdapSettingsInput?
    @State private var password = ""
    @State private var testUser = ""
    @State private var result: LdapTestResult?
    @State private var testing = false
    @State private var saving = false
    @State private var saved = false
    @State private var error: Error?

    var body: some View {
        Form {
            Section { IntroText(text: "sso.ldapIntro") }
            if let binding = Binding($form) {
                Section {
                    hintedToggle("sso.ldapEnabled", "sso.ldapEnabledHint", binding.enabled)
                }
                Section {
                    LabeledField(title: "sso.ldapUrl", text: binding.url, prompt: "ldaps://ldap.example.com:636", keyboard: .URL)
                    FieldHint(text: "sso.ldapUrlHint")
                    hintedToggle("sso.startTls", "sso.startTlsHint", binding.startTls)
                    hintedToggle("sso.skipVerify", "sso.skipVerifyHint", binding.skipVerify)
                    if binding.wrappedValue.isPlainText {
                        Text("sso.plainWarning").font(.footnote).foregroundStyle(.red)
                    }
                    VStack(alignment: .leading, spacing: 4) {
                        Text("sso.caCert").font(.caption).foregroundStyle(Theme.textMuted)
                        TextField(text: optionalText(binding.caCert), prompt: Text(verbatim: "-----BEGIN CERTIFICATE-----"), axis: .vertical) { Text("sso.caCert") }
                            .lineLimit(2...6).font(.caption.monospaced())
                            .textInputAutocapitalization(.never).autocorrectionDisabled()
                    }
                }
                Section {
                    LabeledField(title: "sso.bindDn", text: optionalText(binding.bindDn), prompt: "cn=streamero,ou=services,dc=example,dc=org")
                    LabeledField(title: settings?.bindPasswordSet == true ? "sso.replaceSecret" : "sso.bindPassword", text: $password, secure: true)
                    if settings?.bindPasswordSet == true { TagBadge(text: "sso.secretStored", tone: .success) }
                    FieldHint(text: "sso.secretHint")
                    LabeledField(title: "sso.baseDn", text: binding.baseDn, prompt: "dc=example,dc=org")
                    LabeledField(title: "sso.userFilter", text: binding.userFilter)
                    FieldHint(text: "sso.userFilterHint")
                }
                Section {
                    LabeledField(title: "sso.usernameAttr", text: optionalText(binding.usernameAttribute))
                    FieldHint(text: "sso.usernameAttrHint")
                    LabeledField(title: "sso.emailAttr", text: optionalText(binding.emailAttribute))
                    LabeledField(title: "sso.nameAttr", text: optionalText(binding.nameAttribute))
                    LabeledField(title: "sso.groupAttr", text: optionalText(binding.groupAttribute))
                    LabeledField(title: "sso.groupFilter", text: optionalText(binding.groupFilter))
                    FieldHint(text: "sso.groupFilterHint")
                    LabeledField(title: "sso.adminGroup", text: optionalText(binding.adminGroup))
                    FieldHint(text: "sso.ldapAdminGroupHint")
                    LabeledField(title: "sso.allowedGroup", text: optionalText(binding.allowedGroup))
                    FieldHint(text: "sso.ldapAllowedGroupHint")
                }
                Section {
                    LabeledField(title: "sso.testUser", text: $testUser)
                    FieldHint(text: "sso.testUserHint")
                    Button { Task { await test() } } label: {
                        HStack {
                            Text("sso.testConnection")
                            if testing { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(binding.wrappedValue.url.isEmpty || testing)
                    if let result { testResult(result) }
                }
                Section {
                    Button { Task { await save() } } label: {
                        HStack {
                            Text("save").frame(maxWidth: .infinity)
                            if saving { ProgressView() }
                        }
                    }
                    if saved { Text("saved").foregroundStyle(.green) }
                    ErrorLine(error: error)
                }
            } else if let error {
                Section { ErrorLine(error: error) }
            } else {
                LoadingView().listRowBackground(Color.clear)
            }
        }
        .scrollContentBackground(.hidden)
        .background(Theme.background)
        .navigationTitle(Text("sso.ldapTitle"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .onChange(of: form) { _, _ in saved = false }
    }

    @ViewBuilder private func testResult(_ r: LdapTestResult) -> some View {
        if r.ok {
            Text("sso.ldapTestOk").font(.footnote).foregroundStyle(.green)
        } else {
            Text("sso.ldapTestFailed \(r.message)").font(.footnote).foregroundStyle(.red)
        }
        if let dn = r.userDn {
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: "DN: \(dn)").textSelection(.enabled)
                Text("sso.usernameAttr") + Text(verbatim: ": \(r.username ?? "—")")
                Text("sso.emailAttr") + Text(verbatim: ": \(r.email?.isEmpty == false ? r.email! : "—")")
                Text("sso.groups") + Text(verbatim: ": \(r.groups?.joined(separator: "; ") ?? "—")")
                Text("administrator") + Text(verbatim: ": ") + Text(r.isAdmin == true ? LocalizedStringKey("common.yes") : LocalizedStringKey("common.no"))
                Text("sso.allowed") + Text(verbatim: ": ") + Text(r.allowed == true ? LocalizedStringKey("common.yes") : LocalizedStringKey("common.no"))
            }
            .font(.caption).foregroundStyle(Theme.textMuted)
        }
    }

    private var body_: LdapSettingsInput? {
        guard var f = form else { return nil }
        f.bindPassword = password.isEmpty ? nil : password
        return f
    }

    private func load() async {
        do {
            let s = try await client.ldapSettings()
            settings = s
            form = LdapSettingsInput(s)
            error = nil
        } catch is CancellationError { return } catch { self.error = error }
    }

    private func test() async {
        guard let body = body_ else { return }
        testing = true
        defer { testing = false }
        do {
            result = try await client.testLdap(LdapTestRequest(settings: body, username: testUser.isEmpty ? nil : testUser))
            error = nil
        } catch { self.error = error }
    }

    private func save() async {
        guard let body = body_ else { return }
        saving = true
        defer { saving = false }
        do {
            let s = try await client.saveLdapSettings(body)
            settings = s
            form = LdapSettingsInput(s)
            password = ""
            saved = true
            error = nil
        } catch { self.error = error }
    }
}
