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

export type SleepTimer = { kind: "off" } | { kind: "at"; at: number } | { kind: "chapter"; endMs: number };

/** Whether the sleep timer should pause playback now. */
export function sleepDue(timer: SleepTimer, now: number, positionMs: number): boolean {
  switch (timer.kind) {
    case "at": return now >= timer.at;
    case "chapter": return positionMs >= timer.endMs - 250;
    default: return false;
  }
}

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

/**
 * Makes an EPUB section inert before it is rendered: scripts are removed and a
 * Content-Security-Policy that forbids scripts and outside requests is added.
 * (EPUBs can carry JavaScript; a book must never run code in Streamero's origin.)
 */
export function sanitizeSection(markup: string): string {
  let s = markup
    .replace(/<script\b[^>]*\/>/gi, "")
    .replace(/<script\b[\s\S]*?<\/script\s*>/gi, "")
    .replace(/\son[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi, "")
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
