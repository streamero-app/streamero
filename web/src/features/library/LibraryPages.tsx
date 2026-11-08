import { useEffect, useRef, useState, type ReactNode } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Check, ChevronLeft, ChevronRight, Clapperboard, Heart, Info, Play, RotateCcw, Star } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Art, Badge, Button, ErrorText, IconButton, Select, Spinner, buttonVariants, pressable } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { Page } from "@/components/Page";
import { channelsLabel, formatDuration, formatSize, imageUrl, placeholderHue, resolutionLabel, type ItemSummary } from "./format";

type ItemDetail = components["schemas"]["ItemDetail"];
const PAGE = 60;

export function useLibraries() {
  return useQuery({ queryKey: ["libraries"], queryFn: async () => unwrap(await api.GET("/libraries")) });
}

function progressOf(it: ItemSummary): number {
  return it.positionMs && it.durationMs ? Math.min(100, (it.positionMs / it.durationMs) * 100) : 0;
}

/** Typographic poster for items without artwork: the title set like a film card. */
function PosterFallback({ item, wide = false }: { item: ItemSummary; wide?: boolean }) {
  const hue = placeholderHue(item.title);
  return (
    <div className="absolute inset-0 flex flex-col justify-end p-3.5"
      style={{ background: `radial-gradient(120% 90% at 20% 0%, hsl(${hue} 22% 22%) 0%, hsl(${(hue + 25) % 360} 14% 11%) 55%, #0c0a08 100%)` }}>
      <Clapperboard aria-hidden className="absolute top-3.5 left-3.5 size-4 text-white/25" />
      <span className={`font-display leading-[1.02] text-white/95 ${wide ? "line-clamp-2 text-2xl" : "line-clamp-4 text-[26px]"}`}>{item.title}</span>
      {item.year && <span className="mt-1.5 text-xs text-white/50 tabular">{item.year}</span>}
    </div>
  );
}

function ProgressBar({ value }: { value: number }) {
  if (!value) return null;
  return (
    <div className="absolute inset-x-2.5 bottom-2.5 h-1 overflow-hidden rounded-full bg-black/55 backdrop-blur" aria-hidden>
      <div className="h-full rounded-full bg-accent" style={{ width: `${value}%` }} />
    </div>
  );
}

/** Poster card. Lifts 2px on hover; the art zooms inside its frame. */
export function ItemCard({ item, className = "" }: { item: ItemSummary; className?: string }) {
  const { t } = useTranslation();
  const [broken, setBroken] = useState(false);
  const art = item.hasPoster && !broken;
  return (
    <Link to="/item/$itemId" params={{ itemId: item.id }} data-testid="item-card"
      className={`group flex flex-col gap-2.5 rounded-poster outline-offset-4 ${className}`}>
      <div className="relative aspect-[2/3] overflow-hidden rounded-poster bg-surface-2 ring-1 ring-border transition-[transform,box-shadow] duration-300 ease-out group-hover:-translate-y-0.5 group-hover:shadow-lift group-hover:ring-border-strong">
        {art ? (
          <Art src={imageUrl(item.id, item.kind === "episode" ? "still" : "poster")} onError={() => setBroken(true)}
            className="absolute inset-0 size-full object-cover group-hover:scale-[1.03]" />
        ) : <PosterFallback item={item} />}
        {item.played && (
          <span className="absolute top-2 right-2 grid size-6 place-items-center rounded-full bg-accent text-accent-contrast shadow" title={t("library.watched")}>
            <Check className="size-3.5" strokeWidth={3} aria-label={t("library.watched")} />
          </span>
        )}
        {item.favorite && !item.played && (
          <span className="absolute top-2 right-2 grid size-6 place-items-center rounded-full bg-black/55 text-accent backdrop-blur">
            <Heart className="size-3.5 fill-current" aria-label={t("library.favorite")} />
          </span>
        )}
        <ProgressBar value={progressOf(item)} />
      </div>
      <div className="min-w-0 px-0.5">
        <p className="truncate text-[14px] font-medium text-text/95 group-hover:text-text">{item.title}</p>
        <p className="flex items-center gap-1.5 text-xs text-muted tabular">
          {item.year}
          {item.rating ? <>{item.year && <span aria-hidden className="text-faint">·</span>}<Star className="size-3 fill-accent text-accent" aria-hidden />{item.rating.toFixed(1)}</> : null}
        </p>
      </div>
    </Link>
  );
}

