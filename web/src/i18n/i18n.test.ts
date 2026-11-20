import en from "./locales/en.json";
import hr from "./locales/hr.json";
import i18n, { setLanguage } from "./index";

function keys(obj: object, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([k, v]) =>
    v && typeof v === "object" ? keys(v, `${prefix}${k}.`) : [`${prefix}${k}`],
  );
}

describe("i18n", () => {
  it("HR and EN define exactly the same keys", () => {
    expect(keys(hr).sort()).toEqual(keys(en).sort());
  });
  it("sets the page language with the UI language", () => {
    setLanguage("hr");
    expect(document.documentElement.lang).toBe("hr");
    expect(i18n.t("errors.tryAgain")).toBe("Pokušaj ponovno");
    setLanguage("en");
    expect(document.documentElement.lang).toBe("en");
  });

  it("gives every Croatian plural its three forms", () => {
    const flat = keys(hr);
    const bases = new Set(flat.filter((k) => /_(one|few|other)$/.test(k) && !k.startsWith("music.repeat_")).map((k) => k.replace(/_(one|few|other)$/, "")));
    for (const b of bases) for (const form of ["one", "few", "other"]) expect(flat).toContain(`${b}_${form}`);
  });

  it("has no empty strings", () => {
    for (const dict of [en, hr]) {
      const flat = JSON.stringify(dict);
      expect(flat).not.toMatch(/:""/);
    }
  });
});
