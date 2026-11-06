import en from "./locales/en.json";
import hr from "./locales/hr.json";

function keys(obj: object, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([k, v]) =>
    v && typeof v === "object" ? keys(v, `${prefix}${k}.`) : [`${prefix}${k}`],
  );
}

describe("i18n", () => {
  it("HR and EN define exactly the same keys", () => {
    expect(keys(hr).sort()).toEqual(keys(en).sort());
  });
  it("has no empty strings", () => {
    for (const dict of [en, hr]) {
      const flat = JSON.stringify(dict);
      expect(flat).not.toMatch(/:""/);
    }
  });
});
