import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap, type User } from "@/api/client";
import { Badge, Button, Card, ErrorText, SuccessText, TextField } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { useDateTime } from "./SettingsLayout";

type Patch = { isAdmin?: boolean; disabled?: boolean; password?: string; resetTwoFactor?: boolean };

export function UsersSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const fmt = useDateTime();
  const { data: me } = useMe();
  const users = useQuery({ queryKey: ["users"], queryFn: async () => unwrap(await api.GET("/users")) });
  const refresh = () => qc.invalidateQueries({ queryKey: ["users"] });

  const [form, setForm] = useState({ username: "", password: "", isAdmin: false });
  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/users", { body: form })),
    onSuccess: () => {
      setForm({ username: "", password: "", isAdmin: false });
      void refresh();
    },
  });
  const update = useMutation({
    mutationFn: async ({ id, patch }: { id: string; patch: Patch }) =>
      unwrap(await api.PATCH("/users/{userId}", { params: { path: { userId: id } }, body: patch })),
    onSuccess: () => void refresh(),
  });
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/users/{userId}", { params: { path: { userId: id } } })),
    onSuccess: () => void refresh(),
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
            <input type="checkbox" checked={form.isAdmin} onChange={(e) => setForm({ ...form, isAdmin: e.target.checked })} />
            {t("users.makeAdmin")}
          </label>
          <div className="flex flex-col gap-2 sm:col-span-2">
            <ErrorText error={create.error} />
            {create.isSuccess && <SuccessText>{t("users.created_ok")}</SuccessText>}
            <Button type="submit" disabled={create.isPending} className="self-start">{t("users.add")}</Button>
          </div>
        </form>
      </Card>

      <Card title={t("settings.users")}>
        <ErrorText error={users.error ?? update.error ?? remove.error} />
        <ul className="divide-y divide-border">
          {users.data?.map((u) => (
            <UserRow key={u.id} user={u} isSelf={u.id === me?.id} fmt={fmt} busy={update.isPending || remove.isPending}
              onPatch={(patch) => update.mutate({ id: u.id, patch })}
              onDelete={() => {
                if (window.confirm(t("users.confirmDelete", { name: u.username }))) remove.mutate(u.id);
              }} />
          ))}
        </ul>
      </Card>
    </div>
  );
}

function UserRow({ user, isSelf, fmt, busy, onPatch, onDelete }: {
  user: User; isSelf: boolean; fmt: (iso?: string) => string; busy: boolean;
  onPatch: (p: Patch) => void; onDelete: () => void;
}) {
  const { t } = useTranslation();
  const [resetting, setResetting] = useState(false);
  const [password, setPassword] = useState("");
  return (
    <li className="flex flex-col gap-3 py-4" data-testid="user-row">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{user.username}</span>
        {user.isAdmin ? <Badge tone="accent">{t("users.admin")}</Badge> : <Badge>{t("users.user")}</Badge>}
        {user.disabled && <Badge tone="danger">{t("users.disabled")}</Badge>}
        {user.totpEnabled && <Badge tone="success">2FA</Badge>}
        {isSelf && <Badge tone="success">{t("users.you")}</Badge>}
        <span className="ml-auto text-xs text-muted">{t("users.lastLogin", { when: fmt(user.lastLoginAt) })}</span>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button variant="secondary" disabled={busy} onClick={() => onPatch({ isAdmin: !user.isAdmin })}>
          {user.isAdmin ? t("users.removeAdmin") : t("users.makeAdmin")}
        </Button>
        {!isSelf && (
          <Button variant="secondary" disabled={busy} onClick={() => onPatch({ disabled: !user.disabled })}>
            {user.disabled ? t("users.enable") : t("users.disable")}
          </Button>
        )}
        <Button variant="secondary" disabled={busy} onClick={() => setResetting(!resetting)}>{t("users.resetPassword")}</Button>
        {user.totpEnabled && !isSelf && (
          <Button variant="secondary" disabled={busy}
            onClick={() => window.confirm(t("users.confirmReset2fa", { name: user.username })) && onPatch({ resetTwoFactor: true })}>
            {t("users.reset2fa")}
          </Button>
        )}
        {!isSelf && <Button variant="danger" disabled={busy} onClick={onDelete}>{t("users.delete")}</Button>}
      </div>
      {resetting && (
        <form className="flex flex-wrap items-end gap-2" onSubmit={(e) => {
          e.preventDefault();
          onPatch({ password });
          setPassword("");
          setResetting(false);
        }}>
          <TextField label={t("account.newPassword")} type="password" required minLength={8} autoComplete="new-password"
            value={password} onChange={(e) => setPassword(e.target.value)} />
          <Button type="submit" disabled={busy}>{t("common.save")}</Button>
        </form>
      )}
    </li>
  );
}
