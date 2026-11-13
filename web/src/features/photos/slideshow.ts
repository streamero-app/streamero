// Pure helpers for the photo grid, lightbox and slideshow (unit-tested).

export const INTERVALS = [3, 5, 10, 20] as const;

/** Next index in a slideshow; wraps around at the end when looping. */
export function nextIndex(i: number, count: number, dir: 1 | -1, loop: boolean): number {
  if (count <= 0) return -1;
  const n = i + dir;
  if (n >= count) return loop ? 0 : count - 1;
  if (n < 0) return loop ? count - 1 : 0;
  return n;
}

/** The neighbours worth preloading around the current picture. */
export function neighbours(i: number, count: number): number[] {
  return [i + 1, i - 1, i + 2].filter((x) => x >= 0 && x < count && x !== i);
}

/** Groups pictures by the month they were taken ("2023-07"), newest first as given. */
export function byMonth<T extends { takenAt?: string }>(items: T[]): { key: string; items: T[] }[] {
  const out: { key: string; items: T[] }[] = [];
  for (const it of items) {
    const key = it.takenAt ? it.takenAt.slice(0, 7) : "";
    const last = out[out.length - 1];
    if (last && last.key === key) last.items.push(it);
    else out.push({ key, items: [it] });
  }
  return out;
}

/** "1/250 s · f/2.8 · 50 mm · ISO 400" from EXIF details. */
export function exposureLine(p: { exposureTime?: string; fNumber?: number; focalLength?: number; iso?: number }): string {
  return [
    p.exposureTime ? `${p.exposureTime} s` : null,
    p.fNumber ? `f/${p.fNumber}` : null,
    p.focalLength ? `${p.focalLength} mm` : null,
    p.iso ? `ISO ${p.iso}` : null,
  ].filter(Boolean).join(" · ");
}
