import type { components } from "@/api/schema.gen";
import i18n from "@/i18n";

export type Library = components["schemas"]["Library"];
export type ItemSummary = components["schemas"]["ItemSummary"];
export type MediaFile = components["schemas"]["MediaFile"];

/** "4K", "1080p", "720p", "SD" from the frame height (and width for scope formats). */
export function resolutionLabel(width?: number, height?: number): string {
  if (!width && !height) return "";
  const w = width ?? 0;
  const h = height ?? 0;
  if (w >= 3200 || h >= 1600) return "4K";
  if (w >= 1800 || h >= 1000) return "1080p";
  if (w >= 1200 || h >= 700) return "720p";
  return "SD";
}

export function formatDuration(ms?: number, clock = false): string {
  if (!ms) return "";
  if (clock) {
    const s = Math.round(ms / 1000);
    return s >= 3600 ? `${Math.floor(s / 3600)}:${String(Math.floor((s % 3600) / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}` : `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
  }
  const min = Math.round(ms / 60000);
  if (min < 60) return `${Math.max(1, min)} min`;
  return `${Math.floor(min / 60)} h ${min % 60} min`;
}

/** Numbers in the UI language: "7,4" and "1,5 GB" in Croatian, "7.4" and "1.5 GB" in English. */
function number(v: number, digits: number, lang = i18n.resolvedLanguage ?? "en"): string {
  return new Intl.NumberFormat(lang, { minimumFractionDigits: digits, maximumFractionDigits: digits, useGrouping: false }).format(v);
}

export function formatSize(bytes: number, lang?: string): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${number(v, v >= 10 || i === 0 ? 0 : 1, lang)} ${units[i]}`;
}

/** A 0–10 rating with one decimal, written the UI language's way. */
export function formatRating(rating: number, lang?: string): string {
  return number(rating, 1, lang);
}

export function channelsLabel(ch?: number): string {
  if (!ch) return "";
  if (ch === 1) return "mono";
  if (ch === 2) return "stereo";
  return `${ch - 1}.1`;
}

/** URL of cached artwork; the session cookie authorises it. */
export function imageUrl(itemId: string, type: "poster" | "backdrop" | "still" | "logo"): string {
  return `/api/v1/items/${itemId}/images/${type}`;
}

/** Stable colour for poster placeholders when an item has no artwork. */
export function placeholderHue(title: string): number {
  let h = 0;
  for (const c of title) h = (h * 31 + c.charCodeAt(0)) % 360;
  return h;
}
