import { lazy, Suspense, useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Maximize, Minimize } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { ErrorText, Spinner } from "@/components/ui";
import { ComicReader } from "./ComicReader";

export type ReadManifest = components["schemas"]["ReadManifest"];

const PdfReader = lazy(() => import("./PdfReader"));
const EpubReader = lazy(() => import("./EpubReader"));

export const COMIC_FORMATS = ["cbz", "cbr", "cb7", "cbt"];

/** Reader chrome: a bar that fades out while reading and comes back on movement. */
export function useIdle(ms = 2500): [boolean, () => void] {
  const [idle, setIdle] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const wake = useCallback(() => {
    setIdle(false);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setIdle(true), ms);
  }, [ms]);
  useEffect(() => {
    wake();
    window.addEventListener("pointermove", wake);
    window.addEventListener("keydown", wake);
    return () => {
      window.clearTimeout(timer.current);
      window.removeEventListener("pointermove", wake);
      window.removeEventListener("keydown", wake);
    };
  }, [wake]);
  return [idle, wake];
}

export function useFullscreen(): [boolean, () => void] {
  const [on, setOn] = useState(() => typeof document !== "undefined" && !!document.fullscreenElement);
  useEffect(() => {
    const f = () => setOn(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", f);
    return () => document.removeEventListener("fullscreenchange", f);
  }, []);
  const toggle = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void document.documentElement.requestFullscreen?.().catch(() => undefined);
  }, []);
  return [on, toggle];
}

/** The top bar shared by every reader. */
export function ReaderBar({ title, hidden, children }: { title: string; hidden: boolean; children?: ReactNode }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { itemId } = useParams({ from: "/app/read/$itemId" });
  const [full, toggleFull] = useFullscreen();
  const btn = "tv-focus grid size-10 shrink-0 place-items-center rounded-[6px] text-text/85 transition-colors hover:bg-white/10 hover:text-text";
  return (
    <header className={`fixed inset-x-0 top-0 z-40 flex items-center gap-2 bg-gradient-to-b from-black/85 to-transparent px-3 pt-[max(0.5rem,env(safe-area-inset-top))] pb-6 transition-opacity duration-300 ${hidden ? "pointer-events-none opacity-0" : "opacity-100"}`}>
      <button type="button" className={btn} aria-label={t("library.back")} data-testid="reader-back"
        onClick={() => { if (document.fullscreenElement) void document.exitFullscreen(); void navigate({ to: "/item/$itemId", params: { itemId } }); }}>
        <ArrowLeft className="size-5" aria-hidden />
      </button>
      <h1 className="min-w-0 flex-1 truncate text-[15px] font-semibold">{title}</h1>
      {children}
      <button type="button" className={btn} onClick={toggleFull} aria-label={t(full ? "reader.exitFullscreen" : "reader.fullscreen")}>
        {full ? <Minimize className="size-5" aria-hidden /> : <Maximize className="size-5" aria-hidden />}
      </button>
    </header>
  );
}

/** /read/:id — opens the right reader for the book's format. */
export function ReaderPage() {
  const { t } = useTranslation();
  const { itemId } = useParams({ from: "/app/read/$itemId" });
  const item = useQuery({
    queryKey: ["item", itemId],
    queryFn: async () => unwrap(await api.GET("/items/{itemId}", { params: { path: { itemId } } })),
  });
  const manifest = useQuery({
    queryKey: ["read", itemId],
    staleTime: 0,
    gcTime: 0,
    queryFn: async () => unwrap(await api.GET("/items/{itemId}/read", { params: { path: { itemId } } })),
  });
  const error = item.error ?? manifest.error;
  if (error) return <div className="p-8 pt-24"><ErrorText error={error} /></div>;
  if (!manifest.data || !item.data) return <div role="status" className="flex min-h-dvh items-center justify-center bg-black"><Spinner /></div>;
  const mf = manifest.data;
  const title = item.data.item.title;
  const fallback = <div role="status" className="flex min-h-dvh items-center justify-center bg-black"><Spinner /></div>;
  return (
    <div className="min-h-dvh bg-black text-text" data-testid="reader">
      {COMIC_FORMATS.includes(mf.format)
        ? <ComicReader itemId={itemId} title={title} manifest={mf} />
        : mf.format === "pdf"
          ? <Suspense fallback={fallback}><PdfReader itemId={itemId} title={title} manifest={mf} /></Suspense>
          : ["epub", "mobi", "azw3"].includes(mf.format)
            ? <Suspense fallback={fallback}><EpubReader itemId={itemId} title={title} manifest={mf} /></Suspense>
            : <p className="p-8 pt-24">{t("reader.unsupported")}</p>}
    </div>
  );
}
