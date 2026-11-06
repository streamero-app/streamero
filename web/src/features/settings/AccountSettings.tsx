import { useState, type FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Card, ErrorText, SuccessText, TextField } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { useDateTime } from "./SettingsLayout";

export function AccountSettings() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const fmt = useDateTime();
  const [form, setForm] = useState({ current: "", next: "", confirm: "" });
  const [mismatch, setMismatch] = useState(false);
  const change = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/auth/password", { body: { currentPassword: form.current, newPassword: form.next } })),
    onSuccess: () => setForm({ current: "", next: "", confirm: "" }),
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    setMismatch(form.next !== form.confirm);
    if (form.next === form.confirm) change.mutate();
  };

  return (
    <div className="flex flex-col gap-6">
      <Card title={t("settings.account")}>
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
          <dt className="text-muted">{t("login.username")}</dt>
          <dd>{me?.username}</dd>
          <dt className="text-muted">{t("users.role")}</dt>
          <dd>{me?.isAdmin ? <Badge tone="accent">{t("users.admin")}</Badge> : <Badge>{t("users.user")}</Badge>}</dd>
          <dt className="text-muted">{t("users.created")}</dt>
          <dd>{fmt(me?.createdAt)}</dd>
        </dl>
      </Card>
      <Card title={t("account.changePassword")}>
        <form className="flex max-w-sm flex-col gap-4" onSubmit={onSubmit}>
          <TextField label={t("account.currentPassword")} type="password" autoComplete="current-password" required
            value={form.current} onChange={(e) => setForm({ ...form, current: e.target.value })} />
          <TextField label={t("account.newPassword")} type="password" autoComplete="new-password" required minLength={8}
            value={form.next} onChange={(e) => setForm({ ...form, next: e.target.value })} hint={t("users.passwordHint")} />
          <TextField label={t("setup.confirmPassword")} type="password" autoComplete="new-password" required minLength={8}
            value={form.confirm} onChange={(e) => setForm({ ...form, confirm: e.target.value })} />
          {mismatch && <p role="alert" className="text-sm text-danger">{t("errors.password_mismatch")}</p>}
          <ErrorText error={change.error} />
          {change.isSuccess && <SuccessText>{t("account.passwordChanged")}</SuccessText>}
          <Button type="submit" disabled={change.isPending} className="self-start">{t("account.changePassword")}</Button>
        </form>
      </Card>
    </div>
  );
}
