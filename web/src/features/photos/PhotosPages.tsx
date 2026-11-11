import { useCallback, useEffect, useRef, useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, Download, Heart, Images, Info, Pause, Play, X } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, ErrorText, Select, Spinner, buttonVariants, pressable } from "@/components/ui";
import { imageUrl, type ItemSummary, type Library } from "@/features/library/format";
import { INTERVALS, byMonth, exposureLine, neighbours, nextIndex } from "./slideshow";

type ItemDetail = components["schemas"]["ItemDetail"];
const gutter = "px-5 sm:px-10 lg:px-16";
const PAGE = 120;

function useChildren(libraryId: string, query: { parentId?: string; kind?: "photo" | "photoalbum"; sort?: "taken" | "title" }) {
  return useInfiniteQuery({
    queryKey: ["photos", libraryId, query.parentId ?? "", query.kind ?? "", query.sort ?? ""],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => unwrap(await api.GET("/libraries/{libraryId}/items", {
      params: { path: { libraryId }, query: { ...query, sort: query.sort ?? "taken", limit: PAGE, offset: pageParam } },
    })),
    getNextPageParam: (last, pages) => { const n = pages.reduce((a, p) => a + p.items.length, 0); return n < last.total ? n : undefined; },
  });
}

function monthLabel(key: string, lang: string): string {
  if (!key) return "";
  const [y, m] = key.split("-").map(Number);
  return new Date(Date.UTC(y!, (m ?? 1) - 1, 1)).toLocaleDateString(lang, { month: "long", year: "numeric", timeZone: "UTC" });
}

function Thumb({ item, onOpen }: { item: ItemSummary; onOpen: () => void }) {
  const { t } = useTranslation();
  const [broken, setBroken] = useState(false);
  return (
    <button type="button" onClick={onOpen} data-testid="photo-thumb" aria-label={item.title}
      className="tv-focus group relative aspect-square overflow-hidden rounded-[3px] bg-surface-2 outline-none">
      {item.hasPoster && !broken
        ? <img src={imageUrl(item.id, "poster")} alt="" loading="lazy" draggable={false} onError={() => setBroken(true)}
            className="absolute inset-0 size-full object-cover transition-transform duration-500 ease-out group-hover:scale-[1.04]" />
        : <span className="absolute inset-0 grid place-items-center text-text/30"><Images className="size-1/4" aria-hidden /></span>}
      {item.kind === "video" && (
        <span className="absolute right-1.5 bottom-1.5 grid size-7 place-items-center rounded-full bg-black/60 backdrop-blur" title={t("photos.video")}>
          <Play className="size-3.5 fill-current" aria-hidden />
        </span>
      )}
      {item.favorite && <Heart className="absolute top-1.5 right-1.5 size-4 fill-current text-white drop-shadow" aria-hidden />}
    </button>
  );
}

function AlbumCard({ item }: { item: ItemSummary }) {
  const { t } = useTranslation();
  const [broken, setBroken] = useState(false);
  return (
    <Link to="/item/$itemId" params={{ itemId: item.id }} data-testid="photo-album" className="tv-focus group flex flex-col gap-2.5 outline-none">
      <div className="poster-lift relative aspect-[4/3] overflow-hidden rounded-[4px] bg-surface-2">
        {item.hasPoster && !broken
          ? <img src={imageUrl(item.id, "poster")} alt="" loading="lazy" onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover" />
          : <span className="absolute inset-0 grid place-items-center text-text/30"><Images className="size-1/4" aria-hidden /></span>}
      </div>
      <div className="min-w-0">
        <p className="truncate text-[14px] font-semibold text-text/90 group-hover:text-text">{item.title}</p>
        <p className="text-[13px] text-text/50 tabular">{t("photos.count", { count: item.childCount ?? 0 })}</p>
      </div>
    </Link>
  );
}

