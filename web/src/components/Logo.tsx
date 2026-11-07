import { useTranslation } from "react-i18next";

/** The split-play mark (same art as the app icon). */
export function Mark({ size = 32, className = "" }: { size?: number; className?: string }) {
  return <img src="/favicon.svg" alt="" width={size} height={size} className={`shrink-0 ${className}`} draggable={false} />;
}

export function Logo({ className = "", size = 30 }: { className?: string; size?: number }) {
  const { t } = useTranslation();
  return (
    <div className={`flex items-center gap-2.5 select-none ${className}`}>
      <Mark size={size} />
      <span className="text-[19px] font-semibold tracking-[-0.02em]">{t("app.name")}</span>
    </div>
  );
}
