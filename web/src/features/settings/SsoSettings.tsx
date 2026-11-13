import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Badge, Button, Card, ErrorText, Select, SuccessText, TextField } from "@/components/ui";
import { SettingRow, Toggle } from "./SettingRow";
import { useConfirm } from "@/components/Dialog";

type OidcProvider = components["schemas"]["OidcProvider"];
type OidcInput = components["schemas"]["OidcProviderInput"];
type LdapInput = components["schemas"]["LdapSettingsInput"];

/** Starting points for well-known providers; the admin fills in their own address. */
const PRESETS: Record<string, Partial<OidcInput>> = {
  authentik: { id: "authentik", label: "Authentik", issuer: "https://auth.example.com/application/o/streamero/", groupsClaim: "groups" },
  keycloak: { id: "keycloak", label: "Keycloak", issuer: "https://auth.example.com/realms/home", groupsClaim: "groups" },
  authelia: { id: "authelia", label: "Authelia", issuer: "https://auth.example.com", groupsClaim: "groups", scopes: ["openid", "profile", "email", "groups"] },
  entra: { id: "entra", label: "Microsoft", issuer: "https://login.microsoftonline.com/<tenant-id>/v2.0", groupsClaim: "groups" },
  google: { id: "google", label: "Google", issuer: "https://accounts.google.com", usernameClaim: "email" },
};

const emptyOidc: OidcInput & { clientSecret: string } = {
  id: "", label: "", issuer: "", clientId: "", clientSecret: "", scopes: ["openid", "profile", "email"],
  usernameClaim: "preferred_username", emailClaim: "email", nameClaim: "name", groupsClaim: "groups",
  adminGroup: "", allowedGroup: "", autoCreate: false, linkByEmail: true, trustEmail: false, enabled: true,
};

const inputCls = "h-10 w-full rounded-[6px] border border-border bg-black/30 px-3 text-[15px] text-text placeholder:text-faint hover:border-border-strong focus:border-accent/70";

export function SsoSettings() {
  return (
    <div className="flex flex-col gap-6">
      <GeneralCard />
      <OidcCard />
      <LdapCard />
    </div>
  );
}

function GeneralCard() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["settings", "auth"], queryFn: async () => unwrap(await api.GET("/settings/auth")) });
  const [form, setForm] = useState({ publicUrl: "", disablePasswordLogin: false });
  useEffect(() => {
    if (q.data) setForm({ publicUrl: q.data.publicUrl, disablePasswordLogin: q.data.disablePasswordLogin });
  }, [q.data]);
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/settings/auth", { body: form })),
    onSuccess: (d) => {
      qc.setQueryData(["settings", "auth"], d);
      void qc.invalidateQueries({ queryKey: ["settings", "oidc"] });
    },
  });
  return (
    <form onSubmit={(e) => { e.preventDefault(); save.reset(); save.mutate(); }}>
      <Card title={t("sso.title")}>
        <p className="mb-4 text-sm text-muted">{t("sso.intro")}</p>
        <SettingRow label={t("sso.publicUrl")} hint={t("sso.publicUrlHint")}>
          {(id) => <input id={id} className={inputCls} value={form.publicUrl} placeholder={window.location.origin}
            onChange={(e) => setForm({ ...form, publicUrl: e.target.value })} />}
        </SettingRow>
        <SettingRow label={t("sso.disablePassword")} hint={t("sso.disablePasswordHint")}>
          {(id) => <Toggle id={id} checked={form.disablePasswordLogin} onChange={(v) => setForm({ ...form, disablePasswordLogin: v })} />}
        </SettingRow>
        {q.data?.recoveryActive && <p className="mt-3 text-sm text-danger" data-testid="recovery-active">{t("sso.recoveryActive")}</p>}
        <div className="mt-5 flex flex-wrap items-center gap-4">
          <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
          {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
          <ErrorText error={q.error ?? save.error} />
        </div>
      </Card>
    </form>
  );
}

