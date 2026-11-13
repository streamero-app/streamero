import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, Minus, Plus, Type } from "lucide-react";
import { ErrorText, Spinner } from "@/components/ui";
import { ReaderBar, useIdle, type ReadManifest } from "./ReaderPage";
import { THEMES, readerCss, sanitizeSection, type ReaderTheme } from "./logic";
import { useReadingProgress } from "./useReadingProgress";

// foliate-js (MIT, vendored; see THIRD_PARTY.md) renders EPUB, MOBI and AZW3 with
// CSS-column pagination in sandboxed iframes. It is plain JavaScript without types,
// so only the parts used here are described.
type Relocate = { fraction: number; cfi?: string; location?: { current: number; total: number }; tocItem?: { label?: string } };
type DataEvent = CustomEvent<{ type: string; data: unknown; name: string }>;
type Book = { dir?: string; transformTarget?: EventTarget };
type FoliateView = HTMLElement & {
  open: (book: Book) => Promise<void>;
  init: (opts: { lastLocation?: string | null }) => Promise<void>;
  goToFraction: (f: number) => Promise<void>;
  goLeft: () => Promise<void>;
  goRight: () => Promise<void>;
  close?: () => void;
  renderer: HTMLElement & { setStyles?: (css: string) => void; next: () => Promise<void> };
  book: Book;
};

function pref<T>(key: string, fallback: T, parse: (v: string) => T | undefined): T {
  try {
    const v = localStorage.getItem(key);
    return v === null ? fallback : (parse(v) ?? fallback);
  } catch { return fallback; }
}

