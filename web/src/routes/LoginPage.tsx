import { useState, type FormEvent } from "react";
import { Navigate, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Button, ErrorText, TextField } from "@/components/ui";
import { AuthFrame } from "@/features/auth/AuthFrame";
import { useLogin, useMe } from "@/features/auth/useAuth";
import { useSystemInfo } from "@/features/server/useSystemInfo";

export function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: info } = useSystemInfo();
  const { data: me } = useMe();
  const login = useLogin();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  if (info && !info.setupCompleted) return <Navigate to="/setup" />;
  if (me) return <Navigate to="/" />;

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate({ username, password }, { onSuccess: () => void navigate({ to: "/" }) });
  };

  return (
    <AuthFrame title={t("login.title")}>
      <form className="flex flex-col gap-4" onSubmit={onSubmit}>
        <TextField label={t("login.username")} name="username" autoComplete="username" required autoFocus
          value={username} onChange={(e) => setUsername(e.target.value)} />
        <TextField label={t("login.password")} name="password" type="password" autoComplete="current-password" required
          value={password} onChange={(e) => setPassword(e.target.value)} />
        <ErrorText error={login.error} />
        <Button type="submit" className="mt-2" disabled={login.isPending}>
          {login.isPending ? t("login.signingIn") : t("login.submit")}
        </Button>
      </form>
    </AuthFrame>
  );
}
