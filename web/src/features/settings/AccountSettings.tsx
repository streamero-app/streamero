import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Card, ErrorText, Select, SuccessText, TextField } from "@/components/ui";
import { meQueryKey, useMe } from "@/features/auth/useAuth";
import { MAIN_COLOR } from "@/features/profiles/ProfileSwitcher";
import { SUPPORTED_LANGUAGES, setLanguage, type Language } from "@/i18n";
import { AvatarPicker } from "./AvatarPicker";
import { SettingRow } from "./SettingRow";
import { useDateTime } from "./SettingsLayout";
import { TwoFactorCard } from "./TwoFactorCard";

export function AccountSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: me } = useMe();
  const fmt = useDateTime();
  const [profile, setProfile] = useState({ displayName: "", email: "", language: "en" as Language });
  useEffect(() => {
    if (me) setProfile({ displayName: me.displayName ?? "", email: me.email ?? "", language: (me.language as Language | undefined) ?? "en" });
  }, [me]);
  const saveProfile = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/auth/me", { body: profile })),
    onSuccess: (u) => {
      qc.setQueryData(meQueryKey, u);
      setLanguage(profile.language);
    },
  });
  const [form, setForm] = useState({ current: "", next: "", confirm: "" });
  const [mismatch, setMismatch] = useState(false);
  const change = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/auth/password", { body: { currentPassword: form.current, newPassword: form.next } })),
    onSuccess: () => setForm({ current: "", next: "", confirm: "" }),
  });
  const onPassword = (e: FormEvent) => {
    e.preventDefault();
    setMismatch(form.next !== form.confirm);
    if (form.next === form.confirm) change.mutate();
  };
  if (!me) return null;
  const shown = me.displayName || me.username;

  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-wrap items-center gap-6 rounded-card border border-border bg-[linear-gradient(120deg,color-mix(in_oklab,var(--amb-a)_16%,var(--color-surface)),var(--color-surface)_60%)] p-6" data-testid="account-header">
        <AvatarPicker ownerId={me.id} name={shown} color={MAIN_COLOR} hasAvatar={me.hasAvatar} size={104} />
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-display text-4xl">{shown}</h2>
          <p className="mt-2 flex flex-wrap items-center gap-2 text-sm text-text/60">
            <span>@{me.username}</span>
            {me.isAdmin ? <Badge tone="accent">{t("users.admin")}</Badge> : <Badge>{t("users.user")}</Badge>}
            {me.totpEnabled && <Badge tone="success">{t("account.twoFactorOn")}</Badge>}
          </p>
          <p className="mt-1 text-[13px] text-text/45">{t("account.memberSince", { date: fmt(me.createdAt) })}{me.lastLoginAt ? ` · ${t("account.lastSignIn", { date: fmt(me.lastLoginAt) })}` : ""}</p>
        </div>
      </section>

      <form onSubmit={(e) => { e.preventDefault(); saveProfile.reset(); saveProfile.mutate(); }}>
        <Card title={t("account.profileTitle")}>
          <SettingRow label={t("account.displayName")} hint={t("account.displayNameHint")}>
            {(id) => <input id={id} maxLength={60} value={profile.displayName} placeholder={me.username}
              onChange={(e) => setProfile({ ...profile, displayName: e.target.value })}
              className="h-10 w-full rounded-[6px] border border-border bg-black/30 px-3 text-[15px] text-text placeholder:text-faint hover:border-border-strong focus:border-accent/70" />}
          </SettingRow>
          <SettingRow label={t("account.email")} hint={t("account.emailHint")}>
            {(id) => <input id={id} type="email" maxLength={200} value={profile.email} autoComplete="email"
              onChange={(e) => setProfile({ ...profile, email: e.target.value })}
              className="h-10 w-full rounded-[6px] border border-border bg-black/30 px-3 text-[15px] text-text placeholder:text-faint hover:border-border-strong focus:border-accent/70" />}
          </SettingRow>
          <SettingRow label={t("account.language")} hint={t("account.languageHint")}>
            {(id) => (
              <Select id={id} className="h-10 w-full" value={profile.language} onChange={(e) => setProfile({ ...profile, language: e.target.value as Language })}>
                {SUPPORTED_LANGUAGES.map((l) => <option key={l} value={l}>{t(`language.${l}`)}</option>)}
              </Select>
            )}
          </SettingRow>
          <div className="mt-5 flex flex-wrap items-center gap-4">
            <Button type="submit" disabled={saveProfile.isPending}>{t("common.save")}</Button>
            {saveProfile.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
            <ErrorText error={saveProfile.error} />
          </div>
        </Card>
      </form>

      <Card title={t("account.changePassword")}>
        <form className="flex max-w-sm flex-col gap-4" onSubmit={onPassword}>
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
      <TwoFactorCard />
    </div>
  );
}
