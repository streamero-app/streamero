import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, ZoomIn, ZoomOut } from "lucide-react";
import * as pdfjs from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { ErrorText, Spinner } from "@/components/ui";
import { ReaderBar, useIdle, type ReadManifest } from "./ReaderPage";
import { pageProgression } from "./logic";
import { useReadingProgress } from "./useReadingProgress";

pdfjs.GlobalWorkerOptions.workerSrc = workerUrl;

const bar = "tv-focus grid size-10 shrink-0 place-items-center rounded-[6px] text-text/85 transition-colors hover:bg-white/10 hover:text-text disabled:opacity-30";

/**
 * PDF viewer on Mozilla's PDF.js: one page at a time fitted to the screen (or
 * zoomed), arrows and swipes to turn, the page remembered for every client.
 * Scripts inside PDFs are never run (PDF.js does not execute them by default).
 */
export default function PdfReader({ itemId, title, manifest }: { itemId: string; title: string; manifest: ReadManifest }) {
  const { t } = useTranslation();
  const [idle] = useIdle();
  const [doc, setDoc] = useState<pdfjs.PDFDocumentProxy | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [page, setPage] = useState(() => (manifest.progress.finished ? 0 : (manifest.progress.page ?? 0)));
  const [zoom, setZoom] = useState(1);
  const canvas = useRef<HTMLCanvasElement>(null);
  const box = useRef<HTMLDivElement>(null);
  const save = useReadingProgress(itemId);
  const count = doc?.numPages ?? manifest.pageCount ?? 0;

  useEffect(() => {
    const task = pdfjs.getDocument({ url: manifest.fileUrl, isEvalSupported: false, enableScripting: false } as Parameters<typeof pdfjs.getDocument>[0]);
    task.promise.then((d) => setDoc(d), (e: unknown) => setError(e));
    return () => { void task.destroy(); };
  }, [manifest.fileUrl]);

  // Render the current page to fit the screen at the device's pixel density.
  useEffect(() => {
    if (!doc) return;
    let cancelled = false;
    let render: { cancel: () => void } | null = null;
    void (async () => {
      const p = await doc.getPage(Math.min(page, doc.numPages - 1) + 1);
      if (cancelled || !canvas.current || !box.current) return;
      const base = p.getViewport({ scale: 1 });
      const fitScale = Math.min(box.current.clientWidth / base.width, box.current.clientHeight / base.height);
      const scale = fitScale * zoom;
      const dpr = window.devicePixelRatio || 1;
      const vp = p.getViewport({ scale: scale * dpr });
      const c = canvas.current;
      c.width = Math.floor(vp.width);
      c.height = Math.floor(vp.height);
      c.style.width = `${Math.floor(vp.width / dpr)}px`;
      c.style.height = `${Math.floor(vp.height / dpr)}px`;
      const task = p.render({ canvas: c, viewport: vp } as Parameters<typeof p.render>[0]);
      render = task;
      await task.promise.catch(() => undefined);
    })();
    return () => { cancelled = true; render?.cancel(); };
  }, [doc, page, zoom]);

  useEffect(() => {
    if (!count) return;
    save({ page, progression: pageProgression(page, count), finished: page >= count - 1 });
  }, [page, count, save]);

  const go = useCallback((d: number) => setPage((p) => Math.min(Math.max(0, p + d), Math.max(0, count - 1))), [count]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement) return;
      if (e.key === "ArrowRight" || e.key === "PageDown" || (e.key === " " && zoom === 1)) go(1);
      else if (e.key === "ArrowLeft" || e.key === "PageUp") go(-1);
      else if (e.key === "+" || e.key === "=") setZoom((z) => Math.min(4, z * 1.25));
      else if (e.key === "-") setZoom((z) => Math.max(1, z / 1.25));
      else return;
      e.preventDefault();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [go, zoom]);
  const touch = useRef<number | null>(null);

  return (
    <div className="relative min-h-dvh" data-testid="pdf-reader">
      <ReaderBar title={title} hidden={idle}>
        <button type="button" className={bar} aria-label={t("reader.zoomOut")} disabled={zoom <= 1} onClick={() => setZoom((z) => Math.max(1, z / 1.25))}><ZoomOut className="size-5" aria-hidden /></button>
        <button type="button" className={bar} aria-label={t("reader.zoomIn")} disabled={zoom >= 4} onClick={() => setZoom((z) => Math.min(4, z * 1.25))}><ZoomIn className="size-5" aria-hidden /></button>
      </ReaderBar>
      <div ref={box} className={`flex h-dvh w-full justify-center ${zoom > 1 ? "items-start overflow-auto" : "items-center overflow-hidden"}`}
        onTouchStart={(e) => { touch.current = e.touches[0]?.clientX ?? null; }}
        onTouchEnd={(e) => {
          const x0 = touch.current;
          const x1 = e.changedTouches[0]?.clientX;
          touch.current = null;
          if (zoom === 1 && x0 !== null && x1 !== undefined && Math.abs(x1 - x0) > 50) go(x1 < x0 ? 1 : -1);
        }}>
        {error ? <div className="p-8 pt-24"><ErrorText error={error} /></div> : !doc ? <Spinner /> : <canvas ref={canvas} className="bg-white shadow-2xl" />}
      </div>
      <footer className={`fixed inset-x-0 bottom-0 z-30 flex items-center gap-3 bg-gradient-to-t from-black/85 to-transparent px-4 pt-8 pb-[max(1rem,env(safe-area-inset-bottom))] transition-opacity duration-300 ${idle ? "pointer-events-none opacity-0" : ""}`}>
        <button type="button" className={bar} aria-label={t("reader.previous")} onClick={() => go(-1)} disabled={page <= 0}><ChevronLeft className="size-5" aria-hidden /></button>
        <input type="range" min={0} max={Math.max(0, count - 1)} value={page} aria-label={t("reader.position")}
          onChange={(e) => setPage(Number(e.target.value))} className="flex-1 accent-[var(--amb-a)]" />
        <button type="button" className={bar} aria-label={t("reader.next")} onClick={() => go(1)} disabled={page >= count - 1}><ChevronRight className="size-5" aria-hidden /></button>
        <span className="text-sm text-text/75 tabular" data-testid="pdf-position">{t("reader.pageOf", { page: page + 1, count })}</span>
      </footer>
    </div>
  );
}
