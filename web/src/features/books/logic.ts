// Pure logic shared by the readers and the audiobook player (unit-tested, no DOM).

export type Part = { fileId: string; title: string; offsetMs: number; durationMs: number };
export type Chapter = { title: string; startMs: number; endMs: number };

/** Which part holds a position in the whole book, and where in that part. */
export function locate(parts: Part[], ms: number): { index: number; localMs: number } {
  if (!parts.length) return { index: -1, localMs: 0 };
  const total = parts[parts.length - 1]!.offsetMs + parts[parts.length - 1]!.durationMs;
  const pos = Math.min(Math.max(0, ms), Math.max(0, total - 1));
  for (let i = parts.length - 1; i >= 0; i--) {
    if (pos >= parts[i]!.offsetMs) return { index: i, localMs: pos - parts[i]!.offsetMs };
  }
  return { index: 0, localMs: 0 };
}

/** The chapter playing at a position (-1 when there are none). */
export function chapterAt(chapters: Chapter[], ms: number): number {
  let found = -1;
  for (let i = 0; i < chapters.length; i++) {
    if (chapters[i]!.startMs <= ms) found = i;
    else break;
  }
  return found;
}

/** "Previous chapter" restarts the current one unless it began less than 3 s ago. */
export function previousChapterStart(chapters: Chapter[], ms: number): number {
  const i = chapterAt(chapters, ms);
  if (i < 0) return 0;
  if (ms - chapters[i]!.startMs > 3000 || i === 0) return chapters[i]!.startMs;
  return chapters[i - 1]!.startMs;
}

// The sleep timer lives in its own small module: the always-loaded music player needs it,
// the rest of this file (readers, EPUB cleaning) loads only with the book pages.
export { sleepDue, type SleepTimer } from "./sleep";

export const SPEEDS = [0.75, 1, 1.1, 1.25, 1.5, 1.75, 2] as const;

// --- comics ----------------------------------------------------------------------

/**
 * The pages shown together in double-page mode: the cover alone, then pairs.
 * Right-to-left books show the pair mirrored (the reader decides the visual order).
 */
export function spreads(count: number, double: boolean): number[][] {
  if (count <= 0) return [];
  if (!double) return Array.from({ length: count }, (_, i) => [i]);
  const out: number[][] = [[0]];
  for (let i = 1; i < count; i += 2) out.push(i + 1 < count ? [i, i + 1] : [i]);
  return out;
}

/** Index of the spread that contains a page. */
export function spreadOf(list: number[][], page: number): number {
  const i = list.findIndex((s) => s.includes(page));
  return i < 0 ? 0 : i;
}

/** Arrow keys and screen sides follow the reading direction. */
export function stepFor(key: "left" | "right", rtl: boolean): 1 | -1 {
  return (key === "right") !== rtl ? 1 : -1;
}

/** Fraction read after viewing a page (0-based) of count pages. */
export function pageProgression(page: number, count: number): number {
  if (count <= 0) return 0;
  return Math.min(1, Math.max(0, (page + 1) / count));
}

// --- EPUB safety ---------------------------------------------------------------------

/** CSP placed in every EPUB section: no scripts, no requests leaving the book. */
export const SECTION_CSP = "default-src blob: data:; img-src blob: data:; media-src blob: data:; font-src blob: data:; " +
  "style-src blob: data: 'unsafe-inline'; script-src 'none'; object-src 'none'; form-action 'none'";

/** Elements a book never needs and that can run code, load other documents or change the base URL. */
const DROP_ELEMENTS = "script, iframe, frame, frameset, object, embed, applet, base, portal, noscript, meta[http-equiv]";
/** Attributes holding addresses that must not be script or HTML documents. */
const URL_ATTRS = ["href", "src", "xlink:href", "action", "formaction", "data", "poster", "background", "srcset"];

/** "java\tscript:", "&#106;avascript:" and similar spellings all count. */
function dangerousUrl(value: string): boolean {
  const v = value.replace(/[\u0000- \u007f-\u009f]/g, "").toLowerCase();
  return /^(javascript|vbscript|livescript):/.test(v) || /^data:(text\/html|application\/(xhtml|xml)|image\/svg)/.test(v);
}

/**
 * Cleans a parsed section in place: removes script-capable elements, event handler
 * attributes and script/HTML addresses, then puts the CSP meta first in <head>.
 */
