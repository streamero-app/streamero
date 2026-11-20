import i18n from "i18next";
import { initReactI18next } from "react-i18next";

/** English is the primary language; the others are translations of it. */
export const SUPPORTED_LANGUAGES = ["en", "hr"] as const;
export type Language = (typeof SUPPORTED_LANGUAGES)[number];
const STORAGE_KEY = "streamero.lang";

// Each language is its own chunk: a visitor downloads only the one they read
// (about 65 kB each), not every translation inside the main bundle.
const loaders: Record<Language, () => Promise<{ default: Record<string, unknown> }>> = {
  en: () => import("./locales/en.json"),
  hr: () => import("./locales/hr.json"),
};

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

/** Fetches a language's strings once; resolves at once when they are already here. */
export function loadLanguage(lang: Language): Promise<void> {
  if (i18n.hasResourceBundle(lang, "translation")) return Promise.resolve();
  return loaders[lang]().then((m) => {
    i18n.addResourceBundle(lang, "translation", m.default, true, true);
  });
}

/** The page's language attribute follows the UI (screen readers, hyphenation, spell-check). */
function markDocument(lng: string) {
  if (typeof document !== "undefined") document.documentElement.lang = lng;
}

export function setLanguage(lang: Language) {
  try {
    localStorage.setItem(STORAGE_KEY, lang);
  } catch {
    /* storage unavailable */
  }
  // Already loaded (or in tests): switch now; otherwise once the strings arrive.
  if (i18n.hasResourceBundle(lang, "translation")) void i18n.changeLanguage(lang);
  else void loadLanguage(lang).then(() => i18n.changeLanguage(lang), () => undefined);
}

const lng = initialLanguage();
void i18n.use(initReactI18next).init({
  resources: {},
  partialBundledLanguages: true,
  lng,
  fallbackLng: "en",
  interpolation: { escapeValue: false },
});
markDocument(lng);
i18n.on("languageChanged", markDocument);

/**
 * Resolves when the visitor's language can be shown. English (the fallback for any
 * missing translation) follows in the background when another language is in use.
 */
export const i18nReady: Promise<void> = loadLanguage(lng)
  .catch(() => (lng === "en" ? undefined : loadLanguage("en")))
  .then(() => {
    if (lng !== "en") void loadLanguage("en").catch(() => undefined);
  });

export default i18n;
