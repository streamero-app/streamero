import { nextInDirection } from "./spatialNav";

const rect = (x: number, y: number, w = 100, h = 60) => ({ left: x, top: y, right: x + w, bottom: y + h, width: w, height: h, x, y }) as DOMRect;
const el = (id: string) => ({ id }) as unknown as HTMLElement;

describe("spatial navigation", () => {
  // A row of three cards with a second row below.
  const a = el("a"), b = el("b"), c = el("c"), d = el("d"), e = el("e");
  const items = [
    { el: a, r: rect(0, 0) }, { el: b, r: rect(120, 0) }, { el: c, r: rect(240, 0) },
    { el: d, r: rect(0, 100) }, { el: e, r: rect(130, 100) },
  ];

  it("moves along a row", () => {
    expect(nextInDirection(rect(0, 0), items.filter((i) => i.el !== a), "ArrowRight")).toBe(b);
    expect(nextInDirection(rect(240, 0), items.filter((i) => i.el !== c), "ArrowLeft")).toBe(b);
  });

  it("moves between rows to the best-aligned card", () => {
    expect(nextInDirection(rect(120, 0), items.filter((i) => i.el !== b), "ArrowDown")).toBe(e);
    expect(nextInDirection(rect(0, 100), items.filter((i) => i.el !== d), "ArrowUp")).toBe(a);
  });

  it("returns null at the edge", () => {
    expect(nextInDirection(rect(240, 0), items.filter((i) => i.el !== c), "ArrowRight")).toBeNull();
  });
});
