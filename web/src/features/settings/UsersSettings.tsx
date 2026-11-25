import { useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "lucide-react";
import { api, unwrap, type User } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Badge, Button, Card, ErrorText, SuccessText, TextField } from "@/components/ui";
import { meQueryKey, useMe } from "@/features/auth/useAuth";
import { useDateTime } from "./SettingsLayout";
import { PermissionsFields, type Permissions } from "./PermissionsEditor";
import { ALL_LIBRARIES, DEFAULT_PERMISSIONS, LibraryAccessPicker, describeAccess, useAllLibraries, type LibraryAccess } from "./LibraryAccessPicker";
import { useConfirm } from "@/components/Dialog";

type Patch = components["schemas"]["UpdateUserRequest"];

/** Library access of a stored user. */
function accessOf(u: User): LibraryAccess {
  return { all: u.permissions.allLibraries, ids: u.libraryIds ?? [], newLibraries: u.newLibraries ?? false };
}

/** Request fields for a library choice: permissions carry the allLibraries flag, the list goes along when chosen. */
function accessFields(access: LibraryAccess, permissions: Permissions) {
  return {
    permissions: { ...permissions, allLibraries: access.all },
    ...(access.all ? {} : { libraryIds: access.ids, newLibraries: access.newLibraries }),
  };
}

export function UsersSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: me } = useMe();
  const users = useQuery({ queryKey: ["users"], queryFn: async () => unwrap(await api.GET("/users")) });
  const libraries = useAllLibraries();
  const refresh = () => qc.invalidateQueries({ queryKey: ["users"] });
  const [open, setOpen] = useState<string | null>(null);

  const [form, setForm] = useState({ username: "", password: "", isAdmin: false });
  const [access, setAccess] = useState<LibraryAccess>(ALL_LIBRARIES);
  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/users", {
      body: { ...form, ...(form.isAdmin || access.all ? {} : accessFields(access, DEFAULT_PERMISSIONS)) },
    })),
    onSuccess: () => {
      setForm({ username: "", password: "", isAdmin: false });
      setAccess(ALL_LIBRARIES);
      void refresh();
    },
  });

  const onCreate = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };

  return (
    <div className="flex flex-col gap-6">
      <Card title={t("users.add")}>
        <form className="grid gap-4 sm:grid-cols-2" onSubmit={onCreate}>
          <TextField label={t("login.username")} required maxLength={64} autoComplete="off"
            value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} />
          <TextField label={t("login.password")} type="password" required minLength={8} autoComplete="new-password"
            value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} hint={t("users.passwordHint")} />
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="accent-[var(--color-accent)]" checked={form.isAdmin} onChange={(e) => setForm({ ...form, isAdmin: e.target.checked })} />
            {t("users.makeAdmin")}
          </label>
          <Section title={t("libraryAccess.title")} className="sm:col-span-2">
            {form.isAdmin
              ? <p className="text-sm text-muted">{t("libraryAccess.adminAll")}</p>
              : <LibraryAccessPicker value={access} onChange={setAccess} />}
          </Section>
          <div className="flex flex-col gap-2 sm:col-span-2">
            <ErrorText error={create.error} />
            {create.isSuccess && <SuccessText>{t("users.created_ok")}</SuccessText>}
            <Button type="submit" disabled={create.isPending} className="self-start">{t("users.add")}</Button>
          </div>
        </form>
      </Card>

      <Card title={t("settings.users")}>
        <ErrorText error={users.error} />
        <ul className="divide-y divide-border">
          {users.data?.map((u) => {
            const isSelf = u.id === me?.id;
            const expanded = open === u.id;
            return (
              <li key={u.id} className="flex flex-col gap-4 py-4" data-testid="user-row">
                <UserSummary user={u} isSelf={isSelf} access={u.isAdmin ? t("libraryAccess.all") : describeAccess(t, accessOf(u), libraries.data)}
                  expanded={expanded} onToggle={() => setOpen(expanded ? null : u.id)} />
                {expanded && <UserEditor user={u} isSelf={isSelf} onDeleted={() => setOpen(null)} />}
              </li>
            );
          })}
        </ul>
      </Card>
    </div>
  );
}

