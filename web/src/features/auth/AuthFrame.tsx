import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Logo } from "@/components/Logo";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { useSystemInfo } from "@/features/server/useSystemInfo";

/** Centered card used by the sign-in and setup screens. */
export function AuthFrame({ title, intro, children, wide = false }: { title: string; intro?: ReactNode; children: ReactNode; wide?: boolean }) {
  const { t } = useTranslation();
  const { data: info } = useSystemInfo();
  return (
    <div className="relative flex min-h-full items-center justify-center bg-[radial-gradient(ellipse_at_top,var(--color-surface-2),var(--color-bg)_60%)] px-4 py-12">
      <div className="absolute top-4 right-4">
        <LanguageSwitcher />
      </div>
      <main className={`w-full ${wide ? "max-w-md" : "max-w-sm"} rounded-card border border-border bg-surface/80 p-8 shadow-2xl backdrop-blur`}>
        <Logo className="mb-2 justify-center" />
        <p className="mb-8 text-center text-sm text-muted">{t("app.tagline")}</p>
        <h1 className="mb-2 text-2xl font-semibold">{title}</h1>
        {intro && <div className="mb-6 text-sm text-muted">{intro}</div>}
        {!intro && <div className="mb-4" />}
        {children}
        {info && (
          <p className="mt-8 text-center text-xs text-muted">
            {info.name} · {t("server.version", { version: info.version })}
          </p>
        )}
      </main>
    </div>
  );
}