/** Landscape card for "continue watching": backdrop or still, with time left. */
function WideCard({ item }: { item: ItemSummary }) {
  const { t } = useTranslation();
  const [broken, setBroken] = useState(false);
  const src = item.hasBackdrop ? imageUrl(item.id, "backdrop") : item.kind === "episode" && item.hasPoster ? imageUrl(item.id, "still") : null;
  const left = item.durationMs && item.positionMs ? formatDuration(item.durationMs - item.positionMs) : "";
  return (
    <Link to="/watch/$itemId" params={{ itemId: item.id }} data-testid="item-card"
      className="group flex w-[min(78vw,340px)] shrink-0 flex-col gap-2.5 rounded-poster outline-offset-4">
      <div className="relative aspect-video overflow-hidden rounded-poster bg-surface-2 ring-1 ring-border transition-[transform,box-shadow] duration-300 ease-out group-hover:-translate-y-0.5 group-hover:shadow-lift">
        {src && !broken ? <Art src={src} onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover group-hover:scale-[1.03]" /> : <PosterFallback item={item} wide />}
        <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-transparent to-transparent" />
        <span className="absolute bottom-5 left-3 grid size-10 place-items-center rounded-full bg-white/90 text-black opacity-0 shadow-lg transition-[opacity,transform] duration-200 ease-out group-hover:opacity-100 group-hover:scale-105">
          <Play className="ml-0.5 size-4 fill-current" aria-hidden />
        </span>
        <ProgressBar value={progressOf(item)} />
      </div>
      <div className="min-w-0 px-0.5">
        <p className="truncate text-[14px] font-medium">{item.title}</p>
        {left && <p className="text-xs text-muted tabular">{t("library.inProgress", { left })}</p>}
      </div>
    </Link>
  );
}

/** A titled, horizontally scrolling row (hidden when empty). */
function Shelf({ title, testId, children, count }: { title: string; testId: string; children: ReactNode; count: number }) {
  const { t } = useTranslation();
  const track = useRef<HTMLDivElement>(null);
  const [overflow, setOverflow] = useState(false);
  useEffect(() => {
    const el = track.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => setOverflow(el.scrollWidth > el.clientWidth + 4));
    ro.observe(el);
    return () => ro.disconnect();
  }, [count]);
  if (!count) return null;
  const scroll = (dir: 1 | -1) => track.current?.scrollBy({ left: dir * track.current.clientWidth * 0.85, behavior: "smooth" });
  return (
    <section className="flex flex-col gap-3.5" data-testid={testId}>
      <div className="flex items-end gap-3 px-4 sm:px-6 lg:px-10">
        <h2 className="text-xl font-semibold tracking-tight">{title}</h2>
        <div className={`ml-auto hidden gap-1.5 ${overflow ? "sm:flex" : ""}`}>
          <IconButton label={t("home.scrollLeft")} onClick={() => scroll(-1)} className="size-8"><ChevronLeft className="size-4" /></IconButton>
          <IconButton label={t("home.scrollRight")} onClick={() => scroll(1)} className="size-8"><ChevronRight className="size-4" /></IconButton>
        </div>
      </div>
      <div ref={track} className="shelf-track flex gap-4 overflow-x-auto scroll-px-4 px-4 pt-1 pb-3 sm:scroll-px-6 sm:px-6 lg:scroll-px-10 lg:px-10">
        {children}
      </div>
    </section>
  );
}

const shelfPoster = "w-[38vw] shrink-0 sm:w-[168px] xl:w-[184px]";

export function LatestShelf() {
  const { t } = useTranslation();
  const items = useQuery({ queryKey: ["latest"], queryFn: async () => unwrap(await api.GET("/home/latest")) });
  return (
    <Shelf title={t("home.latest")} testId="latest" count={items.data?.length ?? 0}>
      {items.data?.map((it) => <ItemCard key={it.id} item={it} className={shelfPoster} />)}
    </Shelf>
  );
}

export function FavoritesShelf() {
  const { t } = useTranslation();
  const items = useQuery({ queryKey: ["favorites"], queryFn: async () => unwrap(await api.GET("/home/favorites")) });
  return (
    <Shelf title={t("home.favorites")} testId="favorites" count={items.data?.length ?? 0}>
      {items.data?.map((it) => <ItemCard key={it.id} item={it} className={shelfPoster} />)}
    </Shelf>
  );
}

