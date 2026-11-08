import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery, useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Check, Info, Play, Plus, RotateCcw, Star, Volume2, VolumeX } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Art, Badge, Button, ErrorText, Select, Spinner, buttonVariants, pressable } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { channelsLabel, formatDuration, formatSize, imageUrl, placeholderHue, resolutionLabel, type ItemSummary, type Library } from "./format";
import { AddToMenu, CollectionCard, useCollections } from "./CollectionsPages";
import { MetadataMenu } from "./MetadataEditor";
import { ArtFallback, ItemCard, ProgressBar, RankCard, Row, RowSkeleton, TitleArt, TitleCard, isPlayable, progressOf, rowCard } from "./cards";

export { ItemCard } from "./cards";

type ItemDetail = components["schemas"]["ItemDetail"];
const PAGE = 60;
const gutter = "px-4 sm:px-8 lg:px-14";

export function useLibraries() {
  return useQuery({
    queryKey: ["libraries"],
    queryFn: async () => {
      const data = unwrap(await api.GET("/libraries"));
      return Array.isArray(data) ? data : [];
    },
  });
}

function useItemDetail(id?: string) {
  return useQuery({
    queryKey: ["item", id],
    enabled: !!id,
    queryFn: async () => unwrap(await api.GET("/items/{itemId}", { params: { path: { itemId: id! } } })),
  });
}

/** Backdrop with the scrims every streaming billboard has; a tinted glow when there is no art. */
function BillboardArt({ item, hasBackdrop, still = false }: { item: ItemSummary; hasBackdrop: boolean; still?: boolean }) {
  const [broken, setBroken] = useState(false);
  const hue = placeholderHue(item.title);
  return (
    <div className="absolute inset-0 -z-10 overflow-hidden" aria-hidden>
      {hasBackdrop && !broken ? (
        <Art src={imageUrl(item.id, still ? "still" : "backdrop")} loading="eager" onError={() => setBroken(true)}
          className="hero-settle absolute inset-0 size-full object-cover object-[center_20%]" />
      ) : (
        <div className="absolute inset-0" style={{ background: `radial-gradient(80% 90% at 75% 20%, hsl(${hue} 22% 22%) 0%, transparent 70%), linear-gradient(180deg, #161616, #101010)` }} />
      )}
      <div className="absolute inset-0 bg-gradient-to-r from-black/85 via-black/35 to-transparent" />
      <div className="absolute inset-x-0 bottom-0 h-[38%] bg-gradient-to-t from-bg via-bg/70 to-transparent" />
    </div>
  );
}

const codecMime: Record<string, string> = { h264: "avc1.640028", hevc: "hvc1.1.6.L120.90", av1: "av01.0.08M.08", vp9: "vp09.00.10.08" };
const containerMime: Record<string, string> = { mp4: "video/mp4", m4v: "video/mp4", mov: "video/mp4", webm: "video/webm", mkv: "video/x-matroska" };

/** Can this browser decode the file's video as-is? Then the preview never costs a transcode. */
function previewable(f?: ItemDetail["files"][number]): boolean {
  if (!f || f.hdr || !f.videoCodec || typeof document === "undefined") return false;
  const mime = containerMime[f.container];
  const codec = codecMime[f.videoCodec];
  if (!mime || !codec) return false;
  return document.createElement("video").canPlayType(`${mime}; codecs="${codec}"`) !== "";
}

/**
 * Muted motion preview behind a billboard, like streaming services: starts after a short
 * rest, only when the file plays directly, pauses off-screen, and gives way to the still.
 */
