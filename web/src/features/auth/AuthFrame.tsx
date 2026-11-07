import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Mark } from "@/components/Logo";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { useSystemInfo } from "@/features/server/useSystemInfo";

/** Sign-in and setup screens: the mark in a dark room, lit from below by the projector. */
export function AuthFrame({ title, intro, children, wide = false }: { title: string; intro?: ReactNode; children: ReactNode; wide?: boolean }) {
  const { t } = useTranslation();
  const { data: info } = useSystemInfo();
  return (
    <div className="relative isolate flex min-h-full items-center justify-center overflow-hidden px-4 py-16">
      <div aria-hidden className="absolute inset-0 -z-10"
        style={{ background: "radial-gradient(60% 45% at 50% 105%, rgb(255 150 50 / 0.22), transparent 70%), radial-gradient(40% 35% at 50% 0%, rgb(255 236 214 / 0.05), transparent 70%)" }} />
      <div aria-hidden className="absolute inset-x-0 bottom-0 -z-10 h-px bg-gradient-to-r from-transparent via-accent/40 to-transparent" />
      <div className="absolute top-4 right-4"><LanguageSwitcher /></div>
      <main className={`enter w-full ${wide ? "max-w-md" : "max-w-sm"}`}>
        <div className="mb-8 flex flex-col items-center gap-4 text-center">
          <Mark size={56} className="rounded-[14px] shadow-[0_20px_50px_-15px_rgb(255_150_50/0.45)]" />
          <div>
            <p className="text-[22px] font-semibold tracking-[-0.02em]">{t("app.name")}</p>
            <p className="mt-1 text-sm text-muted">{t("app.tagline")}</p>
          </div>
        </div>
        <div className="rounded-[18px] border border-border bg-surface/80 p-7 shadow-[0_40px_80px_-30px_rgb(0_0_0/0.9)] backdrop-blur-xl sm:p-8">
          <h1 className="mb-2 font-display text-[34px] leading-none">{title}</h1>
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