/** "Continue watching" row on the home page. */
export function ContinueShelf() {
  const { t } = useTranslation();
  const items = useQuery({ queryKey: ["continue"], queryFn: async () => unwrap(await api.GET("/home/continue")) });
  return (
    <Shelf title={t("home.continue")} testId="continue" count={items.data?.length ?? 0}>
      {items.data?.map((it) => <WideCard key={it.id} item={it} />)}
    </Shelf>
  );
}

/** Libraries on the home page: wide tiles with the library's own artwork. */
export function LibraryShelf() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const libraries = useLibraries();
  if (libraries.data?.length === 0) {
    return (
      <section className="mx-4 flex flex-col items-start gap-3 rounded-card border border-border bg-surface/60 p-6 sm:mx-6 lg:mx-10">
        <h2 className="text-xl font-semibold tracking-tight">{t("home.librariesTitle")}</h2>
        <p className="max-w-prose text-sm text-muted">{me?.isAdmin ? t("home.noLibrariesAdmin") : t("home.noLibraries")}</p>
        {me?.isAdmin && <Link to="/settings/libraries" className={`${pressable} ${buttonVariants.primary} h-10 rounded-[10px] px-4 text-sm`}>{t("libraries.add")}</Link>}
      </section>
    );
  }
  return (
    <section className="flex flex-col gap-3.5 px-4 sm:px-6 lg:px-10">
      <h2 className="text-xl font-semibold tracking-tight">{t("home.librariesTitle")}</h2>
      <ErrorText error={libraries.error} />
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {libraries.data?.map((l) => <LibraryTile key={l.id} id={l.id} name={l.name} kind={l.kind} count={l.itemCount} />)}
      </div>
    </section>
  );
}

function LibraryTile({ id, name, kind, count }: { id: string; name: string; kind: string; count: number }) {
  const { t } = useTranslation();
  // Three posters fanned behind the name give each library its own face.
  const peek = useQuery({
    queryKey: ["library-peek", id],
    queryFn: async () => unwrap(await api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId: id }, query: { limit: 6, sort: "added" } } })),
  });
  const posters = (peek.data?.items ?? []).filter((i) => i.hasPoster).slice(0, 3);
  const hue = placeholderHue(name);
  return (
    <Link to="/library/$libraryId" params={{ libraryId: id }} data-testid="library-card"
      className="group relative flex h-36 overflow-hidden rounded-card ring-1 ring-border transition-[transform,box-shadow] duration-300 ease-out hover:-translate-y-0.5 hover:shadow-lift hover:ring-border-strong"
      style={{ background: `radial-gradient(130% 120% at 0% 100%, hsl(${hue} 30% 16%) 0%, #14110e 60%)` }}>
      <div className="relative z-10 flex flex-col justify-end p-5">
        <p className="font-display text-[30px] leading-none">{name}</p>
        <p className="mt-2 text-sm text-muted">{t(`libraries.kind_${kind}`)} · {t("libraries.items", { count })}</p>
      </div>
      <div className="pointer-events-none absolute top-4 -right-3 flex h-[150%] gap-2" aria-hidden>
        {posters.map((p, i) => (
          <img key={p.id} src={imageUrl(p.id, "poster")} alt="" loading="lazy"
            className="h-[62%] rounded-md object-cover shadow-xl ring-1 ring-black/40 transition-transform duration-500 ease-out group-hover:-translate-y-1.5"
            style={{ transform: `rotate(${(i - 1) * 4}deg)`, transitionDelay: `${i * 40}ms` }} />
        ))}
      </div>
    </Link>
  );
}