function PreviewVideo({ detail }: { detail?: ItemDetail }) {
  const { t } = useTranslation();
  const ref = useRef<HTMLVideoElement>(null);
  const [src, setSrc] = useState<{ url: string; startMs: number } | null>(null);
  const [playing, setPlaying] = useState(false);
  const [muted, setMuted] = useState(true);
  const file = detail?.files[0];
  const itemId = detail?.item.id;
  useEffect(() => {
    setSrc(null);
    setPlaying(false);
    if (!itemId || !file || !isPlayable(detail!.item) || !previewable(file)) return;
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
    if ((navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData) return;
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      const startMs = Math.round((file.durationMs ?? 0) * 0.18);
      const res = await api.POST("/items/{itemId}/playback", {
        params: { path: { itemId } },
        body: { startMs, device: { containers: [file.container], videoCodecs: [file.videoCodec!], audioCodecs: (file.audio ?? []).map((a) => a.codec), hls: false } },
      });
      if (!cancelled && res.data?.method === "direct") setSrc({ url: res.data.url, startMs });
    }, 2200);
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [itemId]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const v = ref.current;
    if (!v || !src) return;
    const io = new IntersectionObserver(([e]) => { if (e && !e.isIntersecting) v.pause(); else if (e?.isIntersecting && !v.ended) void v.play().catch(() => undefined); }, { threshold: 0.4 });
    io.observe(v);
    return () => io.disconnect();
  }, [src]);
  if (!src) return null;
  return (
    <>
      <video ref={ref} src={src.url} muted={muted} playsInline autoPlay preload="auto" aria-hidden
        onLoadedMetadata={(e) => { e.currentTarget.currentTime = src.startMs / 1000; }}
        onPlaying={() => setPlaying(true)}
        onTimeUpdate={(e) => { if (e.currentTarget.currentTime - src.startMs / 1000 > 45) e.currentTarget.pause(); }}
        onPause={() => setPlaying(false)} onEnded={() => setPlaying(false)} onError={() => setSrc(null)}
        className={`absolute inset-0 -z-[5] size-full object-cover transition-opacity duration-[1200ms] ease-out ${playing ? "opacity-100" : "opacity-0"}`} />
      {playing && (
        <button type="button" onClick={() => setMuted(!muted)} aria-label={muted ? t("player.unmute") : t("player.mute")}
          className="tv-focus absolute right-4 bottom-[22vh] z-10 grid size-11 place-items-center rounded-full border border-white/60 bg-black/30 text-white backdrop-blur transition-colors hover:bg-white/15 sm:right-8 lg:right-14">
          {muted ? <VolumeX className="size-5" aria-hidden /> : <Volume2 className="size-5" aria-hidden />}
        </button>
      )}
    </>
  );
}

function Facts({ item, detail, className = "" }: { item: ItemSummary; detail?: ItemDetail; className?: string }) {
  const { t } = useTranslation();
  const file = detail?.files[0];
  const res = resolutionLabel(file?.width, file?.height);
  const tag = "rounded-sm border border-white/40 px-1.5 text-xs leading-5";
  return (
    <div className={`flex flex-wrap items-center gap-x-3 gap-y-2 text-[15px] text-white/85 tabular ${className}`}>
      {item.rating ? <span className="inline-flex items-center gap-1 font-semibold text-white"><Star className="size-4 fill-accent text-accent" aria-hidden />{item.rating.toFixed(1)}</span> : null}
      {item.year && <span>{item.year}</span>}
      {detail?.officialRating && <span className={tag}>{detail.officialRating}</span>}
      {item.durationMs ? <span>{formatDuration(item.durationMs)}</span> : <span>{t(`library.kind_${item.kind}`)}</span>}
      {res && <span className={`${tag} font-semibold`}>{res === "4K" ? "4K" : "HD"}</span>}
      {file?.hdr && <span className={`${tag} font-semibold`}>{file.hdr}</span>}
    </div>
  );
}

/** Billboard: the newest title with art, near full screen, with Play and More info. */
export function Billboard({ pool }: { pool?: ItemSummary[] }) {
  const { t } = useTranslation();
  const pick = pool?.find((i) => i.hasBackdrop) ?? pool?.find((i) => i.hasPoster) ?? pool?.[0];
  const detail = useItemDetail(pick?.id);
  if (!pool) return <div aria-hidden className="h-[78vh] min-h-[460px] animate-pulse bg-gradient-to-b from-white/[0.04] to-transparent sm:h-[86vh]" />;
  if (!pick) return <div className="h-24" />;
  const d = detail.data;
  return (
    <section className="relative isolate flex h-[78vh] min-h-[460px] items-end pb-[14vh] sm:h-[86vh] lg:pb-[18vh]" data-testid="home-hero">
      <BillboardArt item={pick} hasBackdrop={!!pick.hasBackdrop} />
      <PreviewVideo detail={d} />
      <div className={`flex w-full max-w-[44rem] flex-col gap-4 ${gutter}`}>
        <h2 className="enter">
          <TitleArt item={pick} className="max-h-[22vh] w-auto max-w-[min(32rem,80vw)]"
            textClass="block font-display text-[42px] leading-[0.92] text-balance drop-shadow-lg sm:text-6xl lg:text-7xl" />
        </h2>
        <Facts item={pick} detail={d} className="enter-2" />
        {d?.overview && <p className="enter-2 line-clamp-3 max-w-[54ch] text-[15px] leading-snug text-white/90 drop-shadow sm:text-lg">{d.overview}</p>}
        <div className="enter-3 mt-2 flex flex-wrap gap-3">
          {isPlayable(pick) && (
            <Link to="/watch/$itemId" params={{ itemId: pick.id }} className={`${pressable} ${buttonVariants.play} tv-focus h-11 rounded-md px-6 text-base font-bold sm:h-12 sm:px-8 sm:text-lg`}>
              <Play className="size-5 fill-current sm:size-6" aria-hidden />{t("player.play")}
            </Link>
          )}
          <Link to="/item/$itemId" params={{ itemId: pick.id }} className={`${pressable} ${buttonVariants.muted} tv-focus h-11 rounded-md px-5 text-base font-bold sm:h-12 sm:px-7 sm:text-lg`}>
            <Info className="size-5 sm:size-6" aria-hidden />{t("home.moreInfo")}
          </Link>
        </div>
      </div>
    </section>
  );
}