/** Pictures grouped by month, with the lightbox. */
function PhotoGrid({ items, hasMore, loadMore, loading }: { items: ItemSummary[]; hasMore: boolean; loadMore: () => void; loading: boolean }) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(-1);
  const groups = byMonth(items);
  let offset = 0;
  return (
    <>
      <div className="flex flex-col gap-8" data-testid="photo-grid">
        {groups.map((g) => {
          const start = offset;
          offset += g.items.length;
          return (
            <section key={`${g.key}-${start}`} className="flex flex-col gap-3">
              {g.key && <h3 className="font-label text-text/60">{monthLabel(g.key, i18n.language)}</h3>}
              <div className="grid grid-cols-3 gap-1 sm:grid-cols-4 md:grid-cols-6 xl:grid-cols-8">
                {g.items.map((it, i) => <Thumb key={it.id} item={it} onOpen={() => setOpen(start + i)} />)}
              </div>
            </section>
          );
        })}
        {hasMore && (
          <Button variant="secondary" className="self-center" disabled={loading} onClick={loadMore}>
            {loading && <Spinner className="size-4" />}{t("library.more")}
          </Button>
        )}
      </div>
      {open >= 0 && <Lightbox items={items} index={open} onIndex={setOpen} onClose={() => setOpen(-1)} />}
    </>
  );
}

/** A photo library: its albums, or every picture by date. */
export function PhotosLibrary({ library }: { library: Library }) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"albums" | "all">("albums");
  const top = useChildren(library.id, { sort: "taken" });
  const all = useChildren(library.id, { kind: "photo", sort: "taken" });
  const q = tab === "albums" ? top : all;
  const items = q.data?.pages.flatMap((p) => p.items) ?? [];
  const albums = tab === "albums" ? items.filter((i) => i.kind === "photoalbum") : [];
  const pictures = tab === "albums" ? items.filter((i) => i.kind !== "photoalbum") : items;
  const tabClass = "relative h-11 shrink-0 px-1 text-[15px] font-medium text-text/60 transition-colors hover:text-text after:absolute after:inset-x-0 after:bottom-0 after:h-[2px] after:scale-x-0 after:bg-[var(--amb-a)] after:transition-transform aria-selected:text-text aria-selected:after:scale-x-100";
  return (
    <div className="flex flex-col gap-6 pt-24" data-testid="photos-library">
      <header className={`flex flex-col gap-4 border-b border-border ${gutter}`}>
        <h1 className="font-display text-4xl sm:text-6xl">{library.name}</h1>
        <div role="tablist" className="-mb-px flex gap-6">
          {(["albums", "all"] as const).map((k) => (
            <button key={k} type="button" role="tab" aria-selected={tab === k} className={tabClass} onClick={() => setTab(k)}>{t(`photos.tab_${k}`)}</button>
          ))}
        </div>
      </header>
      <div className={`flex flex-col gap-8 ${gutter}`}>
        <ErrorText error={q.error} />
        {q.isPending && <div className="flex justify-center py-20"><Spinner /></div>}
        {q.data && !items.length && <p className="py-16 text-center text-sm text-muted">{library.scan.state === "idle" ? t("photos.empty") : t("library.scanning")}</p>}
        {albums.length > 0 && (
          <div className="grid grid-cols-2 gap-x-5 gap-y-7 sm:grid-cols-3 lg:grid-cols-5 2xl:grid-cols-6">
            {albums.map((a) => <AlbumCard key={a.id} item={a} />)}
          </div>
        )}
        {pictures.length > 0 && <PhotoGrid items={pictures} hasMore={!!q.hasNextPage} loading={q.isFetchingNextPage} loadMore={() => void q.fetchNextPage()} />}
      </div>
    </div>
  );
}

