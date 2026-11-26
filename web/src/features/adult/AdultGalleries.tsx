import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, Download, Eye, Images, Pause, Play, UserRound, X, ZoomIn, ZoomOut } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Button, ErrorText, Select, Spinner } from "@/components/ui";
import { BlurArt, Breadcrumbs, Monogram, SearchField, useBlur } from "./AdultCommon";
import {
  clampZoom, fetchGalleries, formatDate, galleryImage, PAGE, stepIndex, useAdultStatus,
  type AdultGallery, type AdultGalleryImage, type GalleryQuery,
} from "./api";

/** A gallery in a grid: its cover (blurred until clicked), title, studio, date and size. */
export function GalleryCard({ gallery }: { gallery: AdultGallery }) {
  const { t, i18n } = useTranslation();
  const blur = useBlur();
  return (
    <Link to="/adult/gallery/$galleryId" params={{ galleryId: gallery.id }} data-testid="adult-gallery-card"
      className="group tv-focus flex flex-col gap-2 rounded-[6px]">
      <div className="relative aspect-[4/5] overflow-hidden rounded-[6px] bg-surface ring-1 ring-border transition-transform duration-200 group-hover:-translate-y-0.5">
        <BlurArt blur={blur} src={gallery.coverImageId ? galleryImage(gallery.coverImageId, "thumb") : undefined}
          fallback={<Monogram name={gallery.studio?.name ?? gallery.title} />} />
        <span className="pointer-events-none absolute right-1.5 bottom-1.5 inline-flex items-center gap-1 rounded bg-black/70 px-1.5 py-0.5 text-[11px] font-semibold text-white tabular">
          <Images className="size-3" aria-hidden />{gallery.imageCount}
        </span>
      </div>
      <div className="min-w-0 px-0.5">
        <p className="line-clamp-2 text-sm font-semibold leading-snug text-text">{gallery.title}</p>
        <p className="mt-0.5 truncate text-xs text-muted">
          {[gallery.studio?.name, formatDate(gallery.date, i18n.language) || gallery.year].filter(Boolean).join(" · ")}
        </p>
        {gallery.performers.length > 0 && <p className="truncate text-xs text-text/60">{gallery.performers.map((p) => p.name).join(", ")}</p>}
        <span className="sr-only">{t("adult.imageCount", { count: gallery.imageCount })}</span>
      </div>
    </Link>
  );
}

const gridClass = "grid grid-cols-[repeat(auto-fill,minmax(min(100%,10.5rem),1fr))] gap-x-4 gap-y-6";

/** Galleries of a query with "load more", or a fixed list. */
export function GalleryGrid({ query, items, empty }: { query?: GalleryQuery; items?: AdultGallery[]; empty?: ReactNode }) {
  const { t } = useTranslation();
  const list = useInfiniteQuery({
    queryKey: ["adult", "galleries", query ?? {}],
    queryFn: ({ pageParam }) => fetchGalleries(query ?? {}, pageParam),
    initialPageParam: 0,
    enabled: !items,
    getNextPageParam: (last, all) => {
      const loaded = all.reduce((n, p) => n + p.items.length, 0);
      return loaded < last.total && last.items.length === PAGE ? loaded : undefined;
    },
  });
  if (items) {
    return <div className={gridClass}>{items.map((g) => <GalleryCard key={g.id} gallery={g} />)}</div>;
  }
  const all = list.data?.pages.flatMap((p) => p.items) ?? [];
  if (list.isPending) return <div className="py-16 text-center"><Spinner /></div>;
  if (list.error) return <ErrorText error={list.error} />;
  if (all.length === 0) return <p className="py-16 text-center text-muted">{empty ?? t("adult.noGalleries")}</p>;
  return (
    <>
      <p className="mb-3 text-xs text-muted tabular">{t("adult.galleryCount", { count: list.data?.pages[0]?.total ?? 0 })}</p>
      <div className={gridClass}>{all.map((g) => <GalleryCard key={g.id} gallery={g} />)}</div>
      {list.hasNextPage && (
        <div className="mt-8 flex justify-center">
          <Button variant="secondary" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>{t("adult.more")}</Button>
        </div>
      )}
    </>
  );
}

export type GalleriesSearch = { q?: string; sort?: "oldest" | "added" | "title" | "images"; tag?: string };

