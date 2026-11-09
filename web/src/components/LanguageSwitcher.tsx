import { useTranslation } from "react-i18next";
import { SUPPORTED_LANGUAGES, setLanguage, type Language } from "@/i18n";
import { Select } from "./ui";

export function LanguageSwitcher() {
  const { t, i18n } = useTranslation();
  return (
    <Select aria-label={t("language.label")} className="bg-black/35 backdrop-blur-md"
      value={i18n.resolvedLanguage} onChange={(e) => setLanguage(e.target.value as Language)}>
      {SUPPORTED_LANGUAGES.map((l) => <option key={l} value={l}>{t(`language.${l}`)}</option>)}
    </Select>
  );
}