/** An album: its sub-albums, then its pictures by date, paged. */
export function PhotoAlbumView({ d }: { d: ItemDetail }) {
  const { t } = useTranslation();
  const q = useChildren(d.libraryId, { parentId: d.item.id, sort: "taken" });
  const items = q.data?.pages.flatMap((p) => p.items) ?? [];
  const albums = items.filter((i) => i.kind === "photoalbum");
  const pictures = items.filter((i) => i.kind !== "photoalbum");
  const [show, setShow] = useState(false);
  return (
    <div className="flex flex-col gap-6 pt-24 pb-6" data-testid="photo-album-page">
      <header className={`flex flex-wrap items-end gap-x-5 gap-y-3 ${gutter}`}>
        <div className="flex min-w-0 flex-col gap-2">
          {d.parentId
            ? <Link to="/item/$itemId" params={{ itemId: d.parentId }} className="text-sm text-text/55 hover:text-text">← {t("library.back")}</Link>
            : <Link to="/library/$libraryId" params={{ libraryId: d.libraryId }} className="text-sm text-text/55 hover:text-text">← {t("library.back")}</Link>}
          <h1 className="font-display text-4xl sm:text-6xl">{d.item.title}</h1>
          <p className="text-sm text-text/55 tabular">{t("photos.count", { count: d.item.childCount ?? items.length })}</p>
        </div>
        {pictures.length > 0 && (
          <button type="button" onClick={() => setShow(true)} data-testid="slideshow"
            className={`${pressable} ${buttonVariants.primary} tv-focus ml-auto h-12 rounded-[6px] px-6 text-base font-semibold`}>
            <Play className="size-5 fill-current" aria-hidden />{t("photos.slideshow")}
          </button>
        )}
      </header>
      <div className={`flex flex-col gap-8 ${gutter}`}>
        <ErrorText error={q.error} />
        {q.isPending && <div className="flex justify-center py-20"><Spinner /></div>}
        {albums.length > 0 && (
          <div className="grid grid-cols-2 gap-x-5 gap-y-7 sm:grid-cols-3 lg:grid-cols-5 2xl:grid-cols-6">
            {albums.map((a) => <AlbumCard key={a.id} item={a} />)}
          </div>
        )}
        {pictures.length > 0 && <PhotoGrid items={pictures} hasMore={!!q.hasNextPage} loading={q.isFetchingNextPage} loadMore={() => void q.fetchNextPage()} />}
      </div>
      {show && <Lightbox items={pictures} index={0} onIndex={() => undefined} onClose={() => setShow(false)} autoplay />}
    </div>
  );
}

/** A single picture or clip opened directly (search, favourites). */
export function PhotoItemView({ d }: { d: ItemDetail }) {
  const [open, setOpen] = useState(true);
  if (!open) {
    return (
      <div className={`flex flex-col gap-4 pt-24 ${gutter}`}>
        {d.parentId && <Link to="/item/$itemId" params={{ itemId: d.parentId }} className="text-sm text-text/60 hover:text-text">← {d.item.title}</Link>}
        <img src={imageUrl(d.item.id, "poster")} alt={d.item.title} className="max-h-[70vh] self-start rounded-[4px]" />
      </div>
    );
  }
  return <Lightbox items={[d.item]} index={0} onIndex={() => undefined} onClose={() => setOpen(false)} />;
}

function usePhotoUrl(item?: ItemSummary) {
  return useQuery({
    queryKey: ["photo-view", item?.id],
    enabled: !!item && item.kind === "photo",
    staleTime: 60 * 60 * 1000,
    queryFn: async () => unwrap(await api.GET("/items/{itemId}/photo", { params: { path: { itemId: item!.id } } })),
  });
}

/**
 * Full-screen viewer: arrows, swipes and keys move between pictures, a slideshow
 * with a chosen interval, EXIF details, favourite and download.
 */