/** Full-bleed spotlight of the newest title with a backdrop. */
export function HomeHero() {
  const { t } = useTranslation();
  const latest = useQuery({ queryKey: ["latest"], queryFn: async () => unwrap(await api.GET("/home/latest")) });
  const pick = latest.data?.find((i) => i.hasBackdrop) ?? latest.data?.find((i) => i.hasPoster) ?? latest.data?.[0];
  const detail = useQuery({
    queryKey: ["item", pick?.id],
    enabled: !!pick,
    queryFn: async () => unwrap(await api.GET("/items/{itemId}", { params: { path: { itemId: pick!.id } } })),
  });
  if (!pick) return null;
  const d = detail.data;
  const playable = pick.kind === "movie" || pick.kind === "episode";
  return (
    <section className={`relative isolate flex items-end overflow-hidden pt-24 pb-12 ${pick.hasBackdrop ? "min-h-[72vh] sm:min-h-[78vh]" : "min-h-[56vh]"}`} data-testid="home-hero">
      <Backdrop item={pick} />
      <div className="relative z-10 flex w-full items-end gap-10 px-4 sm:px-6 lg:px-10">
      <div className="flex max-w-3xl min-w-0 flex-1 flex-col gap-4">
        <p className="enter text-[13px] font-medium text-accent">{t("home.latest")}</p>
        <h2 className="enter font-display text-[44px] leading-[0.95] text-balance sm:text-[64px] lg:text-[80px]">{pick.title}</h2>
        <MetaLine item={pick} detail={d} className="enter-2" />
        {d?.overview && <p className="enter-2 line-clamp-3 max-w-[60ch] text-[15px] leading-relaxed text-text/80">{d.overview}</p>}
        <div className="enter-3 mt-1 flex flex-wrap gap-3">
          {playable && (
            <Link to="/watch/$itemId" params={{ itemId: pick.id }} className={`${pressable} ${buttonVariants.primary} h-12 rounded-xl px-6 text-[15px]`}>
              <Play className="size-4 fill-current" aria-hidden />{t("player.play")}
            </Link>
          )}
          <Link to="/item/$itemId" params={{ itemId: pick.id }}
            className={`${pressable} ${playable ? buttonVariants.secondary : buttonVariants.primary} h-12 rounded-xl px-5 text-[15px] backdrop-blur-md`}>
            <Info className="size-4" aria-hidden />{t("home.moreInfo")}
          </Link>
        </div>
      </div>
      {!pick.hasBackdrop && (
        <div className="enter relative mr-[4vw] hidden aspect-[2/3] w-52 shrink-0 overflow-hidden rounded-poster shadow-[0_30px_60px_-20px_rgb(0_0_0/0.9)] ring-1 ring-white/10 md:block lg:w-60">
          {pick.hasPoster ? <Art src={imageUrl(pick.id, "poster")} loading="eager" className="absolute inset-0 size-full object-cover" /> : <PosterFallback item={pick} />}
        </div>
      )}
      </div>
    </section>
  );
}

/** Backdrop art with the scrims that keep text readable; falls back to a warm glow. */
function Backdrop({ item, poster = false }: { item: ItemSummary & { hasBackdrop?: boolean }; poster?: boolean }) {
  const [broken, setBroken] = useState(false);
  const hue = placeholderHue(item.title);
  const src = item.hasBackdrop ? imageUrl(item.id, "backdrop") : poster && item.hasPoster ? imageUrl(item.id, "poster") : null;
  return (
    <div className="absolute inset-0 -z-10" aria-hidden>
      {src && !broken ? (
        <Art src={src} loading="eager" onError={() => setBroken(true)}
          className={`hero-settle absolute inset-0 size-full object-cover object-[center_25%] ${!item.hasBackdrop ? "scale-110 blur-3xl saturate-150" : ""}`} />
      ) : (
        <div className="absolute inset-0" style={{ background: `radial-gradient(90% 80% at 80% 0%, hsl(${hue} 20% 18%) 0%, transparent 65%), radial-gradient(70% 60% at 0% 100%, rgb(255 150 50 / 0.10), transparent 70%)` }} />
      )}
      <div className="absolute inset-0 bg-gradient-to-t from-bg via-bg/60 to-bg/5" />
      <div className="absolute inset-0 bg-gradient-to-r from-bg/90 via-bg/35 to-transparent" />
      <div className="absolute inset-x-0 top-0 h-32 bg-gradient-to-b from-bg/70 to-transparent" />
    </div>
  );
}

function MetaLine({ item, detail, className = "" }: { item: ItemSummary; detail?: ItemDetail; className?: string }) {
  const { t } = useTranslation();
  const file = detail?.files[0];
  const res = resolutionLabel(file?.width, file?.height);
  const parts = [item.year, t(`library.kind_${item.kind}`), formatDuration(item.durationMs)].filter(Boolean);
  return (
    <div className={`flex flex-wrap items-center gap-x-3 gap-y-2 text-sm text-text/75 tabular ${className}`}>
      {item.rating ? (
        <span className="inline-flex items-center gap-1 font-medium text-text"><Star className="size-4 fill-accent text-accent" aria-hidden />{item.rating.toFixed(1)}</span>
      ) : null}
      {parts.map((p, i) => <span key={i}>{p}</span>)}
      {detail?.officialRating && <Badge>{detail.officialRating}</Badge>}
      {res && <Badge>{res}</Badge>}
      {file?.hdr && <Badge tone="accent">{file.hdr}</Badge>}
    </div>
  );
}