/** Top-rated and genre rows for one library — the streaming-service backbone. */
function LibraryRows({ library }: { library: Library }) {
  const { t } = useTranslation();
  const genres = useQuery({
    queryKey: ["genres", library.id],
    queryFn: async () => unwrap(await api.GET("/libraries/{libraryId}/genres", { params: { path: { libraryId: library.id } } })),
  });
  const top = useQuery({
    queryKey: ["row", `top-${library.id}`],
    queryFn: async () =>
      unwrap(await api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId: library.id }, query: { limit: 20, sort: "rating" } } }))
        .items.filter((i) => i.rating),
  });
  const picked = (genres.data ?? []).slice(0, 4);
  const rows = useQueries({
    queries: picked.map((g) => ({
      queryKey: ["row", `genre-${library.id}-${g}`],
      queryFn: async () => unwrap(await api.GET("/libraries/{libraryId}/items", {
        params: { path: { libraryId: library.id }, query: { limit: 20, sort: "added" as const, genre: g } },
      })).items,
    })),
  });
  return (
    <>
      <Row title={t("home.topTen", { name: library.name })} testId="top" count={top.data?.length ?? 0}>
        {top.data?.slice(0, 10).map((it, i) => <RankCard key={it.id} item={it} rank={i + 1} />)}
      </Row>
      {picked.map((g, i) => (
        <Row key={g} title={g} testId={`genre-${g}`} count={rows[i]?.data?.length ?? 0}>
          {rows[i]?.data?.map((it) => <TitleCard key={it.id} item={it} className={rowCard} />)}
        </Row>
      ))}
    </>
  );
}

/** Home: billboard and rows. */
export function HomeContent() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const libraries = useLibraries();
  const latest = useQuery({ queryKey: ["latest"], queryFn: async () => unwrap(await api.GET("/home/latest")) });
  const cont = useQuery({ queryKey: ["continue"], queryFn: async () => unwrap(await api.GET("/home/continue")) });
  const nextUp = useQuery({ queryKey: ["next-up"], queryFn: async () => unwrap(await api.GET("/home/next-up")) });
  const favs = useQuery({ queryKey: ["favorites"], queryFn: async () => unwrap(await api.GET("/home/favorites")) });
  const name = me?.currentProfile?.name ?? me?.username;
  return (
    <div className="flex flex-col pb-8" data-testid="home-page">
      <h1 className="sr-only">{t("home.welcome", { name })}</h1>
      <Billboard pool={latest.data} />
      <div className={`relative z-10 flex flex-col gap-7 sm:gap-10 ${latest.data?.length ? "-mt-[12vh] lg:-mt-[16vh]" : "pt-20"}`}>
        {latest.isPending && <><RowSkeleton /><RowSkeleton /></>}
        <Row title={t("home.continue")} testId="continue" count={cont.data?.length ?? 0}>
          {cont.data?.map((it) => <TitleCard key={it.id} item={it} className={rowCard} play />)}
        </Row>
        <Row title={t("home.nextUp")} testId="next-up" count={Array.isArray(nextUp.data) ? nextUp.data.length : 0}>
          {Array.isArray(nextUp.data) && nextUp.data.map((it) => <TitleCard key={it.id} item={it} className={rowCard} play />)}
        </Row>
        <Row title={t("home.latest")} testId="latest" count={latest.data?.length ?? 0}>
          {latest.data?.map((it) => <TitleCard key={it.id} item={it} className={rowCard} />)}
        </Row>
        <Row title={t("nav.myList")} testId="favorites" count={favs.data?.length ?? 0}>
          {favs.data?.map((it) => <TitleCard key={it.id} item={it} className={rowCard} />)}
        </Row>
        <CollectionsRow />
        {libraries.data?.map((l) => <LibraryRows key={l.id} library={l} />)}
        <LibraryShelf />
      </div>
    </div>
  );
}

