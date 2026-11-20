import "@testing-library/jest-dom/vitest";
import { configure } from "@testing-library/react";
import i18n from "./i18n";
import en from "./i18n/locales/en.json";
import hr from "./i18n/locales/hr.json";

// The app loads one language at a time; tests switch freely, so both are here up front.
i18n.addResourceBundle("en", "translation", en, true, true);
i18n.addResourceBundle("hr", "translation", hr, true, true);
void i18n.changeLanguage("en");

// Pages load their chunks lazily; on a busy machine the first render of a page can take
// longer than the default 1 s of findBy*/waitFor, which made tests fail at random.
configure({ asyncUtilTimeout: 4000 });