/** Photos: every gallery, with search and sort (kept in the address). */
export function AdultGalleriesPage() {
  const { t } = useTranslation();
  const search = useSearch({ strict: false }) as GalleriesSearch;
  const navigate = useNavigate();
  const set = (patch: Partial<GalleriesSearch>) => void navigate({ to: ".", search: ((s: GalleriesSearch) => ({ ...s, ...patch })) as never, replace: true });
  return (
    <section>
      <div className="mb-5 flex flex-wrap items-center gap-2">
        <SearchField value={search.q ?? ""} onChange={(q) => set({ q: q || undefined })} label={t("adult.searchGalleries")} />
        <Select aria-label={t("adult.sort")} value={search.sort ?? "date"} onChange={(e) => set({ sort: e.target.value === "date" ? undefined : e.target.value as GalleriesSearch["sort"] })}>
          <option value="date">{t("adult.sortDate")}</option>
          <option value="oldest">{t("adult.sortOldest")}</option>
          <option value="added">{t("adult.sortAdded")}</option>
          <option value="title">{t("adult.sortTitle")}</option>
          <option value="images">{t("adult.sortImages")}</option>
        </Select>
        {search.tag && (
          <button type="button" onClick={() => set({ tag: undefined })} className="inline-flex h-9 items-center gap-1.5 rounded-[6px] bg-white/[0.08] px-3 text-sm">
            #{search.tag}<X className="size-3.5" aria-label={t("adult.clearTag")} />
          </button>
        )}
      </div>
      <GalleryGrid query={{ q: search.q, sort: search.sort, tag: search.tag }} />
    </section>
  );
}

/** One gallery: its details and pictures; `?i=` opens the viewer at a picture (so Back closes it). */
export function AdultGalleryPage() {
  const { t, i18n } = useTranslation();
  const { galleryId } = useParams({ from: "/app/adult/gallery/$galleryId" });
  const search = useSearch({ strict: false }) as { i?: number };
  const navigate = useNavigate();
  const blur = useBlur();
  const [revealed, setRevealed] = useState(false);
  const gallery = useQuery({ queryKey: ["adult", "gallery", galleryId],
    queryFn: async () => unwrap(await api.GET("/adult/galleries/{galleryId}", { params: { path: { galleryId } } })) });
  if (gallery.isPending) return <div className="py-16 text-center"><Spinner /></div>;
  if (gallery.error) return <ErrorText error={gallery.error} />;
  const g = gallery.data;
  const open = (i: number | undefined) => void navigate({ to: ".", search: (s: Record<string, unknown>) => ({ ...s, i }), replace: i !== undefined && search.i !== undefined });
  const hidden = blur && !revealed;
  return (
    <article className="flex flex-col gap-6">
      <Breadcrumbs items={[{ label: t("adult.galleries"), to: "/adult/galleries" },
        ...(g.studio ? [{ label: g.studio.name, to: `/adult/studio/${g.studio.id}` }] : []), { label: g.title }]} />
      <header className="flex flex-col gap-3">
        {g.studio && (
          <Link to="/adult/studio/$studioId" params={{ studioId: g.studio.id }} search={{ tab: "galleries" }}
            className="text-xs font-semibold tracking-[0.14em] text-muted uppercase hover:text-text">{g.studio.name}</Link>
        )}
        <h2 className="font-display text-[clamp(1.8rem,3.4vw,2.6rem)] leading-tight">{g.title}</h2>
        <p className="flex flex-wrap gap-x-3 text-sm text-muted tabular">
          {g.date && <span>{formatDate(g.date, i18n.language)}</span>}
          <span>{t("adult.imageCount", { count: g.imageCount })}</span>
          {g.scene && (
            <Link to="/adult/scene/$sceneId" params={{ sceneId: g.scene.id }} className="underline underline-offset-4 hover:text-text" data-testid="adult-gallery-scene">
              {t("adult.fromScene", { title: g.scene.name })}
            </Link>
          )}
        </p>
        {g.performers.length > 0 && (
          <ul className="flex flex-wrap gap-2">
            {g.performers.map((p) => (
              <li key={p.id}>
                <Link to="/adult/performer/$performerId" params={{ performerId: p.id }} search={{ tab: "galleries" }}
                  className="inline-flex h-8 items-center gap-1.5 rounded-full bg-white/[0.07] pr-3 pl-2 text-sm ring-1 ring-inset ring-border hover:bg-white/[0.13]">
                  <UserRound className="size-4 text-muted" aria-hidden />{p.name}
                </Link>
              </li>
            ))}
          </ul>
        )}
        {g.tags.length > 0 && (
          <ul className="flex flex-wrap gap-1.5">
            {g.tags.map((tag) => (
              <li key={tag}><Link to="/adult/tag/$tag" params={{ tag }} className="inline-flex h-7 items-center rounded-[4px] bg-white/[0.05] px-2 text-xs text-text/75 hover:bg-white/[0.1]">#{tag}</Link></li>
            ))}
          </ul>
        )}
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => open(0)} data-testid="adult-gallery-open"><Play className="size-4 fill-current" aria-hidden />{t("adult.viewGallery")}</Button>
          {hidden && <Button variant="secondary" onClick={() => setRevealed(true)}><Eye className="size-4" aria-hidden />{t("adult.revealAll")}</Button>}
        </div>
      </header>
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,9rem),1fr))] gap-2" data-testid="adult-gallery-grid">
        {g.images.map((im, i) => (
          <li key={im.id}>
            <button type="button" onClick={() => open(i)} aria-label={t("adult.openImage", { n: i + 1 })} data-testid="adult-gallery-thumb"
              className="tv-focus relative block aspect-square w-full overflow-hidden rounded-[4px] bg-surface ring-1 ring-border">
              <BlurArt blur={hidden} src={galleryImage(im.id, "thumb")} fallback={<Monogram name={g.title} />} />
            </button>
          </li>
        ))}
      </ul>
      {search.i !== undefined && g.images.length > 0 && (
        <Lightbox images={g.images} index={Math.min(search.i, g.images.length - 1)} title={g.title}
          onIndex={(i) => open(i)} onClose={() => open(undefined)} />
      )}
    </article>
  );
}

