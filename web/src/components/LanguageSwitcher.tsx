import { useTranslation } from "react-i18next";
import { SUPPORTED_LANGUAGES, setLanguage, type Language } from "@/i18n";

export function LanguageSwitcher() {
  const { t, i18n } = useTranslation();
  return (
    <label className="flex items-center gap-2 text-sm text-muted">
      <span className="sr-only">{t("language.label")}</span>
      <select
        aria-label={t("language.label")}
        className="rounded-md border border-border bg-surface px-2 py-1 text-text"
        value={i18n.resolvedLanguage}
        onChange={(e) => setLanguage(e.target.value as Language)}
      >
        {SUPPORTED_LANGUAGES.map((l) => (
          <option key={l} value={l}>
            {t(`language.${l}`)}
          </option>
        ))}
      </select>
    </label>
  );
}
