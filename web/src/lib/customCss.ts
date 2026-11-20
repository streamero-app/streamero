/**
 * The administrator's extra CSS (Settings → Branding) is applied on every page,
 * including sign-in. It is theme text, not code, so before it reaches a <style>
 * element everything that can make the browser talk to another host is removed:
 * `@import`, `@font-face` sources and `url()` / `image-set()` pointing anywhere
 * but this server or inline data. Without that, attribute selectors plus remote
 * backgrounds can read what is typed into a form (CSS exfiltration).
 */

const origin = () => globalThis.location?.origin ?? "http://localhost";

/** Undoes CSS escapes (`\75 rl(` is `url(`) so the checks see what the browser sees. */
function unescapeCss(css: string): string {
  return css
    .replace(/\\([0-9a-f]{1,6})[ \t\n\r\f]?/gi, (_, hex: string) => {
      const code = parseInt(hex, 16);
      return code > 0 && code <= 0x10ffff ? String.fromCodePoint(code) : "�";
    })
    .replace(/\\(\r\n|[\n\r\f])/g, "")
    .replace(/\\([^\n\r\f0-9a-f])/gi, "$1");
}

function allowedTarget(raw: string): boolean {
  const target = raw.trim().replace(/^(["'])([\s\S]*)\1$/, "$2").trim();
  if (!target) return true;
  if (/^data:(image|font)\//i.test(target)) return true;
  if (target.startsWith("#")) return true; // SVG fragment in this document
  if (/[\u0000-\u001f\\]/.test(target)) return false;
  try {
    const url = new URL(target, origin());
    return url.origin === origin() && (url.protocol === "http:" || url.protocol === "https:");
  } catch {
    return false;
  }
}

/** Returns the CSS with every way of reaching another host removed. */
export function sanitizeCustomCss(input: string | null | undefined): string {
  if (!input) return "";
  let css = unescapeCss(String(input).slice(0, 20000));
  // Comments can hide tokens from the checks below ("u/**/rl(").
  css = css.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\*[\s\S]*$/, "");
  // A style element ends at the first closing tag, whatever the CSS says.
  css = css.replace(/<\/?\s*style/gi, "");
  css = css.replace(/@import\b[^;]*(;|$)/gi, "");
  css = css.replace(/@namespace\b[^;]*(;|$)/gi, "");
  // Legacy script hooks in CSS.
  css = css.replace(/expression\s*\(/gi, "(").replace(/-moz-binding\s*:[^;}]*/gi, "").replace(/behavior\s*:[^;}]*/gi, "");
  // url(...) with a quoted or bare target.
  css = css.replace(/url\(\s*("[^"]*"|'[^']*'|[^)]*)\s*\)/gi, (m, target: string) => (allowedTarget(target) ? m : "none"));
  // An unterminated url( swallows the rest of the sheet: drop it.
  css = css.replace(/url\([^)]*$/i, "none");
  // image-set("…") and src("…") take bare strings as addresses.
  css = css.replace(/\b(image-set|-webkit-image-set|src|image)\(([^()]*(?:\([^()]*\)[^()]*)*)\)/gi, (m, _fn: string, body: string) => {
    const strings = body.match(/"[^"]*"|'[^']*'/g) ?? [];
    return strings.every((s) => allowedTarget(s)) ? m : "none";
  });
  return css.trim();
}
