import { useState, type FormEvent } from "react";
import { Navigate, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { KeyRound } from "lucide-react";
import { Button, ErrorText, TextField, buttonVariants, pressable } from "@/components/ui";
import { AuthFrame } from "@/features/auth/AuthFrame";
import { useLogin, useMe } from "@/features/auth/useAuth";
import { ssoErrorKey, takeQueryParam, useAuthProviders } from "@/features/auth/useSso";
import { useSystemInfo } from "@/features/server/useSystemInfo";
import { ApiError } from "@/api/client";
import { QuickConnectLogin } from "@/features/auth/QuickConnectLogin";

export function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: info } = useSystemInfo();
  const { data: me } = useMe();
  const { data: methods } = useAuthProviders();
  const login = useLogin();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [quick, setQuick] = useState(false);
  // A failed single sign-on comes back as /login?sso_error=<code>.
  const [ssoError] = useState(() => takeQueryParam("sso_error"));
  // The server answers totp_required once the password is right; then we ask for the code.
  const needsCode = login.error instanceof ApiError && ["totp_required", "invalid_totp"].includes(login.error.code)
    || totpCode !== "";

  if (info && !info.setupCompleted) return <Navigate to="/setup" />;
  if (me) return <Navigate to="/" />;

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate({ username, password, totpCode: totpCode || undefined }, { onSuccess: () => void navigate({ to: "/" }) });
  };

  if (quick) {
    return (
      <AuthFrame title={t("settings.quickConnect")}>
        <QuickConnectLogin onCancel={() => setQuick(false)} />
      </AuthFrame>
    );
  }

  const providers = methods?.oidc ?? [];
  const passwordForm = methods?.passwordLogin ?? true;

  return (
    <AuthFrame title={t("login.title")}>
      <div className="flex flex-col gap-4">
        {ssoError && <p role="alert" className="text-sm text-danger">{t(ssoErrorKey(ssoError))}</p>}
        {providers.length > 0 && (
          <div className="flex flex-col gap-2" data-testid="sso-buttons">
            {providers.map((p) => (
              // A real navigation: the server redirects to the identity provider.
              <a key={p.id} href={p.authorizeUrl} className={`${pressable} ${buttonVariants.secondary} inline-flex h-11 items-center justify-center gap-2 rounded-[6px] px-4 text-sm font-semibold`}>
                <KeyRound className="size-4" aria-hidden />
                {t("login.continueWith", { name: p.label })}
              </a>
            ))}
          </div>
        )}
        {providers.length > 0 && passwordForm && (
          <p className="flex items-center gap-3 text-xs tracking-[0.08em] text-faint uppercase before:h-px before:flex-1 before:bg-border after:h-px after:flex-1 after:bg-border">
            {t("login.or")}
          </p>
        )}
        {passwordForm && (
          <form className="flex flex-col gap-4" onSubmit={onSubmit}>
            <TextField label={t("login.username")} name="username" autoComplete="username" required autoFocus={providers.length === 0}
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
        )}
        <Button type="button" variant="ghost" onClick={() => setQuick(true)}>{t("quickconnect.useQuickConnect")}</Button>
      </div>
    </AuthFrame>
  );
}