export function LibraryPage() {
  const { t } = useTranslation();
  const { libraryId } = useParams({ from: "/app/library/$libraryId" });
  const libraries = useLibraries();
  const lib = libraries.data?.find((l) => l.id === libraryId);
  const [sort, setSort] = useState<"title" | "year" | "added" | "rating">("title");
  const [genre, setGenre] = useState("");
  const [unplayed, setUnplayed] = useState(false);
  const genres = useQuery({
    queryKey: ["genres", libraryId],
    queryFn: async () => unwrap(await api.GET("/libraries/{libraryId}/genres", { params: { path: { libraryId } } })),
  });
  const items = useInfiniteQuery({
    queryKey: ["library-items", libraryId, sort, genre, unplayed],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) =>
      unwrap(await api.GET("/libraries/{libraryId}/items", {
        params: { path: { libraryId }, query: { limit: PAGE, offset: pageParam, sort, genre: genre || undefined, unplayed: unplayed || undefined } },
      })),
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
  });
  const all = items.data?.pages.flatMap((p) => p.items) ?? [];
  const total = items.data?.pages[0]?.total;

  return (
    <Page className="flex flex-col gap-7">
      <div className="flex flex-wrap items-end gap-x-6 gap-y-4">
        <div className="enter min-w-0 flex-1">
          <h1 className="font-display text-5xl leading-none sm:text-6xl">{lib?.name ?? "…"}</h1>
          {total !== undefined && <p className="mt-2 text-sm text-muted tabular">{t("libraries.items", { count: total })}</p>}
        </div>
        <div className="flex flex-wrap items-center gap-2" aria-label={t("library.filters")} role="group">
          <label className="flex items-center gap-2 text-sm text-muted">
            <span className="sr-only sm:not-sr-only">{t("library.sort")}</span>
            <Select value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} aria-label={t("library.sort")}>
              <option value="title">{t("library.sortTitle")}</option>
              <option value="year">{t("library.sortYear")}</option>
              <option value="added">{t("library.sortAdded")}</option>
              <option value="rating">{t("library.sortRating")}</option>
            </Select>
          </label>
          {(genres.data?.length ?? 0) > 0 && (
            <Select value={genre} onChange={(e) => setGenre(e.target.value)} aria-label={t("library.genre")}>
              <option value="">{t("library.allGenres")}</option>
              {genres.data?.map((g) => <option key={g} value={g}>{g}</option>)}
            </Select>
          )}
          <label className={`${pressable} h-9 cursor-pointer rounded-full px-3.5 text-sm ring-1 ring-inset ${unplayed ? "bg-accent/15 text-accent ring-accent/40" : "text-muted ring-border hover:text-text"}`}>
            <input type="checkbox" className="sr-only" checked={unplayed} onChange={(e) => setUnplayed(e.target.checked)} />
            {unplayed && <Check className="size-3.5" aria-hidden />}
            {t("library.unplayedOnly")}
          </label>
        </div>
      </div>
      <ErrorText error={items.error} />
      {items.isPending && <div className="flex justify-center py-20"><Spinner /></div>}
      {total === 0 && (
        <p className="py-16 text-center text-sm text-muted">{lib && ["queued", "scanning"].includes(lib.scan.state) ? t("library.scanning") : t("library.empty")}</p>
      )}
      <div className="grid grid-cols-[repeat(auto-fill,minmax(132px,1fr))] gap-x-4 gap-y-7 sm:grid-cols-[repeat(auto-fill,minmax(160px,1fr))]">
        {all.map((it) => <ItemCard key={it.id} item={it} />)}
      </div>
      {items.hasNextPage && (
        <Button variant="secondary" className="self-center" disabled={items.isFetchingNextPage} onClick={() => void items.fetchNextPage()}>
          {items.isFetchingNextPage && <Spinner className="size-4" />}{t("library.more")}
        </Button>
      )}
    </Page>
  );
}