function CollectionsRow() {
  const { t } = useTranslation();
  const cols = useCollections();
  const list = Array.isArray(cols.data) ? cols.data : [];
  return (
    <Row title={t("collections.title")} testId="collections" count={list.length}
      action={<Link to="/collections" className="text-sm text-white/60 hover:text-white">{t("collections.all")} ›</Link>}>
      {list.map((c) => <CollectionCard key={c.id} c={c} className="w-[30vw] shrink-0 sm:w-[160px] xl:w-[180px]" />)}
    </Row>
  );
}

/** Library tiles (bottom of home and the phone "Libraries" tab). */
export function LibraryShelf({ asPage = false }: { asPage?: boolean }) {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const libraries = useLibraries();
  if (libraries.data?.length === 0) {
    return (
      <section className={`flex flex-col items-start gap-3 ${gutter} ${asPage ? "pt-24" : ""}`}>
        <h2 className="text-xl font-bold">{t("home.librariesTitle")}</h2>
        <p className="max-w-prose text-sm text-muted">{me?.isAdmin ? t("home.noLibrariesAdmin") : t("home.noLibraries")}</p>
        {me?.isAdmin && <Link to="/settings/libraries" className={`${pressable} ${buttonVariants.play} h-10 rounded-md px-4 text-sm font-bold`}>{t("libraries.add")}</Link>}
      </section>
    );
  }
  return (
    <section className={`flex flex-col gap-3 ${gutter} ${asPage ? "pt-24" : ""}`}>
      {asPage
        ? <h1 className="text-3xl font-bold tracking-tight">{t("home.librariesTitle")}</h1>
        : <h2 className="text-[17px] font-bold tracking-tight sm:text-xl">{t("home.librariesTitle")}</h2>}
      <ErrorText error={libraries.error} />
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {libraries.data?.map((l) => (
          <Link key={l.id} to="/library/$libraryId" params={{ libraryId: l.id }} data-testid="library-card"
            className="tv-focus group relative flex aspect-[16/7] items-end overflow-hidden rounded-md p-4 ring-1 ring-white/10 transition-transform duration-200 hover:scale-[1.02]"
            style={{ background: `radial-gradient(120% 120% at 0% 100%, hsl(${placeholderHue(l.name)} 25% 20%) 0%, #181818 65%)` }}>
            <div>
              <p className="font-display text-3xl leading-none">{l.name}</p>
              <p className="mt-1.5 text-sm text-white/65">{t(`libraries.kind_${l.kind}`)} · {t("libraries.items", { count: l.itemCount })}</p>
            </div>
          </Link>
        ))}
      </div>
    </section>
  );
}

export function LibrariesPage() {
  return <LibraryShelf asPage />;
}

export function MyListPage() {
  const { t } = useTranslation();
  const favs = useQuery({ queryKey: ["favorites"], queryFn: async () => unwrap(await api.GET("/home/favorites")) });
  return (
    <div className={`flex flex-col gap-6 pt-24 ${gutter}`}>
      <h1 className="text-3xl font-bold tracking-tight sm:text-4xl">{t("nav.myList")}</h1>
      {favs.isPending && <Spinner />}
      {favs.data?.length === 0 && <p className="text-muted">{t("library.myListEmpty")}</p>}
      <div className="grid grid-cols-2 gap-x-2 gap-y-6 sm:grid-cols-3 lg:grid-cols-5 2xl:grid-cols-6">
        {favs.data?.map((it) => <TitleCard key={it.id} item={it} />)}
      </div>
    </div>
  );
}