const SLIDE_MS = 4000;

/**
 * Full-screen picture viewer: arrows, swipe, keyboard (← → Esc, + − 0 zoom, space
 * slideshow), zoom with wheel or double-click and drag to pan, download only with
 * the download permission.
 */
export function Lightbox({ images, index, title, onIndex, onClose }: {
  images: AdultGalleryImage[]; index: number; title: string; onIndex: (i: number) => void; onClose: () => void;
}) {
  const { t } = useTranslation();
  const { data: status } = useAdultStatus();
  const [zoom, setZoom] = useState(1);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const [playing, setPlaying] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const drag = useRef<{ x: number; y: number; px: number; py: number; moved: boolean } | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const img = images[index]!;
  const go = useCallback((delta: number) => onIndex(stepIndex(index, delta, images.length)), [index, images.length, onIndex]);

  useEffect(() => { setZoom(1); setPan({ x: 0, y: 0 }); setLoaded(false); }, [index]);
  useEffect(() => { root.current?.focus(); }, []);
  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => { document.body.style.overflow = prev; };
  }, []);
  useEffect(() => {
    if (!playing) return;
    const id = window.setTimeout(() => go(1), SLIDE_MS);
    return () => window.clearTimeout(id);
  }, [playing, index, go]);
  // Neighbours load ahead (no-store: kept only in memory).
  useEffect(() => {
    for (const d of [1, -1]) {
      const n = images[stepIndex(index, d, images.length)];
      if (n) new Image().src = galleryImage(n.id, "full");
    }
  }, [index, images]);

  const onKey = (e: React.KeyboardEvent) => {
    switch (e.key) {
      case "ArrowRight": go(1); break;
      case "ArrowLeft": go(-1); break;
      case "Escape": onClose(); break;
      case "+": case "=": setZoom((z) => clampZoom(z * 1.5)); break;
      case "-": setZoom((z) => { const n = clampZoom(z / 1.5); if (n === 1) setPan({ x: 0, y: 0 }); return n; }); break;
      case "0": setZoom(1); setPan({ x: 0, y: 0 }); break;
      case " ": setPlaying((p) => !p); break;
      default: return;
    }
    e.preventDefault();
  };
  const down = (e: ReactPointerEvent) => {
    // The arrow buttons keep their own clicks (capturing would take them away).
    if ((e.target as HTMLElement).closest("button")) return;
    drag.current = { x: e.clientX, y: e.clientY, px: pan.x, py: pan.y, moved: false };
    (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
  };
  const move = (e: ReactPointerEvent) => {
    const d = drag.current;
    if (!d) return;
    const dx = e.clientX - d.x, dy = e.clientY - d.y;
    if (Math.abs(dx) + Math.abs(dy) > 6) d.moved = true;
    if (zoom > 1) setPan({ x: d.px + dx, y: d.py + dy });
  };
  const up = (e: ReactPointerEvent) => {
    const d = drag.current;
    drag.current = null;
    if (!d || zoom > 1) return;
    const dx = e.clientX - d.x, dy = e.clientY - d.y;
    // A horizontal swipe changes the picture; a swipe down closes.
    if (Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy)) go(dx < 0 ? 1 : -1);
    else if (dy > 90 && Math.abs(dy) > Math.abs(dx)) onClose();
  };
  const ctl = "tv-focus grid size-10 place-items-center rounded-full bg-black/45 text-white/90 backdrop-blur-md transition-colors hover:bg-black/70";
  // Rendered at the end of the document so no transformed ancestor or header sits above it.
  return createPortal(
    <div ref={root} role="dialog" aria-modal="true" aria-label={title} tabIndex={-1} onKeyDown={onKey} data-testid="adult-lightbox"
      className="fixed inset-0 z-[200] flex flex-col bg-black outline-none">
      <div className="flex items-center gap-2 px-3 py-3 text-sm text-white/80 sm:px-5">
        <span className="tabular" data-testid="adult-lightbox-counter">{index + 1} / {images.length}</span>
        <span className="hidden truncate text-white/50 sm:inline">{img.name}</span>
        <div className="ml-auto flex items-center gap-2">
          <button type="button" className={ctl} onClick={() => setZoom((z) => { const n = clampZoom(z / 1.5); if (n === 1) setPan({ x: 0, y: 0 }); return n; })}
            aria-label={t("adult.zoomOut")} disabled={zoom <= 1}><ZoomOut className="size-[18px]" /></button>
          <button type="button" className={ctl} onClick={() => setZoom((z) => clampZoom(z * 1.5))} aria-label={t("adult.zoomIn")}><ZoomIn className="size-[18px]" /></button>
          <button type="button" className={ctl} onClick={() => setPlaying(!playing)} aria-pressed={playing} data-testid="adult-slideshow"
            aria-label={playing ? t("adult.slideshowStop") : t("adult.slideshow")}>
            {playing ? <Pause className="size-[18px]" /> : <Play className="size-[18px]" />}
          </button>
          {status?.canDownload && (
            <a className={ctl} href={galleryImage(img.id, "download")} download aria-label={t("adult.download")} data-testid="adult-download">
              <Download className="size-[18px]" />
            </a>
          )}
          <button type="button" className={ctl} onClick={onClose} aria-label={t("common.close")} data-testid="adult-lightbox-close"><X className="size-[18px]" /></button>
        </div>
      </div>
      <div className="relative min-h-0 flex-1 touch-none select-none overflow-hidden"
        onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={() => { drag.current = null; }}
        onWheel={(e) => setZoom((z) => { const n = clampZoom(z * (e.deltaY < 0 ? 1.15 : 1 / 1.15)); if (n === 1) setPan({ x: 0, y: 0 }); return n; })}
        onDoubleClick={() => { if (zoom > 1) { setZoom(1); setPan({ x: 0, y: 0 }); } else setZoom(2); }}>
        {!loaded && <div className="absolute inset-0 grid place-items-center"><Spinner /></div>}
        <img key={img.id} src={galleryImage(img.id, "full")} alt={t("adult.imageOf", { n: index + 1, count: images.length })} draggable={false}
          onLoad={() => setLoaded(true)} data-testid="adult-lightbox-image"
          className={`absolute inset-0 m-auto max-h-full max-w-full object-contain transition-opacity duration-200 ${loaded ? "opacity-100" : "opacity-0"} ${zoom > 1 ? "cursor-grab" : ""}`}
          style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})`, transition: drag.current ? "none" : undefined }} />
        <button type="button" onClick={(e) => { e.stopPropagation(); go(-1); }} aria-label={t("adult.previous")}
          className={`${ctl} absolute top-1/2 left-3 -translate-y-1/2 max-sm:hidden`}><ChevronLeft className="size-5" /></button>
        <button type="button" onClick={(e) => { e.stopPropagation(); go(1); }} aria-label={t("adult.next")} data-testid="adult-lightbox-next"
          className={`${ctl} absolute top-1/2 right-3 -translate-y-1/2 max-sm:hidden`}><ChevronRight className="size-5" /></button>
      </div>
    </div>,
    document.body,
  );
}
