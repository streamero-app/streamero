import { useState, type FormEvent } from "react";
import { Navigate, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Button, ErrorText, TextField } from "@/components/ui";
import { AuthFrame } from "@/features/auth/AuthFrame";
import { useLogin, useMe } from "@/features/auth/useAuth";
import { useSystemInfo } from "@/features/server/useSystemInfo";
import { ApiError } from "@/api/client";

export function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: info } = useSystemInfo();
  const { data: me } = useMe();
  const login = useLogin();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  // The server answers totp_required once the password is right; then we ask for the code.
  const needsCode = login.error instanceof ApiError && ["totp_required", "invalid_totp"].includes(login.error.code)
    || totpCode !== "";

  if (info && !info.setupCompleted) return <Navigate to="/setup" />;
  if (me) return <Navigate to="/" />;

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate({ username, password, totpCode: totpCode || undefined }, { onSuccess: () => void navigate({ to: "/" }) });
  };

  return (
    <AuthFrame title={t("login.title")}>
      <form className="flex flex-col gap-4" onSubmit={onSubmit}>
        <TextField label={t("login.username")} name="username" autoComplete="username" required autoFocus
          value={username} onChange={(e) => setUsername(e.target.value)} />
        <TextField label={t("login.password")} name="password" type="password" autoComplete="current-password" required
          value={password} onChange={(e) => setPassword(e.target.value)} />
        {needsCode && (
          <TextField label={t("twofa.code")} name="totpCode" inputMode="text" autoComplete="one-time-code" required autoFocus
            maxLength={32} value={totpCode} onChange={(e) => setTotpCode(e.target.value)} hint={t("twofa.loginHint")} />
        )}
        {!(login.error instanceof ApiError && login.error.code === "totp_required") && <ErrorText error={login.error} />}
        <Button type="submit" className="mt-2" disabled={login.isPending}>
          {login.isPending ? t("login.signingIn") : t("login.submit")}
        </Button>
      </form>
    </AuthFrame>
  );
}