export function LibraryPage() {
  const { t } = useTranslation();
  const { libraryId } = useParams({ from: "/app/library/$libraryId" });
  const libraries = useLibraries();
  const lib = libraries.data?.find((l) => l.id === libraryId);
  const [sort, setSort] = useState<"title" | "year" | "added" | "rating">("added");
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
  const [pool, setPool] = useState<ItemSummary[]>();
  // The billboard keeps the newest titles even while filters change the grid.
  useEffect(() => {
    if (!pool && items.data) setPool(all.slice(0, 10));
  }, [all, pool, items.data]);

  return (
    <div className="flex flex-col gap-6">
      <Billboard pool={pool} />
      <div className={`sticky top-16 z-30 -mt-2 flex flex-wrap items-center gap-x-5 gap-y-3 bg-bg/90 py-3 backdrop-blur-xl lg:top-[68px] ${gutter}`}>
        <h1 className="text-2xl font-bold tracking-tight sm:text-3xl">{lib?.name ?? "…"}</h1>
        {total !== undefined && <span className="text-sm text-muted tabular">{t("libraries.items", { count: total })}</span>}
        <div className="flex flex-wrap items-center gap-2 sm:ml-auto" role="group" aria-label={t("library.filters")}>
          {(genres.data?.length ?? 0) > 0 && (
            <Select value={genre} onChange={(e) => setGenre(e.target.value)} aria-label={t("library.genre")} className="rounded-sm bg-black">
              <option value="">{t("library.allGenres")}</option>
              {genres.data?.map((g) => <option key={g} value={g}>{g}</option>)}
            </Select>
          )}
          <Select value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} aria-label={t("library.sort")} className="rounded-sm bg-black">
            <option value="added">{t("library.sortAdded")}</option>
            <option value="title">{t("library.sortTitle")}</option>
            <option value="year">{t("library.sortYear")}</option>
            <option value="rating">{t("library.sortRating")}</option>
          </Select>
          <label className={`${pressable} h-9 cursor-pointer rounded-sm border px-3 text-sm ${unplayed ? "border-white bg-white text-black" : "border-white/30 text-white/80 hover:border-white/70"}`}>
            <input type="checkbox" className="sr-only" checked={unplayed} onChange={(e) => setUnplayed(e.target.checked)} />
            {unplayed && <Check className="size-3.5" aria-hidden />}
            {t("library.unplayedOnly")}
          </label>
        </div>
      </div>
      <div className={`flex flex-col gap-6 ${gutter}`}>
        <ErrorText error={items.error} />
        {items.isPending && <div className="flex justify-center py-20"><Spinner /></div>}
        {total === 0 && (
          <p className="py-16 text-center text-sm text-muted">{lib && ["queued", "scanning"].includes(lib.scan.state) ? t("library.scanning") : t("library.empty")}</p>
        )}
        <div className="grid grid-cols-3 gap-x-2 gap-y-6 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7 2xl:grid-cols-8">
          {all.map((it) => <ItemCard key={it.id} item={it} />)}
        </div>
        {items.hasNextPage && (
          <Button variant="secondary" className="self-center" disabled={items.isFetchingNextPage} onClick={() => void items.fetchNextPage()}>
            {items.isFetchingNextPage && <Spinner className="size-4" />}{t("library.more")}
          </Button>
        )}
      </div>
    </div>
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

const circle = "tv-focus grid size-11 shrink-0 place-items-center rounded-full border-2 border-white/50 bg-black/40 text-white backdrop-blur transition-[border-color,transform] duration-150 hover:border-white active:scale-95 disabled:opacity-50 sm:size-12";

/** My list and watched toggles for the current profile. */
function UserDataButtons({ item }: { item: ItemSummary }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const update = useMutation({
    mutationFn: async (body: { favorite?: boolean; played?: boolean }) =>
      unwrap(await api.PUT("/items/{itemId}/user-data", { params: { path: { itemId: item.id } }, body })),
    onSuccess: () => {
      for (const key of [["item"], ["library-items"], ["favorites"], ["continue"], ["latest"], ["row"]]) void qc.invalidateQueries({ queryKey: key });
    },
  });
  return (
    <>
      <button type="button" className={circle} title={item.favorite ? t("library.removeFromList") : t("library.addToList")}
        aria-label={item.favorite ? t("library.removeFromList") : t("library.addToList")} aria-pressed={!!item.favorite}
        data-testid="favorite" disabled={update.isPending} onClick={() => update.mutate({ favorite: !item.favorite })}>
        {item.favorite ? <Check className="size-5" aria-hidden /> : <Plus className="size-5" aria-hidden />}
      </button>
      <button type="button" className={`${circle} ${item.played ? "!border-accent text-accent" : ""}`}
        title={item.played ? t("library.markUnplayed") : t("library.markPlayed")}
        aria-label={item.played ? t("library.markUnplayed") : t("library.markPlayed")} aria-pressed={!!item.played}
        data-testid="played" disabled={update.isPending} onClick={() => update.mutate({ played: !item.played })}>
        <Check className="size-5" strokeWidth={item.played ? 3 : 2} aria-hidden />
      </button>
    </>
  );
}

/** Episode list of one season. */
function Episodes({ seasonId }: { seasonId: string }) {
  const season = useItemDetail(seasonId);
  const eps = season.data?.children.filter((c) => c.kind === "episode") ?? [];
  if (season.isPending) return <div className="py-8"><Spinner /></div>;
  return (
    <ol className="flex flex-col border-t border-white/10">
      {eps.map((e) => <EpisodeRow key={e.id} item={e} />)}
    </ol>
  );
}

function EpisodeRow({ item: c }: { item: ItemSummary }) {
  const { t } = useTranslation();
  const [broken, setBroken] = useState(false);
  const playable = isPlayable(c);
  return (
    <li className="border-b border-white/10">
      <Link to={playable ? "/watch/$itemId" : "/item/$itemId"} params={{ itemId: c.id }} data-testid="child-row"
        className="tv-focus group flex items-center gap-4 rounded-md px-2 py-4 transition-colors hover:bg-white/[0.05] sm:gap-6 sm:px-4">
        <span className="hidden w-8 shrink-0 text-center text-2xl text-white/60 tabular sm:block">{c.episodeNumber ?? ""}</span>
        <div className="relative aspect-video w-32 shrink-0 overflow-hidden rounded-md bg-surface-2 sm:w-40">
          {c.hasPoster && !broken
            ? <Art src={imageUrl(c.id, "still")} onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover" />
            : <ArtFallback item={c} wide />}
          {playable && (
            <span className="absolute inset-0 grid place-items-center bg-black/30 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
              <span className="grid size-10 place-items-center rounded-full border-2 border-white bg-black/50"><Play className="ml-0.5 size-4 fill-white" aria-hidden /></span>
            </span>
          )}
          <ProgressBar value={progressOf(c)} className="absolute inset-x-0 bottom-0" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline justify-between gap-3">
            <p className="truncate text-[15px] font-semibold">{c.title}</p>
            <span className="shrink-0 text-sm text-white/70 tabular">{formatDuration(c.durationMs)}</span>
          </div>
          <p className="mt-1 text-xs text-muted">{episodeLabel(c)}{c.played ? ` · ${t("library.watched")}` : ""}</p>
        </div>
      </Link>
    </li>
  );
}

function initials(name: string): string {
  return name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join("");
}

/** Same-genre titles from the same library. */
function MoreLikeThis({ detail }: { detail: ItemDetail }) {
  const { t } = useTranslation();
  const genre = detail.genres[0];
  const more = useQuery({
    queryKey: ["row", "like", detail.item.id],
    enabled: !!genre && detail.item.kind !== "episode" && detail.item.kind !== "season",
    queryFn: async () => unwrap(await api.GET("/libraries/{libraryId}/items", {
      params: { path: { libraryId: detail.libraryId }, query: { limit: 13, sort: "rating", genre } },
    })).items.filter((i) => i.id !== detail.item.id).slice(0, 12),
  });
  if (!more.data?.length) return null;
  return (
    <section className={`flex flex-col gap-4 ${gutter}`}>
      <h2 className="text-2xl font-bold tracking-tight">{t("library.moreLikeThis")}</h2>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4 2xl:grid-cols-6">
        {more.data.map((it) => <TitleCard key={it.id} item={it} />)}
      </div>
    </section>
  );
}

export function ItemPage() {
  const { t } = useTranslation();
  const isAdmin = !!useMe().data?.isAdmin;
  const { itemId } = useParams({ from: "/app/item/$itemId" });
  const detail = useItemDetail(itemId);
  const d = detail.data;
  const seasons = d?.children.filter((c) => c.kind === "season") ?? [];
  const [season, setSeason] = useState("");
  const seasonIds = seasons.map((s) => s.id).join();
  useEffect(() => {
    if (seasons.length && !seasons.some((s) => s.id === season)) {
      setSeason((seasons.find((s) => s.seasonNumber === 1) ?? seasons[0]!).id);
    }
  }, [seasonIds]); // eslint-disable-line react-hooks/exhaustive-deps

  if (detail.error) return <div className={`pt-24 ${gutter}`}><ErrorText error={detail.error} /></div>;
  if (!d) return <div role="status" className="flex min-h-[70vh] items-center justify-center"><Spinner /></div>;
  const it = { ...d.item, hasBackdrop: d.hasBackdrop };
  const isEpisode = it.kind === "episode";
  const directors = d.crew.filter((p) => p.role === "Director").map((p) => p.name);
  const writers = d.crew.filter((p) => p.role !== "Director").map((p) => p.name);
  const rows = d.children.filter((c) => c.kind !== "season");
  const title = isEpisode ? `${episodeLabel(it)} · ${it.title}` : it.title;
  const sidePoster = "absolute top-28 right-[6vw] -z-10 hidden aspect-[2/3] w-60 overflow-hidden rounded-md shadow-[0_30px_60px_rgb(0_0_0/0.8)] lg:block xl:w-72";

  return (
    <div className="flex flex-col gap-12 pb-6">
      <section className="relative isolate flex min-h-[80vh] flex-col justify-end pt-28 pb-10" data-testid="item-hero">
        <BillboardArt item={it} hasBackdrop={d.hasBackdrop || (isEpisode && it.hasPoster)} still={!d.hasBackdrop && isEpisode} />
        <PreviewVideo detail={d} />
        {!d.hasBackdrop && !isEpisode && (
          <div className={sidePoster}>
            {it.hasPoster ? <Art src={imageUrl(it.id, "poster")} loading="eager" className="absolute inset-0 size-full object-cover" /> : <ArtFallback item={it} />}
          </div>
        )}
        <div className={`flex max-w-[48rem] flex-col gap-4 ${gutter}`}>
          <h1 className="enter">
            {isEpisode
              ? <span className="block font-display text-[40px] leading-[0.92] text-balance drop-shadow-lg sm:text-6xl lg:text-7xl">{title}</span>
              : <TitleArt item={{ ...it, hasLogo: d.hasLogo }} className="max-h-[24vh] w-auto max-w-[min(34rem,82vw)]"
                  textClass="block font-display text-[40px] leading-[0.92] text-balance drop-shadow-lg sm:text-6xl lg:text-7xl" />}
          </h1>
          {d.originalTitle && d.originalTitle !== it.title && <p className="-mt-1 text-sm text-white/60">{d.originalTitle}</p>}
          <Facts item={it} detail={d} className="enter-2" />
          <div className="enter-3 mt-1 flex flex-wrap items-center gap-3">
            {d.files.length > 0 && (
              <Link to="/watch/$itemId" params={{ itemId: it.id }} data-testid="play"
                className={`${pressable} ${buttonVariants.play} tv-focus h-11 rounded-md px-6 text-base font-bold sm:h-12 sm:px-8 sm:text-lg`}>
                <Play className="size-5 fill-current sm:size-6" aria-hidden />
                {it.positionMs ? t("player.resume", { time: formatClock(it.positionMs) }) : t("player.play")}
              </Link>
            )}
            {d.files.length > 0 && it.positionMs ? (
              <Link to="/watch/$itemId" params={{ itemId: it.id }} search={{ start: 0 }}
                className={`${pressable} ${buttonVariants.muted} tv-focus h-11 rounded-md px-5 text-base font-bold sm:h-12`}>
                <RotateCcw className="size-5" aria-hidden />{t("player.fromStart")}
              </Link>
            ) : null}
            <UserDataButtons item={it} />
            {it.kind !== "season" && <AddToMenu item={it} />}
            {isAdmin && <MetadataMenu d={d} />}
          </div>
          {progressOf(it) > 0 && <ProgressBar value={progressOf(it)} className="max-w-sm rounded-full" />}
        </div>
      </section>

      <section className={`grid gap-8 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)] ${gutter}`}>
        <div className="flex flex-col gap-3">
          {d.tagline && <p className="text-lg font-semibold text-white/90">{d.tagline}</p>}
          {d.overview && <p className="max-w-[70ch] text-base leading-relaxed text-white/85 sm:text-[17px]" data-testid="overview">{d.overview}</p>}
          {d.metadataError && <p className="text-xs text-muted">{t("library.noMetadata")}</p>}
        </div>
        <dl className="flex flex-col gap-2.5 text-sm">
          {d.cast.length > 0 && <div><dt className="inline text-muted">{t("library.starring")}: </dt><dd className="inline">{d.cast.slice(0, 5).map((p) => p.name).join(", ")}</dd></div>}
          {d.genres.length > 0 && <div><dt className="inline text-muted">{t("library.genres")}: </dt><dd className="inline">{d.genres.join(", ")}</dd></div>}
          {(d.collections?.length ?? 0) > 0 && (
            <div><dt className="inline text-muted">{t("collections.partOf")}: </dt>
              <dd className="inline">{d.collections!.map((c, i) => (
                <span key={c.id}>{i > 0 && ", "}<Link to="/collection/$collectionId" params={{ collectionId: c.id }} className="underline-offset-2 hover:underline">{c.name}</Link></span>
              ))}</dd>
            </div>
          )}
          {directors.length > 0 && <div><dt className="inline text-muted">{t("library.directedBy")}: </dt><dd className="inline">{directors.join(", ")}</dd></div>}
          {writers.length > 0 && <div><dt className="inline text-muted">{t("library.writtenBy")}: </dt><dd className="inline">{writers.join(", ")}</dd></div>}
        </dl>
      </section>

      {seasons.length > 0 && (
        <section className={`flex flex-col gap-4 ${gutter}`} data-testid="seasons">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="text-2xl font-bold tracking-tight">{t("library.episodes")}</h2>
            <Select value={season} onChange={(e) => setSeason(e.target.value)} aria-label={t("library.selectSeason")}
              className="h-11 rounded-sm border-white/30 bg-surface-2 px-4 text-base font-semibold">
              {seasons.map((s) => (
                <option key={s.id} value={s.id}>{s.seasonNumber === 0 ? t("library.specials") : t("library.season", { n: s.seasonNumber })}</option>
              ))}
            </Select>
          </div>
          {season && <Episodes seasonId={season} />}
        </section>
      )}

      {rows.length > 0 && (
        <section className={`flex flex-col gap-4 ${gutter}`}>
          <h2 className="text-2xl font-bold tracking-tight">{t("library.episodes")}</h2>
          <ol className="flex flex-col border-t border-white/10">
            {rows.map((c) => <EpisodeRow key={c.id} item={c} />)}
          </ol>
        </section>
      )}

      {d.cast.length > 0 && (
        <section className="flex flex-col gap-4">
          <h2 className={`text-2xl font-bold tracking-tight ${gutter}`}>{t("library.cast")}</h2>
          <ul className={`shelf-track flex gap-4 overflow-x-auto pb-2 ${gutter}`} data-testid="cast">
            {d.cast.map((p) => (
              <li key={p.name + (p.role ?? "")} className="flex w-[112px] shrink-0 flex-col items-center gap-2 text-center">
                <span aria-hidden className="relative grid size-[88px] place-items-center overflow-hidden rounded-full text-2xl font-bold text-white/70 ring-1 ring-white/10"
                  style={{ background: `radial-gradient(circle at 30% 20%, hsl(${placeholderHue(p.name)} 20% 26%), #1a1a1a 75%)` }}>
                  {initials(p.name)}
                  {p.imageUrl && <Art src={p.imageUrl} className="absolute inset-0 size-full object-cover object-top" />}
                </span>
                <span className="min-w-0">
                  <span className="line-clamp-2 text-[13px] leading-tight font-semibold">{p.name}</span>
                  {p.role && <span className="mt-0.5 line-clamp-2 text-xs leading-tight text-muted">{p.role}</span>}
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}

      <MoreLikeThis detail={d} />

      {d.files.length > 0 && (
        <section className={`flex flex-col gap-4 ${gutter}`}>
          <h2 className="text-2xl font-bold tracking-tight">
            {t("library.mediaInfo")}
            <span className="ml-2 text-sm font-normal text-muted">{d.files.length > 1 ? t("library.versions", { count: d.files.length }) : t("library.file")}</span>
          </h2>
          <ul className="grid gap-3 lg:grid-cols-2">
            {d.files.map((f) => (
              <li key={f.id} className="flex min-w-0 flex-col gap-3 rounded-md bg-surface p-4 text-sm" data-testid="media-file">
                <div className="flex flex-wrap items-center gap-2">
                  {f.label && <span className="mr-1 font-semibold">{f.label}</span>}
                  {resolutionLabel(f.width, f.height) && <Badge tone="accent">{resolutionLabel(f.width, f.height)}</Badge>}
                  {f.hdr && <Badge tone="success">{f.hdr}</Badge>}
                  {f.videoCodec && <Badge>{f.videoCodec.toUpperCase()}</Badge>}
                  <Badge>{f.container.toUpperCase()}</Badge>
                  <span className="ml-auto text-xs text-muted tabular">{formatSize(f.size)} · {formatDuration(f.durationMs)}</span>
                </div>
                {f.audio && f.audio.length > 0 && (
                  <p className="text-xs text-muted"><span className="text-white/80">{t("library.audio")}:</span>{" "}
                    {f.audio.map((a) => [a.language?.toUpperCase(), a.codec.toUpperCase(), channelsLabel(a.channels), a.title].filter(Boolean).join(" ")).join(" · ")}
                  </p>
                )}
                {f.subtitles && f.subtitles.length > 0 && (
                  <p className="text-xs text-muted"><span className="text-white/80">{t("library.subtitles")}:</span>{" "}
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
