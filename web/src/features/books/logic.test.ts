import { chapterAt, locate, pageProgression, previousChapterStart, readerCss, sanitizeSection, sleepDue, spreadOf, spreads, stepFor } from "./logic";
import { byMonth, exposureLine, neighbours, nextIndex } from "@/features/photos/slideshow";

const parts = [
  { fileId: "a", title: "One", offsetMs: 0, durationMs: 1000 },
  { fileId: "b", title: "Two", offsetMs: 1000, durationMs: 2000 },
];
const chapters = [
  { title: "Opening", startMs: 0, endMs: 500 },
  { title: "Middle", startMs: 500, endMs: 2000 },
  { title: "End", startMs: 2000, endMs: 3000 },
];

describe("audiobook timeline", () => {
  it("maps a book position to a part and back", () => {
    expect(locate(parts, 0)).toEqual({ index: 0, localMs: 0 });
    expect(locate(parts, 999)).toEqual({ index: 0, localMs: 999 });
    expect(locate(parts, 1000)).toEqual({ index: 1, localMs: 0 });
    expect(locate(parts, 2500)).toEqual({ index: 1, localMs: 1500 });
    expect(locate(parts, 99999)).toEqual({ index: 1, localMs: 1999 }); // clamped to the end
    expect(locate(parts, -5)).toEqual({ index: 0, localMs: 0 });
    expect(locate([], 10).index).toBe(-1);
  });

  it("finds the chapter and where 'previous' goes", () => {
    expect(chapterAt(chapters, 0)).toBe(0);
    expect(chapterAt(chapters, 1999)).toBe(1);
    expect(chapterAt(chapters, 2500)).toBe(2);
    expect(chapterAt([], 10)).toBe(-1);
    expect(previousChapterStart(chapters, 2001)).toBe(500); // just started: the chapter before
    expect(previousChapterStart(chapters, 2900)).toBe(500);
    expect(previousChapterStart(chapters, 6000)).toBe(2000); // well into it: restart it
  });

  it("pauses at the set time or at the end of the chapter", () => {
    expect(sleepDue({ kind: "off" }, 10, 10)).toBe(false);
    expect(sleepDue({ kind: "at", at: 100 }, 99, 0)).toBe(false);
    expect(sleepDue({ kind: "at", at: 100 }, 100, 0)).toBe(true);
    expect(sleepDue({ kind: "chapter", endMs: 2000 }, 0, 1500)).toBe(false);
    expect(sleepDue({ kind: "chapter", endMs: 2000 }, 0, 1800)).toBe(true);
  });
});

describe("comic reader", () => {
  it("pairs pages after a lone cover in double-page mode", () => {
    expect(spreads(5, false)).toEqual([[0], [1], [2], [3], [4]]);
    expect(spreads(5, true)).toEqual([[0], [1, 2], [3, 4]]);
    expect(spreads(4, true)).toEqual([[0], [1, 2], [3]]);
    expect(spreads(0, true)).toEqual([]);
    expect(spreadOf(spreads(5, true), 4)).toBe(2);
    expect(spreadOf(spreads(5, true), 9)).toBe(0);
  });

  it("turns pages the reading direction's way", () => {
    expect(stepFor("right", false)).toBe(1);
    expect(stepFor("left", false)).toBe(-1);
    expect(stepFor("right", true)).toBe(-1); // manga: the next page is on the left
    expect(stepFor("left", true)).toBe(1);
  });

  it("computes progression from the page", () => {
    expect(pageProgression(0, 4)).toBe(0.25);
    expect(pageProgression(3, 4)).toBe(1);
    expect(pageProgression(0, 0)).toBe(0);
  });
});

describe("EPUB reader", () => {
  it("strips scripts and adds a CSP to every section", () => {
    const out = sanitizeSection(`<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"><head><title>x</title>
      <script src="evil.js"/><script>alert(1)</script></head><body onload="steal()"><a href="javascript:alert(1)">x</a><p>Text</p></body></html>`);
    expect(out).not.toMatch(/<script/i);
    expect(out).not.toMatch(/onload/);
    expect(out).not.toMatch(/javascript:/);
    expect(out).toMatch(/<head><meta http-equiv="Content-Security-Policy" content="[^"]*script-src 'none'/);
    expect(out).toContain("<p>Text</p>");
    expect(sanitizeSection("<html><body>a</body></html>")).toMatch(/^<html><head><meta http-equiv/);
  });

  it("builds the theme stylesheet with a clamped font size", () => {
    expect(readerCss("sepia", 300, true)).toContain("font-size: 200%");
    expect(readerCss("dark", 50, false)).toContain("font-size: 70%");
    expect(readerCss("light", 100, false)).not.toContain("Georgia");
  });
});

describe("photo slideshow", () => {
  it("steps with or without looping", () => {
    expect(nextIndex(2, 3, 1, true)).toBe(0);
    expect(nextIndex(2, 3, 1, false)).toBe(2);
    expect(nextIndex(0, 3, -1, true)).toBe(2);
    expect(nextIndex(0, 0, 1, true)).toBe(-1);
    expect(neighbours(0, 3)).toEqual([1, 2]);
  });

  it("groups by month and describes the exposure", () => {
    const g = byMonth([{ takenAt: "2023-07-20T10:00:00Z" }, { takenAt: "2023-07-01T10:00:00Z" }, { takenAt: "2023-06-30T10:00:00Z" }, {}]);
    expect(g.map((x) => [x.key, x.items.length])).toEqual([["2023-07", 2], ["2023-06", 1], ["", 1]]);
    expect(exposureLine({ exposureTime: "1/250", fNumber: 2.8, focalLength: 50, iso: 400 })).toBe("1/250 s · f/2.8 · 50 mm · ISO 400");
    expect(exposureLine({})).toBe("");
  });
});
