import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Mark } from "@/components/Logo";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { useSystemInfo } from "@/features/server/useSystemInfo";

/** Sign-in and setup screens: a dark cinema before the film, the screen's light low on the floor. */
export function AuthFrame({ title, intro, children, wide = false }: { title: string; intro?: ReactNode; children: ReactNode; wide?: boolean }) {
  const { t } = useTranslation();
  const { data: info } = useSystemInfo();
  return (
    <div className="relative isolate flex min-h-full items-center justify-center overflow-hidden px-4 py-16">
      <div aria-hidden className="absolute inset-0 -z-10"
        style={{ background: "radial-gradient(70% 50% at 50% 110%, color-mix(in oklab, var(--amb-a) 26%, transparent), transparent 70%)" }} />
      <div className="absolute top-4 right-4"><LanguageSwitcher /></div>
      <main className={`enter w-full ${wide ? "max-w-md" : "max-w-sm"}`}>
        <div className="mb-10 flex items-center gap-3">
          <Mark size={36} />
          <div>
            <p className="font-head text-[19px] tracking-[0.05em]">{t("app.name")}</p>
            <p className="text-sm text-muted">{t("app.tagline")}</p>
          </div>
        </div>
        <div className="rounded-[8px] border border-border bg-surface/90 p-7 shadow-[0_40px_80px_-30px_rgb(0_0_0/0.9)] backdrop-blur-xl sm:p-8">
          <h1 className="mb-3 font-display text-[34px] text-balance">{title}</h1>
          {intro ? <div className="mb-6 text-sm leading-relaxed text-muted">{intro}</div> : <div className="mb-5" />}
          {children}
        </div>
        {info && (
          <p className="mt-6 text-center text-xs text-faint">
            {info.name} · {t("server.version", { version: info.version })}
          </p>
        )}
      </main>
    </div>
  );
}
