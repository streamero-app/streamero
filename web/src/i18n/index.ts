import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import en from "./locales/en.json";
import hr from "./locales/hr.json";

/** English is the primary language; the others are translations of it. */
export const SUPPORTED_LANGUAGES = ["en", "hr"] as const;
export type Language = (typeof SUPPORTED_LANGUAGES)[number];
const STORAGE_KEY = "streamero.lang";

function initialLanguage(): Language {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === "hr" || saved === "en") return saved;
  } catch {
    /* storage unavailable */
  }
  // English until someone picks another language (on the sign-in page or in their account).
  return "en";
}

export function setLanguage(lang: Language) {
  void i18n.changeLanguage(lang);
  try {
    localStorage.setItem(STORAGE_KEY, lang);
  } catch {
    /* storage unavailable */
  }
}

void i18n.use(initReactI18next).init({
  resources: { en: { translation: en }, hr: { translation: hr } },
  lng: initialLanguage(),
  fallbackLng: "en",
  interpolation: { escapeValue: false },
});

i18n.on("languageChanged", (lng) => {
  if (typeof document !== "undefined") document.documentElement.lang = lng;
});

export default i18n;
