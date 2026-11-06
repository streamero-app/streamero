import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Logo } from "@/components/Logo";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { useSystemInfo } from "@/features/server/useSystemInfo";

export function LoginPage() {
  const { t } = useTranslation();
  const { data: info } = useSystemInfo();
  const [notice, setNotice] = useState(false);

  // Authentication is implemented in P1; the form is a shell for now.
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    setNotice(true);
  };

  return (
    <div className="relative flex min-h-full items-center justify-center bg-[radial-gradient(ellipse_at_top,var(--color-surface-2),var(--color-bg)_60%)] px-4">
      <div className="absolute top-4 right-4">
        <LanguageSwitcher />
      </div>
      <main className="w-full max-w-sm rounded-card border border-border bg-surface/80 p-8 shadow-2xl backdrop-blur">
        <Logo className="mb-2 justify-center" />
        <p className="mb-8 text-center text-sm text-muted">{t("app.tagline")}</p>
        <h1 className="mb-6 text-2xl font-semibold">{t("login.title")}</h1>
        <form className="flex flex-col gap-4" onSubmit={onSubmit}>
          <label className="flex flex-col gap-1 text-sm">
            {t("login.username")}
            <input name="username" autoComplete="username" required
              className="rounded-md border border-border bg-bg px-3 py-2 text-base outline-none focus:border-accent" />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            {t("login.password")}
            <input name="password" type="password" autoComplete="current-password"
              className="rounded-md border border-border bg-bg px-3 py-2 text-base outline-none focus:border-accent" />
          </label>
          <button type="submit" className="mt-2 rounded-md bg-accent px-4 py-2 font-medium text-accent-contrast hover:opacity-90">
            {t("login.submit")}
          </button>
          <button type="button" className="text-sm text-muted hover:text-text" onClick={() => setNotice(true)}>
            {t("login.quickConnect")}
          </button>
          {notice && <p role="alert" className="text-sm text-muted">{t("login.notAvailable")}</p>}
        </form>
        {info && (
          <p className="mt-8 text-center text-xs text-muted">
            {info.name} · {t("server.version", { version: info.version })}
          </p>
        )}
      </main>
    </div>
  );
}