function sanitizeDocument(doc: Document) {
  for (const el of Array.from(doc.querySelectorAll(DROP_ELEMENTS))) el.remove();
  for (const el of Array.from(doc.querySelectorAll("*"))) {
    for (const attr of Array.from(el.attributes)) {
      const name = attr.name.toLowerCase();
      if (name.startsWith("on") || name === "srcdoc" || name === "formaction") el.removeAttribute(attr.name);
      else if (URL_ATTRS.includes(name) || name.endsWith(":href")) {
        if (attr.value.split(",").some((part) => dangerousUrl(part.trim()))) el.removeAttribute(attr.name);
      }
    }
  }
  // New elements take the document's own namespace (none for a bare <html> in XML).
  const ns = doc.documentElement?.namespaceURI ?? null;
  let head: Element | null = doc.querySelector("head");
  if (!head) {
    head = doc.createElementNS(ns, "head");
    doc.documentElement?.insertBefore(head, doc.documentElement.firstChild);
  }
  const meta = doc.createElementNS(ns, "meta");
  meta.setAttribute("http-equiv", "Content-Security-Policy");
  meta.setAttribute("content", SECTION_CSP);
  head.insertBefore(meta, head.firstChild);
}

/**
 * Makes an EPUB section inert before it is rendered: script-capable markup is removed
 * and a Content-Security-Policy that forbids scripts and outside requests is placed
 * first in its head. (EPUBs can carry JavaScript; a book must never run code in
 * Streamero's origin.) The markup is parsed and cleaned as a document; the text-based
 * cleaner below is only used where no DOM parser exists.
 */
export function sanitizeSection(markup: string, type = "application/xhtml+xml"): string {
  if (typeof DOMParser !== "undefined" && typeof XMLSerializer !== "undefined") {
    const xml = type === "application/xhtml+xml";
    let doc = new DOMParser().parseFromString(markup, xml ? "application/xhtml+xml" : "text/html");
    // Not well-formed XHTML (common in real books): read it the forgiving way.
    if (xml && doc.getElementsByTagName("parsererror").length) {
      doc = new DOMParser().parseFromString(markup, "text/html");
      sanitizeDocument(doc);
      return "<!DOCTYPE html>" + doc.documentElement.outerHTML;
    }
    sanitizeDocument(doc);
    return xml ? new XMLSerializer().serializeToString(doc) : "<!DOCTYPE html>" + doc.documentElement.outerHTML;
  }
  return sanitizeSectionText(markup);
}

/** Text-only fallback of sanitizeSection (no DOMParser). */
export function sanitizeSectionText(markup: string): string {
  let s = markup
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/<script\b[^>]*\/>/gi, "")
    .replace(/<script\b[\s\S]*?<\/script\s*>/gi, "")
    .replace(/<\/?(iframe|frame|object|embed|base|meta\s+http-equiv)\b[^>]*>/gi, "")
    .replace(/[\s/]on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi, " ")
    .replace(/(href|src|xlink:href)\s*=\s*(["'])\s*javascript:[^"']*\2/gi, '$1=$2#$2');
  const meta = `<meta http-equiv="Content-Security-Policy" content="${SECTION_CSP}"/>`;
  if (/<head\b[^>]*>/i.test(s)) s = s.replace(/<head\b[^>]*>/i, (m) => m + meta);
  else if (/<html\b[^>]*>/i.test(s)) s = s.replace(/<html\b[^>]*>/i, (m) => `${m}<head>${meta}</head>`);
  else s = meta + s;
  return s;
}

// --- EPUB reader settings ------------------------------------------------------------------

export type ReaderTheme = "dark" | "sepia" | "light";
export const THEMES: Record<ReaderTheme, { bg: string; fg: string; link: string }> = {
  dark: { bg: "#121212", fg: "#e6e1d8", link: "#d9b26a" },
  sepia: { bg: "#f4ecd8", fg: "#3b2f20", link: "#8a5a14" },
  light: { bg: "#ffffff", fg: "#1a1a1a", link: "#0b57d0" },
};

/** Stylesheet given to the EPUB renderer for a theme and font size (percent). */
export function readerCss(theme: ReaderTheme, fontSize: number, serif: boolean): string {
  const c = THEMES[theme];
  const size = Math.min(200, Math.max(70, Math.round(fontSize)));
  return `html { color-scheme: ${theme === "dark" ? "dark" : "light"}; }
html, body { background: ${c.bg} !important; color: ${c.fg} !important; }
body { font-size: ${size}% !important; line-height: 1.55; ${serif ? "font-family: Georgia, 'Iowan Old Style', serif !important;" : ""} }
a:any-link { color: ${c.link} !important; }
img, svg { max-width: 100%; height: auto; }
p { hyphens: auto; }`;
}
