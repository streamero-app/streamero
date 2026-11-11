/** Guide geometry and time helpers (pure, so the grid layout is unit-tested). */

const MIN = 60_000;
export const HALF_HOUR = 30 * MIN;

/** The guide opens at the current half hour. */
export function windowStart(now: Date): Date {
  return new Date(Math.floor(now.getTime() / HALF_HOUR) * HALF_HOUR);
}

/** Moves a window start by whole hours (negative = earlier). */
export function shiftWindow(start: Date, hours: number): Date {
  return new Date(start.getTime() + hours * 60 * MIN);
}

/**
 * Where a programme sits in a window: left offset and width in pixels at `pxPerMin`.
 * Programmes that started before the window are clipped at its left edge, ones that
 * run past its end at the right edge; programmes outside the window give null.
 */
export function blockGeometry(
  prog: { start: string; end: string },
  windowStartMs: number,
  windowEndMs: number,
  pxPerMin: number,
): { left: number; width: number; clippedStart: boolean; clippedEnd: boolean } | null {
  const s = new Date(prog.start).getTime();
  const e = new Date(prog.end).getTime();
  if (!(e > windowStartMs && s < windowEndMs)) return null;
  const from = Math.max(s, windowStartMs);
  const to = Math.min(e, windowEndMs);
  return {
    left: ((from - windowStartMs) / MIN) * pxPerMin,
    width: ((to - from) / MIN) * pxPerMin,
    clippedStart: s < windowStartMs,
    clippedEnd: e > windowEndMs,
  };
}

/** Half-hour ticks of a window, for the time ruler. */
export function ticks(windowStartMs: number, windowEndMs: number): number[] {
  const out: number[] = [];
  for (let t = Math.ceil(windowStartMs / HALF_HOUR) * HALF_HOUR; t < windowEndMs; t += HALF_HOUR) out.push(t);
  return out;
}

/** Share of a programme already shown (0..1). */
export function progress(prog: { start: string; end: string }, now: number): number {
  const s = new Date(prog.start).getTime();
  const e = new Date(prog.end).getTime();
  if (e <= s) return 0;
  return Math.min(1, Math.max(0, (now - s) / (e - s)));
}

export function isAiring(prog: { start: string; end: string }, now: number): boolean {
  return new Date(prog.start).getTime() <= now && now < new Date(prog.end).getTime();
}

/** "20:15" in the UI language. */
export function timeOf(iso: string | number, lang?: string): string {
  return new Intl.DateTimeFormat(lang === "hr" ? "hr-HR" : "en-GB", { hour: "2-digit", minute: "2-digit" }).format(new Date(iso));
}

/** "Tue 12 Nov" in the UI language. */
export function dayOf(iso: string | number, lang?: string): string {
  return new Intl.DateTimeFormat(lang === "hr" ? "hr-HR" : "en-GB", { weekday: "short", day: "numeric", month: "short" }).format(new Date(iso));
}

/** "S2 E5" when the guide knows the episode. */
export function episodeTag(p: { season?: number; episode?: number }): string {
  if (p.season === undefined && p.episode === undefined) return "";
  return [p.season !== undefined ? `S${p.season}` : "", p.episode !== undefined ? `E${p.episode}` : ""].filter(Boolean).join(" ");
}