export function Lightbox({ items, index: initial, onIndex, onClose, autoplay = false }: {
  items: ItemSummary[]; index: number; onIndex: (i: number) => void; onClose: () => void; autoplay?: boolean;
}) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const [index, setIndexState] = useState(initial);
  const [playing, setPlaying] = useState(autoplay);
  const [interval, setInterval_] = useState<number>(() => {
    try { const v = Number(localStorage.getItem("slideshow-interval")); return (INTERVALS as readonly number[]).includes(v) ? v : 5; } catch { return 5; }
  });
  const [info, setInfo] = useState(false);
  const [fav, setFav] = useState<Record<string, boolean>>({});
  const item = items[index];
  const view = usePhotoUrl(item);
  useEffect(() => {
    for (const n of neighbours(index, items.length)) {
      const it = items[n];
      if (it?.kind === "photo") {
        void qc.prefetchQuery({ queryKey: ["photo-view", it.id], staleTime: 60 * 60 * 1000,
          queryFn: async () => unwrap(await api.GET("/items/{itemId}/photo", { params: { path: { itemId: it.id } } })) });
      }
    }
  }, [index, items, qc]);
  const detail = useQuery({
    queryKey: ["item", item?.id],
    enabled: info && !!item,
    queryFn: async () => unwrap(await api.GET("/items/{itemId}", { params: { path: { itemId: item!.id } } })),
  });
  const setIndex = useCallback((i: number) => { setIndexState(i); onIndex(i); }, [onIndex]);
  const step = useCallback((dir: 1 | -1) => setIndex(nextIndex(index, items.length, dir, playing)), [index, items.length, playing, setIndex]);

  useEffect(() => {
    if (!playing || items.length < 2) return;
    const id = window.setTimeout(() => step(1), interval * 1000);
    return () => window.clearTimeout(id);
  }, [playing, interval, index, step, items.length]);
  useEffect(() => { try { localStorage.setItem("slideshow-interval", String(interval)); } catch { /* private mode */ } }, [interval]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLSelectElement) return;
      if (e.key === "Escape") onClose();
      else if (e.key === "ArrowRight") step(1);
      else if (e.key === "ArrowLeft") step(-1);
      else if (e.key === " ") setPlaying((p) => !p);
      else if (e.key === "i") setInfo((v) => !v);
      else return;
      e.preventDefault();
    };
    window.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => { window.removeEventListener("keydown", onKey); document.body.style.overflow = prev; };
  }, [onClose, step]);
  const touch = useRef<number | null>(null);
  if (!item) return null;
  const favorite = fav[item.id] ?? !!item.favorite;
  const toggleFav = async () => {
    setFav((f) => ({ ...f, [item.id]: !favorite }));
    await api.PUT("/items/{itemId}/user-data", { params: { path: { itemId: item.id } }, body: { favorite: !favorite } });
  };
  const btn = "tv-focus grid size-11 shrink-0 place-items-center rounded-full bg-black/40 text-white/90 backdrop-blur transition-colors hover:bg-black/60 hover:text-white aria-pressed:bg-white/20";
  const p = detail.data?.photo;
  return (
    <div role="dialog" aria-modal aria-label={item.title} className="fixed inset-0 z-[70] bg-black" data-testid="lightbox"
      onTouchStart={(e) => { touch.current = e.touches[0]?.clientX ?? null; }}
      onTouchEnd={(e) => {
        const x0 = touch.current;
        const x1 = e.changedTouches[0]?.clientX;
        touch.current = null;
        if (x0 !== null && x1 !== undefined && Math.abs(x1 - x0) > 50) step(x1 < x0 ? 1 : -1);
      }}>
      <div className="absolute inset-0 grid place-items-center">
        {item.kind === "video" ? (
          <div className="relative">
            <img src={imageUrl(item.id, "poster")} alt={item.title} className="max-h-dvh max-w-full object-contain opacity-80" />
            <Link to="/watch/$itemId" params={{ itemId: item.id }} className="absolute inset-0 grid place-items-center" aria-label={t("player.play")}>
              <span className="grid size-20 place-items-center rounded-full bg-black/55 backdrop-blur"><Play className="size-9 fill-current" aria-hidden /></span>
            </Link>
          </div>
        ) : view.data ? (
          <img key={view.data.url} src={view.data.url} alt={item.title} draggable={false} data-testid="lightbox-image"
            className="max-h-dvh max-w-full object-contain" />
        ) : view.error ? <ErrorText error={view.error} /> : <Spinner />}
      </div>
      <div className="absolute inset-x-0 top-0 flex items-center gap-2 bg-gradient-to-b from-black/70 to-transparent p-3 pt-[max(0.75rem,env(safe-area-inset-top))]">
        <button type="button" className={btn} aria-label={t("common.close")} onClick={onClose} data-testid="lightbox-close"><X className="size-5" aria-hidden /></button>
        <p className="min-w-0 flex-1 truncate text-sm text-white/80 tabular">{index + 1} / {items.length}</p>
        {items.length > 1 && (
          <>
            <Select value={String(interval)} onChange={(e) => setInterval_(Number(e.target.value))} aria-label={t("photos.interval")} data-testid="slideshow-interval">
              {INTERVALS.map((s) => <option key={s} value={s}>{t("photos.seconds", { count: s })}</option>)}
            </Select>
            <button type="button" className={btn} aria-pressed={playing} aria-label={playing ? t("photos.stopSlideshow") : t("photos.slideshow")}
              onClick={() => setPlaying(!playing)}>{playing ? <Pause className="size-5" aria-hidden /> : <Play className="size-5" aria-hidden />}</button>
          </>
        )}
        <button type="button" className={btn} aria-pressed={favorite} aria-label={t("photos.favorite")} onClick={() => void toggleFav()}>
          <Heart className={`size-5 ${favorite ? "fill-current" : ""}`} aria-hidden />
        </button>
        {view.data?.originalUrl && <a className={btn} href={view.data.originalUrl} download aria-label={t("books.download")}><Download className="size-5" aria-hidden /></a>}
        <button type="button" className={btn} aria-pressed={info} aria-label={t("photos.info")} onClick={() => setInfo(!info)}><Info className="size-5" aria-hidden /></button>
      </div>
      {items.length > 1 && (
        <>
          <button type="button" className={`${btn} absolute top-1/2 left-3 -translate-y-1/2`} aria-label={t("reader.previous")} onClick={() => step(-1)}><ChevronLeft className="size-6" aria-hidden /></button>
          <button type="button" className={`${btn} absolute top-1/2 right-3 -translate-y-1/2`} aria-label={t("reader.next")} onClick={() => step(1)}><ChevronRight className="size-6" aria-hidden /></button>
        </>
      )}
      {info && (
        <aside className="absolute right-3 bottom-3 w-[min(92vw,22rem)] rounded-[8px] border border-white/10 bg-black/75 p-4 text-sm text-white/85 backdrop-blur-xl" data-testid="photo-info">
          <p className="font-semibold text-white">{item.title}</p>
          {(p?.takenAt ?? item.takenAt) && <p>{new Date((p?.takenAt ?? item.takenAt)!).toLocaleString(i18n.language, { dateStyle: "long", timeStyle: "short", timeZone: "UTC" })}</p>}
          {p?.camera && <p>{p.camera}{p.lens ? ` · ${p.lens}` : ""}</p>}
          {p && exposureLine(p) && <p className="tabular">{exposureLine(p)}</p>}
          {(p?.width ?? item.width) && <p className="tabular">{p?.width ?? item.width} × {p?.height ?? item.height}</p>}
          {p?.latitude !== undefined && p.longitude !== undefined && (
            <a className="text-[var(--amb-a)] underline underline-offset-4" target="_blank" rel="noreferrer noopener"
              href={`https://www.openstreetmap.org/?mlat=${p.latitude}&mlon=${p.longitude}#map=14/${p.latitude}/${p.longitude}`}>{t("photos.onMap")}</a>
          )}
        </aside>
      )}
    </div>
  );
}
