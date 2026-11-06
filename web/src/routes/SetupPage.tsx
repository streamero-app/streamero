import { useState, type FormEvent } from "react";
import { Navigate, useNavigate } from "@tanstack/react-router";
import { Trans, useTranslation } from "react-i18next";
import { Button, ErrorText, TextField } from "@/components/ui";
import { AuthFrame } from "@/features/auth/AuthFrame";
import { useSetup } from "@/features/auth/useAuth";
import { useSystemInfo } from "@/features/server/useSystemInfo";

/** First-run wizard: setup code → server name → first admin account. */
export function SetupPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: info } = useSystemInfo();
  const setup = useSetup();
  const [form, setForm] = useState({ setupCode: "", serverName: "", username: "", password: "", confirm: "" });
  const [mismatch, setMismatch] = useState(false);

  if (info?.setupCompleted && !setup.isSuccess) return <Navigate to="/login" />;

  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (form.password !== form.confirm) {
      setMismatch(true);
      return;
    }
    setMismatch(false);
    const { confirm: _, ...body } = form;
    setup.mutate({ ...body, serverName: body.serverName.trim() || (info?.name ?? "") },
      { onSuccess: () => void navigate({ to: "/" }) });
  };

  return (
    <AuthFrame wide title={t("setup.title")} intro={t("setup.intro")}>
      <form className="flex flex-col gap-4" onSubmit={onSubmit}>
        <TextField label={t("setup.code")} name="setupCode" required autoFocus autoComplete="off" spellCheck={false}
          placeholder="XXXX-XXXX" value={form.setupCode} onChange={set("setupCode")}
          hint={<Trans i18nKey="setup.codeHint" components={{ code: <code className="rounded bg-surface-2 px-1" /> }} />} />
        <TextField label={t("setup.serverName")} name="serverName" placeholder={info?.name} maxLength={100}
          value={form.serverName} onChange={set("serverName")} />
        <hr className="border-border" />
        <p className="text-sm font-medium">{t("setup.adminAccount")}</p>
        <TextField label={t("login.username")} name="username" autoComplete="username" required maxLength={64}
          value={form.username} onChange={set("username")} />
        <TextField label={t("login.password")} name="password" type="password" autoComplete="new-password" required minLength={8}
          value={form.password} onChange={set("password")} hint={t("users.passwordHint")} />
        <TextField label={t("setup.confirmPassword")} name="confirm" type="password" autoComplete="new-password" required minLength={8}
          value={form.confirm} onChange={set("confirm")} />
        {mismatch && <p role="alert" className="text-sm text-danger">{t("errors.password_mismatch")}</p>}
        <ErrorText error={setup.error} />
        <Button type="submit" className="mt-2" disabled={setup.isPending}>
          {t("setup.submit")}
        </Button>
      </form>
    </AuthFrame>
  );
}