function OidcCard() {
  const confirmDialog = useConfirm();
  const { t } = useTranslation();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["settings", "oidc"], queryFn: async () => unwrap(await api.GET("/settings/auth/oidc")) });
  const [editing, setEditing] = useState<{ existing?: OidcProvider } | null>(null);
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/settings/auth/oidc/{providerId}", { params: { path: { providerId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings", "oidc"] }),
  });
  return (
    <Card title={t("sso.oidcTitle")} actions={!editing && <Button onClick={() => setEditing({})}>{t("sso.addProvider")}</Button>}>
      <p className="mb-4 text-sm text-muted">{t("sso.oidcIntro")}</p>
      {list.data?.length === 0 && !editing && <p className="text-sm text-faint">{t("sso.noProviders")}</p>}
      <ul className="flex flex-col divide-y divide-border">
        {list.data?.map((p) => (
          <li key={p.id} className="flex flex-wrap items-center gap-3 py-3" data-testid={`oidc-${p.id}`}>
            <div className="min-w-0 flex-1">
              <p className="flex items-center gap-2 font-medium">
                {p.label} {p.enabled ? <Badge tone="success">{t("sso.enabled")}</Badge> : <Badge>{t("sso.disabled")}</Badge>}
              </p>
              <p className="truncate text-[13px] text-text/55">{p.issuer}</p>
              {p.redirectUri
                ? <p className="mt-1 text-[13px] text-text/70">{t("sso.redirectUri")}: <code className="break-all select-all">{p.redirectUri}</code></p>
                : <p className="mt-1 text-[13px] text-danger">{t("sso.needsPublicUrl")}</p>}
            </div>
            <Button variant="ghost" onClick={() => setEditing({ existing: p })}>{t("common.edit")}</Button>
            <Button variant="danger" disabled={remove.isPending}
              onClick={() => void confirmDialog({ message: t("sso.confirmDelete", { name: p.label }), danger: true }).then((ok) => ok && remove.mutate(p.id))}>{t("common.delete")}</Button>
          </li>
        ))}
      </ul>
      <ErrorText error={list.error ?? remove.error} />
      {editing && <OidcForm existing={editing.existing} onDone={() => setEditing(null)} />}
    </Card>
  );
}

