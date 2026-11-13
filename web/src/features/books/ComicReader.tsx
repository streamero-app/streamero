import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { BookOpen, ChevronLeft, ChevronRight, Columns2, File, MoveHorizontal } from "lucide-react";
import { ReaderBar, useIdle, type ReadManifest } from "./ReaderPage";
import { pageProgression, spreadOf, spreads, stepFor } from "./logic";
import { useReadingProgress } from "./useReadingProgress";

const bar = "tv-focus grid size-10 shrink-0 place-items-center rounded-[6px] text-text/85 transition-colors hover:bg-white/10 hover:text-text aria-pressed:bg-white/15 aria-pressed:text-text";

function stored<T extends string>(key: string, fallback: T, allowed: readonly T[]): T {
  try {
    const v = localStorage.getItem(key) as T | null;
    return v && allowed.includes(v) ? v : fallback;
  } catch { return fallback; }
}

/**
 * Comic and manga reader: one page or two side by side, right-to-left for manga,
 * keyboard (arrows, space, Home/End), tap the left or right third to turn pages,
 * and the next pages preloaded.
 */
export function ComicReader({ itemId, title, manifest }: { itemId: string; title: string; manifest: ReadManifest }) {
  const { t } = useTranslation();
  const pages = manifest.pages;
  const count = pages.length;
  const [idle] = useIdle();
  const [rtl, setRtl] = useState(manifest.rightToLeft);
  const [layout, setLayout] = useState<"single" | "double">(() => stored("comic-layout", "single", ["single", "double"] as const));
  const [fit, setFit] = useState<"height" | "width">(() => stored("comic-fit", "height", ["height", "width"] as const));
  const list = useMemo(() => spreads(count, layout === "double"), [count, layout]);
  const start = Math.min(Math.max(0, manifest.progress.finished ? 0 : (manifest.progress.page ?? 0)), Math.max(0, count - 1));
  const [spread, setSpread] = useState(() => spreadOf(spreads(count, layout === "double"), start));
  const save = useReadingProgress(itemId);
  const scroller = useRef<HTMLDivElement>(null);

  useEffect(() => { try { localStorage.setItem("comic-layout", layout); localStorage.setItem("comic-fit", fit); } catch { /* private mode */ } }, [layout, fit]);
  const current = list[Math.min(spread, list.length - 1)] ?? [0];
  const lastPage = current[current.length - 1] ?? 0;

  useEffect(() => {
    if (!count) return;
    save({ page: lastPage, progression: pageProgression(lastPage, count), finished: lastPage >= count - 1 });
    if (scroller.current) scroller.current.scrollTop = 0;
  }, [lastPage, count, save]);

  const go = useCallback((d: number) => setSpread((s) => Math.min(Math.max(0, s + d), list.length - 1)), [list.length]);
  const changeLayout = (l: "single" | "double") => {
    const page = current[0] ?? 0;
    setLayout(l);
    setSpread(spreadOf(spreads(count, l === "double"), page));
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) return;
      switch (e.key) {
        case "ArrowLeft": go(stepFor("left", rtl)); break;
        case "ArrowRight": go(stepFor("right", rtl)); break;
        case " ": case "PageDown": go(e.shiftKey ? -1 : 1); break;
        case "PageUp": go(-1); break;
        case "Home": setSpread(0); break;
        case "End": setSpread(list.length - 1); break;
        default: return;
      }
      e.preventDefault();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [go, rtl, list.length]);

  // Swipe: follow the finger's direction in reading order.
  const touch = useRef<number | null>(null);
  const shown = rtl ? [...current].reverse() : current;
  const preload = [...(list[spread + 1] ?? []), ...(list[spread + 2] ?? [])];

  if (!count) return <><ReaderBar title={title} hidden={false} /><p className="p-8 pt-24 text-text/70">{t("reader.noPages")}</p></>;
  return (
    <div className="relative min-h-dvh select-none" dir="ltr">
      <ReaderBar title={title} hidden={idle}>
        <button type="button" className={bar} aria-pressed={layout === "double"} aria-label={t("reader.doublePage")} title={t("reader.doublePage")}
          onClick={() => changeLayout(layout === "double" ? "single" : "double")} data-testid="comic-double">
          {layout === "double" ? <Columns2 className="size-5" aria-hidden /> : <File className="size-5" aria-hidden />}
        </button>
        <button type="button" className={bar} aria-pressed={fit === "width"} aria-label={t("reader.fitWidth")} title={t("reader.fitWidth")}
          onClick={() => setFit(fit === "width" ? "height" : "width")}><MoveHorizontal className="size-5" aria-hidden /></button>
        <button type="button" className={`${bar} w-auto px-2.5 text-xs font-bold`} aria-pressed={rtl} onClick={() => setRtl(!rtl)}
          title={t("reader.rtl")} data-testid="comic-rtl">{rtl ? t("reader.rtlShort") : t("reader.ltrShort")}</button>
      </ReaderBar>
      <div ref={scroller} className={`flex min-h-dvh justify-center ${fit === "height" ? "h-dvh items-center overflow-hidden" : "items-start overflow-y-auto"}`}
        onTouchStart={(e) => { touch.current = e.touches[0]?.clientX ?? null; }}
        onTouchEnd={(e) => {
          const x0 = touch.current;
          const x1 = e.changedTouches[0]?.clientX;
          touch.current = null;
          if (x0 === null || x1 === undefined || Math.abs(x1 - x0) < 50) return;
          go(stepFor(x1 < x0 ? "right" : "left", rtl));
        }}>
        {shown.map((p) => (
          <img key={p} src={pages[p]} alt={t("reader.pageOf", { page: p + 1, count })} draggable={false} data-testid="comic-page"
            className={fit === "height" ? `max-h-dvh object-contain ${shown.length > 1 ? "max-w-[50vw]" : "max-w-full"}` : `h-auto ${shown.length > 1 ? "w-1/2 max-w-[60rem]" : "w-full max-w-[75rem]"}`} />
        ))}
      </div>
      {/* Tap zones: the outer thirds turn the page, the middle shows the controls. */}
      <button type="button" aria-label={t("reader.previous")} className="fixed inset-y-16 left-0 w-1/3 cursor-w-resize opacity-0"
        onClick={() => go(stepFor("left", rtl))} tabIndex={-1} />
      <button type="button" aria-label={t("reader.next")} className="fixed inset-y-16 right-0 w-1/3 cursor-e-resize opacity-0"
        onClick={() => go(stepFor("right", rtl))} tabIndex={-1} />
      <footer className={`fixed inset-x-0 bottom-0 z-30 flex items-center gap-3 bg-gradient-to-t from-black/85 to-transparent px-4 pt-8 pb-[max(1rem,env(safe-area-inset-bottom))] transition-opacity duration-300 ${idle ? "pointer-events-none opacity-0" : ""}`}>
        <button type="button" className={bar} aria-label={t("reader.previous")} onClick={() => go(stepFor("left", rtl))}><ChevronLeft className="size-5" aria-hidden /></button>
        <input type="range" min={0} max={Math.max(0, list.length - 1)} value={spread} aria-label={t("reader.position")}
          onChange={(e) => setSpread(Number(e.target.value))} className="flex-1 accent-[var(--amb-a)]" dir={rtl ? "rtl" : "ltr"} />
        <button type="button" className={bar} aria-label={t("reader.next")} onClick={() => go(stepFor("right", rtl))}><ChevronRight className="size-5" aria-hidden /></button>
        <span className="flex items-center gap-1.5 text-sm text-text/75 tabular" data-testid="comic-position">
          <BookOpen className="size-4" aria-hidden />{t("reader.pageOf", { page: current.map((p) => p + 1).join("–"), count })}
        </span>
      </footer>
      <div hidden>{preload.map((p) => <img key={p} src={pages[p]} alt="" />)}</div>
    </div>
  );
}
