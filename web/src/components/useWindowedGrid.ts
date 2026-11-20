import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type RefObject } from "react";

/** Rows rendered above and below the screen, so fast scrolling and arrow keys never reach a gap. */
const OVERSCAN_ROWS = 3;
/** Items rendered before the grid could be measured (first paint, tests without layout). */
const UNMEASURED = 120;

export type GridWindow = { start: number; end: number; padTop: number; padBottom: number };

/**
 * Which items of a page-scrolled grid to render: the rows near the viewport, with the
 * rest standing in as padding. Pure, so it can be unit-tested without layout.
 */
export function gridWindow(
  count: number, columns: number, rowHeight: number, gridTop: number, scrollY: number, viewport: number, overscan = OVERSCAN_ROWS,
): GridWindow {
  if (count === 0) return { start: 0, end: 0, padTop: 0, padBottom: 0 };
  if (rowHeight <= 0 || columns <= 0) return { start: 0, end: Math.min(count, UNMEASURED), padTop: 0, padBottom: 0 };
  const rows = Math.ceil(count / columns);
  const first = Math.max(0, Math.floor((scrollY - gridTop) / rowHeight) - overscan);
  const last = Math.min(rows, Math.ceil((scrollY + viewport - gridTop) / rowHeight) + overscan);
  const startRow = Math.min(first, Math.max(0, last - 1));
  const endRow = Math.max(startRow + 1, last);
  return {
    start: startRow * columns,
    end: Math.min(count, endRow * columns),
    padTop: startRow * rowHeight,
    padBottom: Math.max(0, (rows - endRow) * rowHeight),
  };
}

function columnsOf(el: HTMLElement): number {
  const tpl = getComputedStyle(el).gridTemplateColumns;
  return tpl && tpl !== "none" ? tpl.trim().split(/\s+/).length : 1;
}

/**
 * Windowing for a CSS grid that scrolls with the page (library posters and lists that
 * reach thousands of titles). The grid keeps its own responsive columns; the hook
 * reads them, measures one row, and renders only the rows near the screen.
 */
export function useWindowedGrid<T extends HTMLElement>(count: number): { ref: RefObject<T | null>; win: GridWindow; style: CSSProperties } {
  const ref = useRef<T | null>(null);
  const [m, setM] = useState({ columns: 0, rowHeight: 0, top: 0, scrollY: 0, viewport: 0 });

  const measure = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    const first = el.firstElementChild as HTMLElement | null;
    const gap = parseFloat(getComputedStyle(el).rowGap) || 0;
    const rowHeight = first ? first.getBoundingClientRect().height + gap : 0;
    const columns = columnsOf(el);
    const top = el.getBoundingClientRect().top + window.scrollY;
    setM((p) => (p.columns === columns && p.rowHeight === rowHeight && p.top === top && p.scrollY === window.scrollY && p.viewport === window.innerHeight
      ? p : { columns, rowHeight, top, scrollY: window.scrollY, viewport: window.innerHeight }));
  }, []);

  useLayoutEffect(() => { measure(); }, [measure, count]);
  useEffect(() => {
    let frame = 0;
    const onScroll = () => {
      if (frame) return;
      frame = requestAnimationFrame(() => { frame = 0; measure(); });
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onScroll);
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(onScroll) : null;
    if (ref.current) ro?.observe(ref.current);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("resize", onScroll);
      ro?.disconnect();
    };
  }, [measure]);

  // A row's height includes the gap after it, so padding of n rows matches n real rows.
  const win = gridWindow(count, m.columns, m.rowHeight, m.top, m.scrollY, m.viewport);
  return { ref, win, style: { paddingTop: win.padTop, paddingBottom: win.padBottom } };
}

/**
 * Loads the next page when the element (the "Show more" button) comes near the screen.
 * The button stays, so keyboard and screen-reader users can still ask for more.
 */
export function useLoadMoreWhenVisible<T extends HTMLElement>(enabled: boolean, loaded: number, load: () => void): RefObject<T | null> {
  const ref = useRef<T | null>(null);
  const loadRef = useRef(load);
  loadRef.current = load;
  // Observed again after every page: a button still in range after loading fires again.
  useEffect(() => {
    const el = ref.current;
    if (!enabled || !el || typeof IntersectionObserver === "undefined") return;
    const io = new IntersectionObserver(([e]) => { if (e?.isIntersecting) loadRef.current(); }, { rootMargin: "800px 0px" });
    io.observe(el);
    return () => io.disconnect();
  }, [enabled, loaded]);
  return ref;
}