/** Favorite and watched toggles for the current profile. */
function UserDataButtons({ item }: { item: ItemSummary }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const update = useMutation({
    mutationFn: async (body: { favorite?: boolean; played?: boolean }) =>
      unwrap(await api.PUT("/items/{itemId}/user-data", { params: { path: { itemId: item.id } }, body })),
    onSuccess: () => {
      for (const key of [["item"], ["library-items"], ["favorites"], ["continue"], ["latest"]]) void qc.invalidateQueries({ queryKey: key });
    },
  });
  return (
    <>
      <IconButton label={item.favorite ? t("library.removeFavorite") : t("library.addFavorite")} active={!!item.favorite}
        aria-pressed={!!item.favorite} data-testid="favorite" disabled={update.isPending} className="size-12"
        onClick={() => update.mutate({ favorite: !item.favorite })}>
        <Heart className={`size-5 transition-transform duration-200 ${item.favorite ? "scale-110 fill-current" : ""}`} />
      </IconButton>
      <IconButton label={item.played ? t("library.markUnplayed") : t("library.markPlayed")} active={!!item.played}
        aria-pressed={!!item.played} data-testid="played" disabled={update.isPending} className="size-12"
        onClick={() => update.mutate({ played: !item.played })}>
        <Check className="size-5" strokeWidth={item.played ? 3 : 2} />
      </IconButton>
    </>
  );
}