function UserSummary({ user, isSelf, access, expanded, onToggle }: {
  user: User; isSelf: boolean; access: string; expanded: boolean; onToggle: () => void;
}) {
  const { t } = useTranslation();
  const fmt = useDateTime();
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="min-w-0">
        <span className="font-medium">{user.username}</span>
        {user.displayName && <span className="ml-2 text-sm text-muted">{user.displayName}</span>}
      </span>
      {user.isAdmin ? <Badge tone="accent">{t("users.admin")}</Badge> : <Badge>{t("users.user")}</Badge>}
      {user.disabled && <Badge tone="danger">{t("users.disabled")}</Badge>}
      {user.totpEnabled && <Badge tone="success">2FA</Badge>}
      {isSelf && <Badge tone="success">{t("users.you")}</Badge>}
      {user.adultAccess && <Badge tone="danger">{t("adult.badge")}</Badge>}
      <span className="ml-auto flex flex-wrap items-center gap-3">
        <span className="text-xs text-muted" data-testid="user-access">{access}</span>
        <span className="text-xs text-muted">{t("users.lastLogin", { when: fmt(user.lastLoginAt) })}</span>
        <Button variant="secondary" aria-expanded={expanded} onClick={onToggle} data-testid="user-edit">
          {t("common.edit")}
          <ChevronDown className={`size-4 transition-transform duration-200 ${expanded ? "rotate-180" : ""}`} aria-hidden />
        </Button>
      </span>
    </div>
  );
}

function Section({ title, children, className = "" }: { title: string; children: ReactNode; className?: string }) {
  return (
    <section className={`flex flex-col gap-3 ${className}`}>
      <h3 className="text-sm font-semibold text-text/90">{title}</h3>
      {children}
    </section>
  );
}