/** EPUB, MOBI and AZW3 reader: paginated, font size, theme, serif, keyboard and swipes. */
export default function EpubReader({ itemId, title, manifest }: { itemId: string; title: string; manifest: ReadManifest }) {
  const { t } = useTranslation();
  const [idle] = useIdle(3500);
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<FoliateView | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [ready, setReady] = useState(false);
  const [where, setWhere] = useState<Relocate | null>(null);
  const [size, setSize] = useState(() => pref("reader-font", 100, (v) => (Number(v) >= 70 && Number(v) <= 200 ? Number(v) : undefined)));
  const [theme, setTheme] = useState<ReaderTheme>(() => pref<ReaderTheme>("reader-theme", "dark", (v) => (v in THEMES ? (v as ReaderTheme) : undefined)));
  const [serif, setSerif] = useState(() => pref("reader-serif", true, (v) => v === "1"));
  const [panel, setPanel] = useState(false);
  const save = useReadingProgress(itemId);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const [{ makeBook }, res] = await Promise.all([
          import("@/vendor/foliate-js/view.js") as Promise<{ makeBook: (f: File) => Promise<Book> }>,
          fetch(manifest.fileUrl),
        ]);
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
        const file = new File([await res.blob()], `book.${manifest.format}`);
        const book = await makeBook(file);
        // Make every section inert before it is shown (EPUBs can carry scripts).
        book.transformTarget?.addEventListener("data", (e) => {
          const { detail } = e as DataEvent;
          if (detail.type === "application/xhtml+xml" || detail.type === "text/html") {
            detail.data = Promise.resolve(detail.data).then((d) => (typeof d === "string" ? sanitizeSection(d) : d));
          }
        });
        if (cancelled || !host.current) return;
        const el = document.createElement("foliate-view") as FoliateView;
        el.style.cssText = "display:block;width:100%;height:100%";
        host.current.replaceChildren(el);
        view.current = el;
        el.addEventListener("relocate", (e) => {
          const d = (e as CustomEvent<Relocate>).detail;
          setWhere(d);
          save({ locator: d.cfi, progression: d.fraction ?? 0 });
        });
        await el.open(book);
        el.renderer.setAttribute("flow", "paginated");
        el.renderer.setAttribute("max-inline-size", "720px");
        el.renderer.setAttribute("gap", "6%");
        el.renderer.setStyles?.(readerCss(theme, size, serif));
        const p = manifest.progress;
        if (p.locator && !p.finished) await el.init({ lastLocation: p.locator });
        else if (p.progression > 0 && !p.finished) await el.goToFraction(p.progression);
        else await el.renderer.next();
        if (!cancelled) setReady(true);
      } catch (e) {
        if (!cancelled) setError(e);
      }
    })();
    return () => {
      cancelled = true;
      view.current?.close?.();
      view.current?.remove();
      view.current = null;
    };
  }, [manifest.fileUrl]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    view.current?.renderer.setStyles?.(readerCss(theme, size, serif));
    try {
      localStorage.setItem("reader-font", String(size));
      localStorage.setItem("reader-theme", theme);
      localStorage.setItem("reader-serif", serif ? "1" : "0");
    } catch { /* private mode */ }
  }, [theme, size, serif]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement) return;
      if (e.key === "ArrowLeft") void view.current?.goLeft();
      else if (e.key === "ArrowRight" || e.key === " ") void view.current?.goRight();
      else return;
      e.preventDefault();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const colors = THEMES[theme];
  const bar = "tv-focus grid size-10 shrink-0 place-items-center rounded-[6px] text-text/85 transition-colors hover:bg-white/10 hover:text-text";
  const pct = Math.round((where?.fraction ?? manifest.progress.progression) * 100);
  return (
    <div className="relative h-dvh" style={{ background: colors.bg }} data-testid="epub-reader">
      <ReaderBar title={title} hidden={idle && !panel}>
        <button type="button" className={bar} aria-label={t("reader.appearance")} aria-expanded={panel} onClick={() => setPanel(!panel)} data-testid="reader-appearance">
          <Type className="size-5" aria-hidden />
        </button>
      </ReaderBar>
      {panel && (
        <div className="fixed top-16 right-3 z-50 flex w-72 flex-col gap-4 rounded-[8px] border border-border bg-surface/97 p-4 shadow-2xl backdrop-blur-xl" role="dialog" aria-label={t("reader.appearance")}>
          <div className="flex items-center justify-between gap-3">
            <span className="text-sm text-text/70">{t("reader.fontSize")}</span>
            <div className="flex items-center gap-1">
              <button type="button" className={bar} aria-label={t("reader.smaller")} onClick={() => setSize((s) => Math.max(70, s - 10))}><Minus className="size-4" aria-hidden /></button>
              <span className="w-12 text-center text-sm tabular" data-testid="font-size">{size}%</span>
              <button type="button" className={bar} aria-label={t("reader.larger")} onClick={() => setSize((s) => Math.min(200, s + 10))}><Plus className="size-4" aria-hidden /></button>
            </div>
          </div>
          <div className="flex gap-2" role="radiogroup" aria-label={t("reader.theme")}>
            {(Object.keys(THEMES) as ReaderTheme[]).map((k) => (
              <button key={k} type="button" role="radio" aria-checked={theme === k} onClick={() => setTheme(k)}
                className="tv-focus flex-1 rounded-[6px] border px-2 py-2 text-sm aria-checked:ring-2 aria-checked:ring-[var(--amb-a)]"
                style={{ background: THEMES[k].bg, color: THEMES[k].fg, borderColor: "rgb(255 255 255 / 0.15)" }}>{t(`reader.theme_${k}`)}</button>
            ))}
          </div>
          <label className="flex items-center gap-3 text-sm text-text/80">
            <input type="checkbox" checked={serif} onChange={(e) => setSerif(e.target.checked)} className="accent-[var(--amb-a)]" />
            {t("reader.serif")}
          </label>
        </div>
      )}
      <div ref={host} className="absolute inset-x-0 top-14 bottom-14" onClick={() => setPanel(false)} />
      {!ready && !error && <div className="absolute inset-0 grid place-items-center"><Spinner /></div>}
      {error !== null && <div className="absolute inset-0 p-8 pt-24"><ErrorText error={error} /></div>}
      <footer className="fixed inset-x-0 bottom-0 z-30 flex items-center gap-3 px-4 pb-[max(0.75rem,env(safe-area-inset-bottom))]" style={{ color: colors.fg }}>
        <button type="button" className={bar} style={{ color: colors.fg }} aria-label={t("reader.previous")} onClick={() => void view.current?.goLeft()}><ChevronLeft className="size-5" aria-hidden /></button>
        <input type="range" min={0} max={1000} value={Math.round((where?.fraction ?? 0) * 1000)} aria-label={t("reader.position")}
          onChange={(e) => void view.current?.goToFraction(Number(e.target.value) / 1000)} className="flex-1 accent-[var(--amb-a)]" />
        <span className="w-28 truncate text-right text-xs opacity-70 tabular" data-testid="epub-position">
          {where?.location ? t("reader.locationOf", { current: where.location.current + 1, total: where.location.total }) : `${pct}%`}
        </span>
        <button type="button" className={bar} style={{ color: colors.fg }} aria-label={t("reader.next")} onClick={() => void view.current?.goRight()}><ChevronRight className="size-5" aria-hidden /></button>
      </footer>
    </div>
  );
}
