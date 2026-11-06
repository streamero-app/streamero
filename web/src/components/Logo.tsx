import { useTranslation } from "react-i18next";

/** Placeholder wordmark; replaced by the official streamero.net logo asset. */
export function Logo({ className = "" }: { className?: string }) {
  const { t } = useTranslation();
  return (
    <div className={`flex items-center gap-2 select-none ${className}`}>
      <img src="/favicon.svg" alt="" width={32} height={32} className="rounded-lg" />
      <span className="text-xl font-semibold tracking-tight">{t("app.name")}</span>
    </div>
  );
}