/** Everything about one account: details, library access, permissions and the one-off actions. */
function UserEditor({ user, isSelf, onDeleted }: { user: User; isSelf: boolean; onDeleted: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const confirmDialog = useConfirm();
  const libraries = useAllLibraries();
  const hasAdult = Array.isArray(libraries.data) && libraries.data.some((l) => l.kind === "adult");

  const [username, setUsername] = useState(user.username);
  const [displayName, setDisplayName] = useState(user.displayName ?? "");
  const [isAdmin, setIsAdmin] = useState(user.isAdmin);
  const [enabled, setEnabled] = useState(!user.disabled);
  const [access, setAccess] = useState<LibraryAccess>(accessOf(user));
  const [perms, setPerms] = useState<Permissions>(user.permissions);
  const [password, setPassword] = useState("");
  const [settingPassword, setSettingPassword] = useState(false);
  const [done, setDone] = useState<string | null>(null);

  const afterChange = () => {
    void qc.invalidateQueries({ queryKey: ["users"] });
    if (isSelf) void qc.invalidateQueries({ queryKey: meQueryKey });
  };
  const patch = useMutation({
    mutationFn: async ({ body }: { body: Patch; message: string }) =>
      unwrap(await api.PATCH("/users/{userId}", { params: { path: { userId: user.id } }, body })),
    onSuccess: (_, { message }) => {
      setDone(message);
      afterChange();
    },
  });
  const adultAccess = useMutation({
    mutationFn: async (on: boolean) =>
      unwrap(await api.PUT("/users/{userId}/adult-access", { params: { path: { userId: user.id } }, body: { enabled: on, confirm: on } })),
    onSuccess: () => {
      setDone(null);
      afterChange();
      void qc.invalidateQueries({ queryKey: meQueryKey });
    },
  });
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/users/{userId}", { params: { path: { userId: user.id } } })),
    onSuccess: () => {
      onDeleted();
      afterChange();
    },
  });
  const busy = patch.isPending || adultAccess.isPending || remove.isPending;
  const run = (body: Patch, message: string) => {
    setDone(null);
    patch.mutate({ body, message });
  };

  const save = (e: FormEvent) => {
    e.preventDefault();
    const body: Patch = {};
    if (username.trim() !== user.username) body.username = username.trim();
    if (displayName.trim() !== (user.displayName ?? "")) body.displayName = displayName.trim();
    if (isAdmin !== user.isAdmin) body.isAdmin = isAdmin;
    if (!isSelf && enabled === user.disabled) body.disabled = !enabled;
    if (!isAdmin) {
      // allLibraries is owned by the library-access choice, so the two can never disagree.
      const fields = accessFields(access, perms);
      if (JSON.stringify(fields.permissions) !== JSON.stringify(user.permissions)) body.permissions = fields.permissions;
      if (!access.all) {
        const before = accessOf(user);
        const sameIds = before.ids.length === access.ids.length && access.ids.every((id) => before.ids.includes(id));
        if (!sameIds || before.all) body.libraryIds = access.ids;
        if (access.newLibraries !== before.newLibraries) body.newLibraries = access.newLibraries;
      }
    }
    if (Object.keys(body).length === 0) return setDone(t("common.saved"));
    run(body, t("common.saved"));
  };

  const toggleAdult = () => {
    if (user.adultAccess) return adultAccess.mutate(false);
    void confirmDialog({ message: t("adult.confirmGrant", { name: user.username }), danger: true })
      .then((ok) => ok && adultAccess.mutate(true));
  };

  return (
    <div className="flex flex-col gap-6 rounded-md border border-border bg-white/[0.02] p-4 sm:p-5 [animation:fade_160ms_ease-out_both]"
      data-testid="user-editor">
      <form className="flex flex-col gap-6" onSubmit={save}>
        <Section title={t("users.details")}>
          <div className="grid gap-4 sm:grid-cols-2">
            <TextField label={t("login.username")} required maxLength={64} autoComplete="off" value={username}
              onChange={(e) => setUsername(e.target.value)} />
            <TextField label={t("users.displayName")} maxLength={64} autoComplete="off" value={displayName}
              onChange={(e) => setDisplayName(e.target.value)} hint={t("users.displayNameHint")} />
          </div>
          <div className="flex flex-wrap gap-x-6 gap-y-2 text-sm">
            <label className="flex items-center gap-2">
              <input type="checkbox" className="accent-[var(--color-accent)]" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />
              {t("users.admin")}
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" className="accent-[var(--color-accent)]" checked={enabled} disabled={isSelf}
                onChange={(e) => setEnabled(e.target.checked)} />
              {t("users.enabled")}
              {isSelf && <span className="text-xs text-muted">{t("users.selfEnabledHint")}</span>}
            </label>
          </div>
        </Section>

        <Section title={t("libraryAccess.title")}>
          {isAdmin
            ? <p className="text-sm text-muted" data-testid="admin-all-libraries">{t("libraryAccess.adminAll")}</p>
            : <LibraryAccessPicker value={access} onChange={setAccess} />}
        </Section>

        <Section title={t("permissions.title")}>
          {isAdmin
            ? <p className="text-sm text-muted">{t("permissions.adminAll")}</p>
            : <PermissionsFields value={perms} onChange={setPerms} hide={["allLibraries"]} />}
        </Section>

        <div className="flex flex-wrap items-center gap-3">
          <Button type="submit" disabled={busy}>{t("users.saveChanges")}</Button>
          {done && <SuccessText>{done}</SuccessText>}
        </div>
      </form>

      <Section title={t("users.actions")}>
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" disabled={busy} aria-expanded={settingPassword} onClick={() => setSettingPassword(!settingPassword)}>
            {t("users.resetPassword")}
          </Button>
          {user.totpEnabled && !isSelf && (
            <Button variant="secondary" disabled={busy}
              onClick={() => void confirmDialog({ message: t("users.confirmReset2fa", { name: user.username }), danger: true })
                .then((ok) => ok && run({ resetTwoFactor: true }, t("users.reset2faDone")))}>
              {t("users.reset2fa")}
            </Button>
          )}
          {!isSelf && (
            <Button variant="secondary" disabled={busy}
              onClick={() => void confirmDialog({ message: t("users.confirmSignOutEverywhere", { name: user.username }), confirmLabel: t("users.signOutEverywhere") })
                .then((ok) => ok && run({ signOutEverywhere: true }, t("users.signedOutEverywhere")))}>
              {t("users.signOutEverywhere")}
            </Button>
          )}
          <Button variant="secondary" disabled={busy || !hasAdult} data-testid="adult-access" onClick={toggleAdult}
            aria-describedby={hasAdult ? undefined : `adult-hint-${user.id}`}>
            {user.adultAccess ? t("adult.revoke") : t("adult.grant")}
          </Button>
          {!isSelf && (
            <Button variant="danger" disabled={busy}
              onClick={() => void confirmDialog({ message: t("users.confirmDelete", { name: user.username }), danger: true }).then((ok) => ok && remove.mutate())}>
              {t("users.delete")}
            </Button>
          )}
        </div>
        {!hasAdult && libraries.data && (
          <p id={`adult-hint-${user.id}`} className="text-xs text-muted" data-testid="adult-access-hint">{t("adult.createLibraryFirst")}</p>
        )}
        {settingPassword && (
          <form className="flex flex-wrap items-end gap-2" onSubmit={(e) => {
            e.preventDefault();
            run({ password }, t("users.passwordSet"));
            setPassword("");
            setSettingPassword(false);
          }}>
            <TextField label={t("account.newPassword")} type="password" required minLength={8} autoComplete="new-password"
              value={password} onChange={(e) => setPassword(e.target.value)} />
            <Button type="submit" disabled={busy}>{t("common.save")}</Button>
          </form>
        )}
      </Section>
      <ErrorText error={patch.error ?? adultAccess.error ?? remove.error} />
    </div>
  );
}