function OidcForm({ existing, onDone }: { existing?: OidcProvider; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [f, setF] = useState(() => (existing ? { ...emptyOidc, ...existing, clientSecret: "" } : { ...emptyOidc }));
  const [scopes, setScopes] = useState((existing?.scopes ?? emptyOidc.scopes ?? []).join(" "));
  const set = (patch: Partial<typeof f>) => setF((x) => ({ ...x, ...patch }));
  const body = (): OidcInput => ({
    id: f.id, label: f.label, issuer: f.issuer, clientId: f.clientId, clientSecret: f.clientSecret || undefined,
    scopes: scopes.split(/\s+/).filter(Boolean), usernameClaim: f.usernameClaim, emailClaim: f.emailClaim, nameClaim: f.nameClaim,
    groupsClaim: f.groupsClaim, adminGroup: f.adminGroup, allowedGroup: f.allowedGroup, autoCreate: f.autoCreate,
    linkByEmail: f.linkByEmail, trustEmail: f.trustEmail, enabled: f.enabled,
  });
  const save = useMutation({
    mutationFn: async () => existing
      ? unwrap(await api.PUT("/settings/auth/oidc/{providerId}", { params: { path: { providerId: existing.id } }, body: body() }))
      : unwrap(await api.POST("/settings/auth/oidc", { body: body() })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["settings", "oidc"] });
      onDone();
    },
  });
  const test = useMutation({ mutationFn: async () => unwrap(await api.POST("/settings/auth/oidc-test", { body: { issuer: f.issuer } })) });
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  const text = (key: keyof typeof f, label: string, hint?: string, props: Record<string, unknown> = {}) => (
    <TextField label={label} hint={hint} value={String(f[key] ?? "")} onChange={(e) => set({ [key]: e.target.value } as Partial<typeof f>)} {...props} />
  );
  const toggle = (key: "autoCreate" | "linkByEmail" | "trustEmail" | "enabled", label: string, hint: string) => (
    <SettingRow label={label} hint={hint}>{(id) => <Toggle id={id} checked={Boolean(f[key])} onChange={(v) => set({ [key]: v })} />}</SettingRow>
  );
  return (
    <form className="mt-5 flex flex-col gap-4 border-t border-border pt-5" onSubmit={onSubmit} aria-label={t("sso.providerForm")}>
      {!existing && (
        <label className="flex max-w-xs flex-col gap-1 text-sm">
          {t("sso.preset")}
          <Select defaultValue="" onChange={(e) => {
            const p = PRESETS[e.target.value];
            if (p) { set(p); if (p.scopes) setScopes(p.scopes.join(" ")); }
          }}>
            <option value="">{t("sso.presetGeneric")}</option>
            {Object.entries(PRESETS).map(([k, p]) => <option key={k} value={k}>{p.label}</option>)}
          </Select>
        </label>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        {text("label", t("sso.label"), t("sso.labelHint"), { required: true, maxLength: 60 })}
        {text("id", t("sso.id"), t("sso.idHint"), { required: true, disabled: Boolean(existing), pattern: "[a-z0-9][a-z0-9-]{0,31}" })}
      </div>
      {text("issuer", t("sso.issuer"), t("sso.issuerHint"), { required: true, spellCheck: false })}
      <div className="grid gap-4 sm:grid-cols-2">
        {text("clientId", t("sso.clientId"), undefined, { required: true, spellCheck: false })}
        {text("clientSecret", existing?.clientSecretSet ? t("sso.replaceSecret") : t("sso.clientSecret"), t("sso.secretHint"),
          { type: "password", autoComplete: "off" })}
      </div>
      <TextField label={t("sso.scopes")} hint={t("sso.scopesHint")} value={scopes} onChange={(e) => setScopes(e.target.value)} />
      <div className="grid gap-4 sm:grid-cols-2">
        {text("usernameClaim", t("sso.usernameClaim"))}
        {text("emailClaim", t("sso.emailClaim"))}
        {text("nameClaim", t("sso.nameClaim"))}
        {text("groupsClaim", t("sso.groupsClaim"), t("sso.groupsClaimHint"))}
        {text("adminGroup", t("sso.adminGroup"), t("sso.adminGroupHint"))}
        {text("allowedGroup", t("sso.allowedGroup"), t("sso.allowedGroupHint"))}
      </div>
      <div>
        {toggle("autoCreate", t("sso.autoCreate"), t("sso.autoCreateHint"))}
        {toggle("linkByEmail", t("sso.linkByEmail"), t("sso.linkByEmailHint"))}
        {toggle("trustEmail", t("sso.trustEmail"), t("sso.trustEmailHint"))}
        {toggle("enabled", t("sso.enabledLabel"), t("sso.enabledHint"))}
      </div>
      {test.data && (
        <p role="status" className="text-sm text-success" data-testid="oidc-test-ok">
          {t("sso.testOk", { keys: test.data.signingKeys })}{!test.data.pkceS256 && ` ${t("sso.testNoPkce")}`}
        </p>
      )}
      <ErrorText error={test.error ?? save.error} />
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
        <Button type="button" variant="ghost" disabled={test.isPending || !f.issuer} onClick={() => test.mutate()}>{t("sso.testConnection")}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
      </div>
    </form>
  );
}

function LdapCard() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["settings", "ldap"], queryFn: async () => unwrap(await api.GET("/settings/auth/ldap")) });
  const [f, setF] = useState<LdapInput & { bindPassword: string }>({
    enabled: false, url: "", startTls: true, skipVerify: false, caCert: "", bindDn: "", bindPassword: "", baseDn: "",
    userFilter: "", usernameAttribute: "", emailAttribute: "", nameAttribute: "", groupAttribute: "", groupFilter: "",
    adminGroup: "", allowedGroup: "",
  });
  const [testUser, setTestUser] = useState("");
  useEffect(() => {
    if (q.data) { const { bindPasswordSet: _set, ...rest } = q.data; setF({ ...rest, bindPassword: "" }); }
  }, [q.data]);
  const set = (patch: Partial<typeof f>) => setF((x) => ({ ...x, ...patch }));
  const body = (): LdapInput => ({ ...f, bindPassword: f.bindPassword || undefined });
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/settings/auth/ldap", { body: body() })),
    onSuccess: (d) => qc.setQueryData(["settings", "ldap"], d),
  });
  const test = useMutation({
    mutationFn: async () => unwrap(await api.POST("/settings/auth/ldap/test", { body: { settings: body(), username: testUser || undefined } })),
  });
  const text = (key: keyof typeof f, label: string, hint?: string, props: Record<string, unknown> = {}) => (
    <TextField label={label} hint={hint} value={String(f[key] ?? "")} spellCheck={false}
      onChange={(e) => set({ [key]: e.target.value } as Partial<typeof f>)} {...props} />
  );
  const plain = f.url.startsWith("ldap://") && !f.startTls;
  return (
    <form onSubmit={(e) => { e.preventDefault(); save.reset(); save.mutate(); }}>
      <Card title={<span className="flex items-center gap-2">{t("sso.ldapTitle")} {q.data?.enabled
        ? <Badge tone="success">{t("sso.enabled")}</Badge> : <Badge>{t("sso.disabled")}</Badge>}</span>}>
        <p className="mb-4 text-sm text-muted">{t("sso.ldapIntro")}</p>
        <SettingRow label={t("sso.ldapEnabled")} hint={t("sso.ldapEnabledHint")}>
          {(id) => <Toggle id={id} checked={f.enabled} onChange={(v) => set({ enabled: v })} />}
        </SettingRow>
        <div className="mt-4 flex flex-col gap-4">
          {text("url", t("sso.ldapUrl"), t("sso.ldapUrlHint"), { placeholder: "ldaps://ldap.example.com:636" })}
          <div>
            <SettingRow label={t("sso.startTls")} hint={t("sso.startTlsHint")}>
              {(id) => <Toggle id={id} checked={f.startTls} onChange={(v) => set({ startTls: v })} />}
            </SettingRow>
            <SettingRow label={t("sso.skipVerify")} hint={t("sso.skipVerifyHint")}>
              {(id) => <Toggle id={id} checked={f.skipVerify} onChange={(v) => set({ skipVerify: v })} />}
            </SettingRow>
          </div>
          {plain && <p className="text-sm text-danger">{t("sso.plainWarning")}</p>}
          <label className="flex flex-col gap-1 text-sm">
            {t("sso.caCert")}
            <textarea rows={3} value={f.caCert ?? ""} onChange={(e) => set({ caCert: e.target.value })} spellCheck={false}
              placeholder="-----BEGIN CERTIFICATE-----" className="rounded-[6px] border border-border bg-black/30 px-3 py-2 font-mono text-xs text-text" />
          </label>
          <div className="grid gap-4 sm:grid-cols-2">
            {text("bindDn", t("sso.bindDn"), undefined, { placeholder: "cn=streamero,ou=services,dc=example,dc=org" })}
            {text("bindPassword", q.data?.bindPasswordSet ? t("sso.replaceSecret") : t("sso.bindPassword"), t("sso.secretHint"),
              { type: "password", autoComplete: "off" })}
          </div>
          {text("baseDn", t("sso.baseDn"), undefined, { placeholder: "dc=example,dc=org" })}
          {text("userFilter", t("sso.userFilter"), t("sso.userFilterHint"))}
          <div className="grid gap-4 sm:grid-cols-2">
            {text("usernameAttribute", t("sso.usernameAttr"), t("sso.usernameAttrHint"))}
            {text("emailAttribute", t("sso.emailAttr"))}
            {text("nameAttribute", t("sso.nameAttr"))}
            {text("groupAttribute", t("sso.groupAttr"))}
          </div>
          {text("groupFilter", t("sso.groupFilter"), t("sso.groupFilterHint"))}
          <div className="grid gap-4 sm:grid-cols-2">
            {text("adminGroup", t("sso.adminGroup"), t("sso.ldapAdminGroupHint"))}
            {text("allowedGroup", t("sso.allowedGroup"), t("sso.ldapAllowedGroupHint"))}
          </div>
          <div className="flex flex-wrap items-end gap-2">
            <div className="max-w-xs flex-1">
              <TextField label={t("sso.testUser")} hint={t("sso.testUserHint")} value={testUser} onChange={(e) => setTestUser(e.target.value)} />
            </div>
            <Button type="button" variant="ghost" disabled={test.isPending || !f.url} onClick={() => test.mutate()}>{t("sso.testConnection")}</Button>
          </div>
          {test.data && (
            <div role="status" className={`text-sm ${test.data.ok ? "text-success" : "text-danger"}`} data-testid="ldap-test-result">
              {test.data.ok ? t("sso.ldapTestOk") : t("sso.ldapTestFailed", { message: test.data.message })}
              {test.data.userDn && (
                <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 text-text/80">
                  <dt>DN</dt><dd className="break-all">{test.data.userDn}</dd>
                  <dt>{t("sso.usernameAttr")}</dt><dd>{test.data.username}</dd>
                  <dt>{t("sso.emailAttr")}</dt><dd>{test.data.email || "—"}</dd>
                  <dt>{t("sso.groups")}</dt><dd className="break-all">{test.data.groups?.join("; ") || "—"}</dd>
                  <dt>{t("users.admin")}</dt><dd>{test.data.isAdmin ? t("common.yes") : t("common.no")}</dd>
                  <dt>{t("sso.allowed")}</dt><dd>{test.data.allowed ? t("common.yes") : t("common.no")}</dd>
                </dl>
              )}
            </div>
          )}
          <ErrorText error={q.error ?? test.error ?? save.error} />
          {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
          <Button type="submit" className="self-start" disabled={save.isPending}>{t("common.save")}</Button>
        </div>
      </Card>
    </form>
  );
}