function formatClock(ms: number): string {
  const s = Math.floor(ms / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = String(s % 60).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${sec}` : `${m}:${sec}`;
}

function episodeLabel(it: ItemSummary): string {
  if (it.episodeNumber === undefined) return "";
  const e = String(it.episodeNumber).padStart(2, "0");
  return it.episodeNumberEnd ? `E${e}–E${String(it.episodeNumberEnd).padStart(2, "0")}` : `E${e}`;
}

function initials(name: string): string {
  return name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join("");
}

export function ItemPage() {
  const { t } = useTranslation();
  const { itemId } = useParams({ from: "/app/item/$itemId" });
  const detail = useQuery({
    queryKey: ["item", itemId],
    queryFn: async () => unwrap(await api.GET("/items/{itemId}", { params: { path: { itemId } } })),
  });
  const d = detail.data;
  if (detail.error) return <Page><ErrorText error={detail.error} /></Page>;
  if (!d) return <div role="status" className="flex min-h-[60vh] items-center justify-center"><Spinner /></div>;
  const it = { ...d.item, hasBackdrop: d.hasBackdrop };
  const backLink = "inline-flex h-9 items-center gap-1.5 rounded-full bg-black/35 pr-3.5 pl-2.5 text-sm text-text/85 ring-1 ring-border backdrop-blur-md transition-colors hover:bg-black/55 hover:text-text";
  const back = d.parentId
    ? <Link to="/item/$itemId" params={{ itemId: d.parentId }} className={backLink}><ArrowLeft className="size-4" aria-hidden />{t("library.back")}</Link>
    : <Link to="/library/$libraryId" params={{ libraryId: d.libraryId }} className={backLink}><ArrowLeft className="size-4" aria-hidden />{t("library.back")}</Link>;

  const isEpisode = it.kind === "episode";
  const directors = d.crew.filter((p) => p.role === "Director").map((p) => p.name);
  const writers = d.crew.filter((p) => p.role !== "Director").map((p) => p.name);
  const seasons = d.children.filter((c) => c.kind === "season");
  const episodes = d.children.filter((c) => c.kind === "episode");
  const others = d.children.filter((c) => c.kind !== "season" && c.kind !== "episode");
  const title = isEpisode ? `${episodeLabel(it)} · ${it.title}` : it.title;

  return (
    <div className="flex flex-col gap-12 pb-6">
      <section className={`relative isolate flex flex-col justify-end overflow-hidden px-4 pt-32 pb-10 sm:px-6 lg:px-10 ${it.hasBackdrop ? "min-h-[78vh]" : "min-h-[56vh]"}`} data-testid="item-hero">
        <Backdrop item={it} poster />
        <div className="absolute top-20 left-4 z-10 sm:left-6 lg:left-10">{back}</div>
        <div className="flex flex-col gap-8 md:flex-row md:items-end">
          {it.hasPoster ? (
            <Art src={imageUrl(it.id, isEpisode ? "still" : "poster")} loading="eager"
              className={`enter shrink-0 rounded-poster object-cover shadow-[0_30px_60px_-20px_rgb(0_0_0/0.9)] ring-1 ring-white/10 ${isEpisode ? "aspect-video w-full max-w-md" : "hidden aspect-[2/3] w-56 md:block lg:w-64"}`} />
          ) : !isEpisode && (
            <div className="enter relative hidden aspect-[2/3] w-56 shrink-0 overflow-hidden rounded-poster shadow-[0_30px_60px_-20px_rgb(0_0_0/0.9)] ring-1 ring-white/10 md:block lg:w-64">
              <PosterFallback item={it} />
            </div>
          )}
          <div className="flex min-w-0 max-w-3xl flex-col gap-4">
            <h1 className="enter font-display text-[44px] leading-[0.95] text-balance sm:text-6xl lg:text-7xl">{title}</h1>
            {d.originalTitle && d.originalTitle !== it.title && <p className="enter -mt-1 text-sm text-muted italic">{d.originalTitle}</p>}
            <MetaLine item={it} detail={d} className="enter-2" />
            {d.genres.length > 0 && <p className="enter-2 text-sm text-muted">{d.genres.join(" · ")}</p>}
            {d.tagline && <p className="enter-2 font-display text-2xl text-text/90 italic">{d.tagline}</p>}
            {d.overview && <p className="enter-2 max-w-[68ch] text-[15px] leading-relaxed text-text/80" data-testid="overview">{d.overview}</p>}
            {d.metadataError && <p className="text-xs text-muted">{t("library.noMetadata")}</p>}
            <div className="enter-3 mt-2 flex flex-wrap items-center gap-3">
              {d.files.length > 0 && (
                <Link to="/watch/$itemId" params={{ itemId: it.id }} data-testid="play"
                  className={`${pressable} ${buttonVariants.primary} h-12 rounded-xl px-6 text-[15px]`}>
                  <Play className="size-4 fill-current" aria-hidden />
                  {it.positionMs ? t("player.resume", { time: formatClock(it.positionMs) }) : t("player.play")}
                </Link>
              )}
              {d.files.length > 0 && it.positionMs ? (
                <Link to="/watch/$itemId" params={{ itemId: it.id }} search={{ start: 0 }}
                  className={`${pressable} ${buttonVariants.secondary} h-12 rounded-xl px-5 text-[15px] backdrop-blur-md`}>
                  <RotateCcw className="size-4" aria-hidden />{t("player.fromStart")}
                </Link>
              ) : null}
              <UserDataButtons item={it} />
            </div>
            {it.positionMs && it.durationMs ? (
              <div className="h-1 max-w-sm overflow-hidden rounded-full bg-white/10" aria-hidden>
                <div className="h-full rounded-full bg-accent" style={{ width: `${progressOf(it)}%` }} />
              </div>
            ) : null}
          </div>
        </div>
      </section>

      {seasons.length > 0 && (
        <Shelf title={t("library.seasons")} testId="seasons" count={seasons.length}>
          {seasons.map((c) => (
            <ItemCard key={c.id} className={shelfPoster}
              item={{ ...c, title: c.seasonNumber === 0 ? t("library.specials") : t("library.season", { n: c.seasonNumber }) }} />
          ))}
        </Shelf>
      )}

      {episodes.length > 0 && (
        <section className="flex flex-col gap-3 px-4 sm:px-6 lg:px-10">
          <h2 className="text-xl font-semibold tracking-tight">{t("library.episodes")} <span className="ml-1 text-sm font-normal text-muted tabular">{episodes.length}</span></h2>
          <ul className="flex flex-col gap-1">
            {episodes.map((c) => <EpisodeRow key={c.id} item={c} />)}
          </ul>
        </section>
      )}

      {others.length > 0 && (
        <section className="flex flex-col gap-3 px-4 sm:px-6 lg:px-10">
          <ul className="flex flex-col gap-1">
            {others.map((c) => <EpisodeRow key={c.id} item={c} />)}
          </ul>
        </section>
      )}

      {(d.cast.length > 0 || directors.length > 0 || writers.length > 0) && (
        <section className="flex flex-col gap-4 px-4 sm:px-6 lg:px-10">
          <h2 className="text-xl font-semibold tracking-tight">{t("library.people")}</h2>
          {d.cast.length > 0 && (
            <ul className="shelf-track -mx-1 flex gap-3 overflow-x-auto px-1 pb-2" data-testid="cast" aria-label={t("library.cast")}>
              {d.cast.map((p) => (
                <li key={p.name + (p.role ?? "")} className="flex w-[124px] shrink-0 flex-col items-center gap-2 text-center">
                  <span aria-hidden className="grid size-[84px] place-items-center rounded-full bg-surface-2 font-display text-[28px] text-text/70 ring-1 ring-border"
                    style={{ background: `radial-gradient(circle at 30% 20%, hsl(${placeholderHue(p.name)} 25% 22%), #17130f 75%)` }}>
                    {initials(p.name)}
                  </span>
                  <span className="min-w-0">
                    <span className="line-clamp-2 text-[13px] leading-tight font-medium">{p.name}</span>
                    {p.role && <span className="mt-0.5 line-clamp-2 text-xs leading-tight text-muted">{p.role}</span>}
                  </span>
                </li>
              ))}
            </ul>
          )}
          {(directors.length > 0 || writers.length > 0) && (
            <dl className="grid max-w-3xl grid-cols-1 gap-x-10 gap-y-3 text-sm sm:grid-cols-2">
              {directors.length > 0 && <div><dt className="text-muted">{t("library.directedBy")}</dt><dd className="mt-0.5">{directors.join(", ")}</dd></div>}
              {writers.length > 0 && <div><dt className="text-muted">{t("library.writtenBy")}</dt><dd className="mt-0.5">{writers.join(", ")}</dd></div>}
            </dl>
          )}
        </section>
      )}

      {d.files.length > 0 && (
        <section className="flex flex-col gap-4 px-4 sm:px-6 lg:px-10">
          <h2 className="text-xl font-semibold tracking-tight">
            {t("library.mediaInfo")}
            <span className="ml-2 text-sm font-normal text-muted">{d.files.length > 1 ? t("library.versions", { count: d.files.length }) : t("library.file")}</span>
          </h2>
          <ul className="grid gap-3 lg:grid-cols-2">
            {d.files.map((f) => (
              <li key={f.id} className="flex min-w-0 flex-col gap-3 rounded-card border border-border bg-surface/60 p-4 text-sm" data-testid="media-file">
                <div className="flex flex-wrap items-center gap-2">
                  {f.label && <span className="mr-1 font-medium">{f.label}</span>}
                  {resolutionLabel(f.width, f.height) && <Badge tone="accent">{resolutionLabel(f.width, f.height)}</Badge>}
                  {f.hdr && <Badge tone="success">{f.hdr}</Badge>}
                  {f.videoCodec && <Badge>{f.videoCodec.toUpperCase()}</Badge>}
                  <Badge>{f.container.toUpperCase()}</Badge>
                  <span className="ml-auto text-xs text-muted tabular">{formatSize(f.size)} · {formatDuration(f.durationMs)}</span>
                </div>
                {f.audio && f.audio.length > 0 && (
                  <p className="text-xs text-muted"><span className="text-text/80">{t("library.audio")}:</span>{" "}
                    {f.audio.map((a) => [a.language?.toUpperCase(), a.codec.toUpperCase(), channelsLabel(a.channels), a.title].filter(Boolean).join(" ")).join(" · ")}
                  </p>
                )}
                {f.subtitles && f.subtitles.length > 0 && (
                  <p className="text-xs text-muted"><span className="text-text/80">{t("library.subtitles")}:</span>{" "}
                    {f.subtitles.map((s) => [s.language?.toUpperCase() ?? "?", s.forced ? t("library.forced") : "", s.external ? t("library.external") : ""].filter(Boolean).join(" ")).join(" · ")}
                  </p>
                )}
                <code className="truncate font-mono text-[11px] text-faint" title={f.path}>{f.path}</code>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

/** Episode list row: still, number, title, runtime and progress. */
function EpisodeRow({ item: c }: { item: ItemSummary }) {
  const [broken, setBroken] = useState(false);
  return (
    <li>
      <Link to="/item/$itemId" params={{ itemId: c.id }} data-testid="child-row"
        className="group flex items-center gap-4 rounded-card p-2 transition-colors duration-150 hover:bg-white/[0.04]">
        <div className="relative aspect-video w-32 shrink-0 overflow-hidden rounded-lg bg-surface-2 ring-1 ring-border sm:w-44">
          {c.hasPoster && !broken
            ? <Art src={imageUrl(c.id, "still")} onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover group-hover:scale-[1.03]" />
            : <div className="absolute inset-0 grid place-items-center text-faint"><Play className="size-5" aria-hidden /></div>}
          <ProgressBar value={progressOf(c)} />
        </div>
        <div className="min-w-0 flex-1">
          <p className="flex items-center gap-2 text-xs text-muted tabular">
            {episodeLabel(c) && <span className="font-medium text-accent">{episodeLabel(c)}</span>}
            {formatDuration(c.durationMs)}
          </p>
          <p className="mt-0.5 truncate text-[15px] font-medium group-hover:text-text">{c.title}</p>
        </div>
        {c.played && <Check className="mr-2 size-4 shrink-0 text-accent" aria-hidden />}
      </Link>
    </li>
  );
}
