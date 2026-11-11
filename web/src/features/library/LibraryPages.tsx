import { Fragment, useEffect, useRef, useState, type ReactNode } from "react";
import { HOME_SECTIONS, type HomeSection } from "@/features/settings/usePreferences";
import { useInfiniteQuery, useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Check, Info, LayoutGrid, List, Play, Plus, RotateCcw, Shuffle, Volume2, VolumeX, X } from "lucide-react";
import { useEnhancements, useSurprise } from "@/features/enhancements/useEnhancements";
import { AlbumView, ArtistView, MusicLibrary } from "@/features/music/MusicPages";
import { BookCard, BookView, BooksLibrary } from "@/features/books/BooksPages";
import { PhotoAlbumView, PhotoItemView, PhotosLibrary } from "@/features/photos/PhotosPages";

/** Libraries with their own pages (not the movie/show grid, not on the home rows). */
const ownPages = ["music", "books", "audiobooks", "photos"];
import { SubtitleFinder } from "./SubtitleFinder";
import { DownloadOriginalButton } from "@/features/downloads/DownloadOriginal";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Art, Badge, Button, ErrorText, IconButton, Select, Spinner, buttonVariants, pressable } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { channelsLabel, formatDuration, formatSize, imageUrl, placeholderHue, resolutionLabel, type ItemSummary, type Library } from "./format";
import { AddToMenu, CollectionCard, useCollections } from "./CollectionsPages";
import { MetadataMenu } from "./MetadataEditor";
import { ArtFallback, ItemCard, PortraitArt, ProgressBar, RankCard, Row, RowSkeleton, TitleArt, TitleCard, isPlayable, posterCard, progressOf, rowCard } from "./cards";
import { WatchTogetherButton } from "@/features/syncplay/WatchTogether";
import { useAmbient, useAmbientLight } from "@/features/ambient/Ambient";

export { ItemCard } from "./cards";

type ItemDetail = components["schemas"]["ItemDetail"];
const PAGE = 60;
const gutter = "px-5 sm:px-10 lg:px-16";

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

/** The picture on the screen: backdrop art, or a tinted ground when there is none. */
function BillboardArt({ item, hasBackdrop, still = false }: { item: ItemSummary; hasBackdrop: boolean; still?: boolean }) {
  const [broken, setBroken] = useState(false);
  const hue = placeholderHue(item.title);
  return (
    <div className="screen-settle absolute inset-0 overflow-hidden" aria-hidden>
      {hasBackdrop && !broken ? (
        <Art src={imageUrl(item.id, still ? "still" : "backdrop")} loading="eager" onError={() => setBroken(true)}
          className="absolute inset-0 size-full object-cover object-[60%_30%]" />
      ) : (
        <div className="absolute inset-0" style={{ background: `radial-gradient(80% 90% at 70% 30%, hsl(${hue} 22% 22%) 0%, var(--color-bg) 75%)` }} />
      )}
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
 * Muted motion preview on the screen: starts after a short rest, only when the file
 * plays directly, pauses off-screen, and gives way to the still.
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
    if (!v || !src || typeof IntersectionObserver === "undefined") return;
    const io = new IntersectionObserver(([e]) => { if (e && !e.isIntersecting) v.pause(); else if (e?.isIntersecting && !v.ended) void v.play().catch(() => undefined); }, { threshold: 0.3 });
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
        className={`absolute inset-0 size-full object-cover object-[60%_30%] transition-opacity duration-[1400ms] ease-out ${playing ? "opacity-100" : "opacity-0"}`} />
      {playing && (
        <button type="button" onClick={() => setMuted(!muted)} aria-label={muted ? t("player.unmute") : t("player.mute")}
          className="tv-focus absolute top-24 right-5 z-20 grid size-10 place-items-center rounded-[6px] bg-black/45 text-text backdrop-blur transition-colors hover:bg-black/70 sm:right-10 lg:right-16">
          {muted ? <VolumeX className="size-4" aria-hidden /> : <Volume2 className="size-4" aria-hidden />}
        </button>
      )}
    </>
  );
}

/**
 * The screen: the film's picture edge to edge behind the page top, darkened toward the
 * text and lit low on the left in the film's colour. Content sits on top.
 */
function Screen({ item, detail, still = false, children, className = "", testId }: {
  item: ItemSummary; detail?: ItemDetail; still?: boolean; children: ReactNode; className?: string; testId?: string;
}) {
  const hasArt = !!item.hasBackdrop || (still && item.hasPoster);
  return (
    <section className={`relative isolate flex flex-col justify-end ${className}`} data-testid={testId}>
      <div className="absolute inset-x-0 top-0 -z-10 h-[64svh] overflow-hidden md:inset-0 md:h-auto">
        <BillboardArt key={item.id} item={item} hasBackdrop={hasArt} still={still && !item.hasBackdrop} />
        <PreviewVideo detail={detail} />
        <div className="screen-scrim absolute inset-0" />
      </div>
      {children}
    </section>
  );
}

/** Year, length, rating and picture as one quiet line of text. */
function Facts({ item, detail, className = "" }: { item: ItemSummary; detail?: ItemDetail; className?: string }) {
  const { t } = useTranslation();
  const file = detail?.files[0];
  const res = resolutionLabel(file?.width, file?.height);
  const parts: ReactNode[] = [];
  if (item.rating) parts.push(<span key="r" className="font-semibold text-text">★ {item.rating.toFixed(1)}</span>);
  if (item.year) parts.push(<span key="y">{item.year}</span>);
  parts.push(<span key="d">{item.durationMs ? formatDuration(item.durationMs) : t(`library.kind_${item.kind}`)}</span>);
  if (detail?.genres[0]) parts.push(<span key="g">{detail.genres.slice(0, 2).join(", ")}</span>);
  if (detail?.officialRating) parts.push(<span key="o" className="rounded-[3px] px-1.5 ring-1 ring-text/40">{detail.officialRating}</span>);
  if (res) parts.push(<span key="q" className="font-semibold text-text">{res === "4K" ? "4K" : "HD"}</span>);
  if (file?.hdr) parts.push(<span key="h" className="font-semibold text-text">{file.hdr}</span>);
  return (
    <p className={`flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[15px] text-text/70 tabular ${className}`}>
      {parts.map((p, i) => <span key={i} className="contents">{i > 0 && <span aria-hidden className="text-text/35">·</span>}{p}</span>)}
    </p>
  );
}

/** Watched so far, as a line and the time that is left. */
function Watched({ item }: { item: ItemSummary }) {
  const { t } = useTranslation();
  const p = progressOf(item);
  if (!p || !item.durationMs || !item.positionMs) return null;
  const left = Math.max(1, Math.round((item.durationMs - item.positionMs) / 60000));
  return (
    <div className="flex items-center gap-3 text-[13px] text-text/60 tabular">
      <ProgressBar value={p} className="w-44" />
      {t("home.minutesLeft", { count: left })}
    </div>
  );
}

const stageGutter = "px-5 sm:px-10 lg:px-16";
const bigButton = "tv-focus h-13 rounded-[6px] px-7 text-base font-semibold";

/**
 * The stage (home and libraries): one film on the screen, its title set like a title
 * card, and the strip of posters along the bottom; focusing a poster puts that film on
 * the screen and its colour into the interface.
 */
export function Billboard({ pool, label }: { pool?: ItemSummary[]; label?: string }) {
  const { t } = useTranslation();
  const enh = useEnhancements();
  const surprise = useSurprise();
  const libs = useLibraries();
  const { light } = useAmbient();
  const choices = (pool ?? []).filter((i, n, all) => all.findIndex((x) => x.id === i.id) === n).slice(0, 14);
  const [pickId, setPickId] = useState<string>();
  const pick = choices.find((i) => i.id === pickId) ?? choices.find((i) => i.hasBackdrop) ?? choices[0];
  const detail = useItemDetail(pick?.id);
  useEffect(() => { if (pick) light(pick.colors); }, [pick?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  const timer = useRef<number | undefined>(undefined);
  const choose = (id: string) => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setPickId(id), 180);
  };
  if (!pool) return <div aria-hidden className="h-[86svh] min-h-[520px]" />;
  if (!pick) return <div className="h-24" />;
  const d = detail.data;
  return (
    <Screen item={pick} detail={d} className="min-h-[86svh] pt-[46svh] md:min-h-[min(100svh,980px)] md:pt-32" testId="home-hero">
      <div className={stageGutter}><div key={pick.id} className="flex max-w-[44rem] min-w-0 flex-col gap-5">
        <h2 className="enter">
          <TitleArt item={pick} className="max-h-[22vh] w-auto max-w-[min(34rem,82vw)]"
            textClass="block font-display text-[clamp(2.9rem,7.4vw,7rem)] text-balance" />
        </h2>
        <Facts item={pick} detail={d} className="enter-2" />
        {d?.overview && <p className="enter-2 line-clamp-3 max-w-[48ch] text-[15px] leading-relaxed text-text/72 sm:text-[17px]">{d.overview}</p>}
        <div className="enter-3 mt-2 flex flex-wrap gap-3">
          {isPlayable(pick) && (
            <Link to="/watch/$itemId" params={{ itemId: pick.id }} className={`${pressable} ${buttonVariants.primary} ${bigButton}`}>
              <Play className="size-5 fill-current" aria-hidden />
              {pick.positionMs ? t("player.resume", { time: formatClock(pick.positionMs) }) : t("player.play")}
            </Link>
          )}
          <Link to="/item/$itemId" params={{ itemId: pick.id }} className={`${pressable} ${buttonVariants.glass} ${bigButton}`}>
            <Info className="size-5" aria-hidden />{t("home.moreInfo")}
          </Link>
          {enh.randomButton && (
            <button type="button" onClick={() => void surprise((Array.isArray(libs.data) ? libs.data : []).filter((l) => !ownPages.includes(l.kind)).map((l) => l.id))}
              aria-label={t("home.surprise")} title={t("home.surprise")} data-testid="surprise"
              className={`${pressable} ${buttonVariants.glass} tv-focus size-13 rounded-[6px]`}>
              <Shuffle className="size-5" aria-hidden />
            </button>
          )}
        </div>
        <Watched item={pick} />
      </div></div>
      {choices.length > 1 && (
        <div className="mt-10 flex flex-col gap-3 md:mt-14">
          {label && <p className={`font-label text-text/55 ${stageGutter}`}>{label}</p>}
          <div className="shelf-track flex gap-3 overflow-x-auto px-5 pt-2 pb-8 sm:px-10 lg:gap-4 lg:px-16" role="listbox" aria-label={label ?? t("home.latest")}>
            {choices.map((it) => {
              const on = it.id === pick.id;
              return (
                <button key={it.id} type="button" role="option" aria-selected={on} aria-label={it.title}
                  onMouseEnter={() => choose(it.id)} onFocus={() => { window.clearTimeout(timer.current); setPickId(it.id); }}
                  onClick={() => setPickId(it.id)}
                  className={`relative aspect-[2/3] w-[96px] shrink-0 overflow-hidden rounded-[6px] bg-surface-2 transition-[transform,opacity,outline-color] duration-300 ease-out outline-2 outline-offset-4 sm:w-[120px] lg:w-[136px] ${on ? "-translate-y-1.5 opacity-100 outline-text" : "opacity-60 outline-transparent hover:opacity-95"}`}>
                  <PortraitArt item={it} />
                </button>
              );
            })}
          </div>
        </div>
      )}
    </Screen>
  );
}

/** Top-rated and genre strips for one library. */
/** The best rated titles of one library, ranked. */
function LibraryTop({ library }: { library: Library }) {
  const { t } = useTranslation();
  const top = useQuery({
    queryKey: ["row", `top-${library.id}`],
    queryFn: async () =>
      unwrap(await api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId: library.id }, query: { limit: 20, sort: "rating" } } }))
        .items.filter((i) => i.rating),
  });
  return (
    <Row title={t("home.topTen", { name: library.name })} testId="top" count={top.data?.length ?? 0}>
      {top.data?.slice(0, 10).map((it, i) => <RankCard key={it.id} item={it} rank={i + 1} />)}
    </Row>
  );
}

/** A strip per genre for one library. */
function LibraryRows({ library }: { library: Library }) {
  const genres = useQuery({
    queryKey: ["genres", library.id],
    queryFn: async () => unwrap(await api.GET("/libraries/{libraryId}/genres", { params: { path: { libraryId: library.id } } })),
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
      {picked.map((g, i) => (
        <Row key={g} title={g} testId={`genre-${g}`} count={rows[i]?.data?.length ?? 0}>
          {rows[i]?.data?.map((it) => <ItemCard key={it.id} item={it} className={posterCard} />)}
        </Row>
      ))}
    </>
  );
}

/** Admins without a TMDb key get one clear way to turn on proper metadata. */
function MetadataNudge() {
  const { t } = useTranslation();
  const meta = useQuery({ queryKey: ["settings", "metadata"], queryFn: async () => unwrap(await api.GET("/settings/metadata")), retry: false });
  const [hidden, setHidden] = useState(() => { try { return sessionStorage.getItem("tmdb-nudge") === "1"; } catch { return false; } });
  if (hidden || !meta.data || meta.data.tmdbConfigured) return null;
  return (
    <div className="fixed inset-x-3 top-[4.75rem] z-30 mx-auto flex max-w-3xl flex-wrap items-center gap-3 rounded-[8px] border border-[color-mix(in_oklab,var(--amb-a)_45%,transparent)] bg-surface/95 p-4 shadow-2xl backdrop-blur-xl" role="status" data-testid="tmdb-nudge">
      <Info className="size-5 shrink-0 text-[var(--amb-a)]" aria-hidden />
      <p className="min-w-0 flex-1 text-sm text-text/85"><span className="font-semibold text-text">{t("metadata.nudgeTitle")}</span> {t("metadata.nudgeText")}</p>
      <Link to="/settings/metadata" className={`${pressable} ${buttonVariants.primary} h-9 rounded-[6px] px-4 text-sm font-semibold`}>{t("metadata.nudgeAction")}</Link>
      <button type="button" onClick={() => { setHidden(true); try { sessionStorage.setItem("tmdb-nudge", "1"); } catch { /* private mode */ } }}
        aria-label={t("common.close")} className="grid size-8 place-items-center rounded-[6px] text-text/60 hover:bg-white/10 hover:text-text"><X className="size-4" aria-hidden /></button>
    </div>
  );
}

/** Home: the stage, then strips. */
export function HomeContent() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const libraries = useLibraries();
  const latest = useQuery({ queryKey: ["latest"], queryFn: async () => unwrap(await api.GET("/home/latest")) });
  const cont = useQuery({ queryKey: ["continue"], queryFn: async () => unwrap(await api.GET("/home/continue")) });
  const nextUp = useQuery({ queryKey: ["next-up"], queryFn: async () => unwrap(await api.GET("/home/next-up")) });
  const reading = useQuery({ queryKey: ["continue-reading"], queryFn: async () => unwrap(await api.GET("/home/continue-reading")) });
  const favs = useQuery({ queryKey: ["favorites"], queryFn: async () => unwrap(await api.GET("/home/favorites")) });
  const name = me?.currentProfile?.name ?? me?.username;
  const up = Array.isArray(nextUp.data) ? nextUp.data : [];
  const prefs = useQuery({ queryKey: ["preferences"], queryFn: async () => unwrap(await api.GET("/me/preferences")) });
  const sections = (prefs.data?.homeSections?.length ? prefs.data.homeSections : HOME_SECTIONS) as readonly HomeSection[];
  const libs = Array.isArray(libraries.data) ? libraries.data : [];
  const blocks: Record<HomeSection, ReactNode> = {
    continue: (
      <>
        <Row title={t("home.continue")} testId="continue" count={cont.data?.length ?? 0}>
          {cont.data?.map((it) => <TitleCard key={it.id} item={it} className={rowCard} play />)}
        </Row>
        <Row title={t("books.continueRow")} testId="continue-reading" count={reading.data?.length ?? 0}>
          {reading.data?.map((it) => <div key={it.id} className="w-[34vw] shrink-0 sm:w-[150px] xl:w-[170px]"><BookCard item={it} /></div>)}
        </Row>
      </>
    ),
    nextUp: (
      <Row title={t("home.nextUp")} testId="next-up" count={up.length}>
        {up.map((it) => <TitleCard key={it.id} item={it} className={rowCard} play />)}
      </Row>
    ),
    latest: (
      <Row title={t("home.latest")} testId="latest" count={latest.data?.length ?? 0}>
        {latest.data?.map((it) => <ItemCard key={it.id} item={it} className={posterCard} />)}
      </Row>
    ),
    favorites: (
      <Row title={t("nav.myList")} testId="favorites" count={favs.data?.length ?? 0}>
        {favs.data?.map((it) => <ItemCard key={it.id} item={it} className={posterCard} />)}
      </Row>
    ),
    collections: <CollectionsRow />,
    top: libs.filter((l) => !ownPages.includes(l.kind)).map((l) => <LibraryTop key={l.id} library={l} />),
    genres: libs.filter((l) => !ownPages.includes(l.kind)).map((l) => <LibraryRows key={l.id} library={l} />),
    libraries: <LibraryShelf />,
  };
  return (
    <div className="flex flex-col gap-12 pb-8 sm:gap-14" data-testid="home-page">
      <h1 className="sr-only">{t("home.welcome", { name })}</h1>
      {me?.isAdmin && <MetadataNudge />}
      <Billboard pool={latest.data} label={t("home.latest")} />
      {latest.isPending && <><RowSkeleton /><RowSkeleton /></>}
      {sections.map((sec) => <Fragment key={sec}>{blocks[sec]}</Fragment>)}
    </div>
  );
}

function CollectionsRow() {
  const { t } = useTranslation();
  const cols = useCollections();
  const list = Array.isArray(cols.data) ? cols.data : [];
  return (
    <Row title={t("collections.title")} testId="collections" count={list.length}
      action={<Link to="/collections" className="pb-0.5 text-sm font-semibold text-text/55 hover:text-text">{t("collections.all")}</Link>}>
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
        <h2 className="font-head text-xl">{t("home.librariesTitle")}</h2>
        <p className="max-w-prose text-sm text-muted">{me?.isAdmin ? t("home.noLibrariesAdmin") : t("home.noLibraries")}</p>
        {me?.isAdmin && <Link to="/settings/libraries" className={`${pressable} ${buttonVariants.primary} h-10 rounded-[6px] px-5 text-sm font-semibold`}>{t("libraries.add")}</Link>}
      </section>
    );
  }
  return (
    <section className={`flex flex-col gap-5 ${gutter} ${asPage ? "pt-28" : ""}`}>
      {asPage
        ? <h1 className="font-display text-4xl sm:text-5xl">{t("home.librariesTitle")}</h1>
        : <h2 className="font-head text-xl leading-none sm:text-[26px]">{t("home.librariesTitle")}</h2>}
      <ErrorText error={libraries.error} />
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {libraries.data?.map((l) => (
          <Link key={l.id} to="/library/$libraryId" params={{ libraryId: l.id }} data-testid="library-card"
            className="tv-focus group relative flex aspect-[16/7] items-end overflow-hidden rounded-[6px] border border-border p-5 transition-colors duration-200 hover:border-border-strong"
            style={{ background: `linear-gradient(150deg, hsl(${placeholderHue(l.name)} 20% 17%) 0%, var(--color-surface) 70%)` }}>
            <div>
              <p className="font-display text-3xl">{l.name}</p>
              <p className="mt-2 text-sm text-text/60">{t(`libraries.kind_${l.kind}`)} · {t("libraries.items", { count: l.itemCount })}</p>
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
    <div className={`flex flex-col gap-8 pt-28 ${gutter}`}>
      <h1 className="font-display text-4xl sm:text-5xl">{t("nav.myList")}</h1>
      {favs.isPending && <Spinner />}
      {favs.data?.length === 0 && <p className="text-muted">{t("library.myListEmpty")}</p>}
      <div className="grid grid-cols-1 gap-x-5 gap-y-6 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
        {favs.data?.map((it) => <TitleCard key={it.id} item={it} />)}
      </div>
    </div>
  );
}

type LibTab = "all" | "suggested" | "genres" | "collections" | "favorites";
type Status = "" | "unplayed" | "resumable" | "favorite";
const LETTERS = ["#", ..."ABCDEFGHIJKLMNOPQRSTUVWXYZ"];

/** Recommendations inside one library: what is started, newest, best rated, and by genre. */
function LibrarySuggestions({ library }: { library: Library }) {
  const { t } = useTranslation();
  const q = (key: string, query: Record<string, unknown>) => ({
    queryKey: ["row", key, library.id],
    queryFn: async () => unwrap(await api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId: library.id }, query: { limit: 20, ...query } } })).items,
  });
  const resume = useQuery(q("resume", { resumable: true, sort: "added" }));
  const latest = useQuery(q("latest", { sort: "added" }));
  const unwatched = useQuery(q("unwatched", { unplayed: true, sort: "rating" }));
  return (
    <div className="flex flex-col gap-12">
      <Row title={t("home.continue")} testId="lib-resume" count={resume.data?.length ?? 0}>
        {resume.data?.map((it) => <TitleCard key={it.id} item={it} className={rowCard} play />)}
      </Row>
      <Row title={t("home.latest")} testId="lib-latest" count={latest.data?.length ?? 0}>
        {latest.data?.map((it) => <ItemCard key={it.id} item={it} className={posterCard} />)}
      </Row>
      <Row title={t("library.notWatchedYet")} testId="lib-unwatched" count={unwatched.data?.length ?? 0}>
        {unwatched.data?.map((it) => <TitleCard key={it.id} item={it} className={rowCard} />)}
      </Row>
      <LibraryTop library={library} />
      <LibraryRows library={library} />
    </div>
  );
}

/** Genre tiles, each shown through a picture from one of its titles. */
function GenreGrid({ library, onPick }: { library: Library; onPick: (g: string) => void }) {
  const { t } = useTranslation();
  const genres = useQuery({
    queryKey: ["genres", library.id],
    queryFn: async () => unwrap(await api.GET("/libraries/{libraryId}/genres", { params: { path: { libraryId: library.id } } })),
  });
  const list = genres.data ?? [];
  const covers = useQueries({
    queries: list.map((g) => ({
      queryKey: ["row", `genre-cover-${library.id}-${g}`],
      queryFn: async () => unwrap(await api.GET("/libraries/{libraryId}/items", {
        params: { path: { libraryId: library.id }, query: { limit: 6, sort: "rating" as const, genre: g } },
      })),
    })),
  });
  if (genres.isPending) return <div className="flex justify-center py-20"><Spinner /></div>;
  if (!list.length) return <p className="py-16 text-center text-sm text-muted">{t("library.noGenres")}</p>;
  const used = new Set<string>();
  return (
    <div className={`grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4 ${gutter}`}>
      {list.map((g, i) => {
        const page = covers[i]?.data;
        // Each tile gets a different film where the library allows it.
        const cover = page?.items.find((x) => x.hasBackdrop && !used.has(x.id)) ?? page?.items.find((x) => x.hasBackdrop) ?? page?.items[0];
        if (cover) used.add(cover.id);
        return (
          <button key={g} type="button" onClick={() => onPick(g)} data-testid="genre-tile"
            className="tv-focus group relative aspect-[16/8] overflow-hidden rounded-[6px] bg-surface-2 text-left">
            {cover?.hasBackdrop && <Art src={imageUrl(cover.id, "backdrop")} className="absolute inset-0 size-full object-cover transition-transform duration-700 ease-out group-hover:scale-105" />}
            <span className="absolute inset-0 bg-gradient-to-t from-black/90 via-black/40 to-black/10" />
            <span className="absolute inset-x-0 bottom-0 flex items-end justify-between gap-3 p-5">
              <span className="font-display text-3xl">{g}</span>
              {page && <span className="pb-1 text-sm text-text/65 tabular">{t("libraries.items", { count: page.total })}</span>}
            </span>
          </button>
        );
      })}
    </div>
  );
}

/** One row of the list view: poster, title, year, length, genres and the watched state. */
function ListRow({ it }: { it: ItemSummary }) {
  const { t } = useTranslation();
  return (
    <li className="border-b border-border">
      <Link to="/item/$itemId" params={{ itemId: it.id }} data-testid="item-card"
        className="tv-focus group flex items-center gap-4 rounded-[6px] px-2 py-3 transition-colors hover:bg-white/[0.04] sm:gap-6">
        <div className="relative aspect-[2/3] w-14 shrink-0 overflow-hidden rounded-[4px] bg-surface-2 sm:w-16"><PortraitArt item={it} /></div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-[15px] font-semibold">{it.title}</p>
          <p className="mt-0.5 truncate text-[13px] text-text/55 tabular">
            {[it.year, it.durationMs ? formatDuration(it.durationMs) : null, it.rating ? `★ ${it.rating.toFixed(1)}` : null].filter(Boolean).join(" · ")}
          </p>
        </div>
        {it.played && <span className="hidden text-[13px] text-text/55 sm:block">{t("library.watched")}</span>}
        {progressOf(it) > 0 && <ProgressBar value={progressOf(it)} className="hidden w-28 sm:block" />}
      </Link>
    </li>
  );
}

export function LibraryPage() {
  const { t } = useTranslation();
  const enh = useEnhancements();
  const surprise = useSurprise();
  const { libraryId } = useParams({ from: "/app/library/$libraryId" });
  const libraries = useLibraries();
  const lib = libraries.data?.find((l) => l.id === libraryId);
  const [tab, setTab] = useState<LibTab>("all");
  const [sort, setSort] = useState<"title" | "year" | "added" | "rating">("added");
  const [genre, setGenre] = useState("");
  const [status, setStatus] = useState<Status>("");
  const [decade, setDecade] = useState("");
  const [letter, setLetter] = useState("");
  const [view, setView] = useState<"posters" | "list">(() => {
    try { return localStorage.getItem("library-view") === "list" ? "list" : "posters"; } catch { return "posters"; }
  });
  useEffect(() => { try { localStorage.setItem("library-view", view); } catch { /* private mode */ } }, [view]);
  const genres = useQuery({
    queryKey: ["genres", libraryId],
    queryFn: async () => unwrap(await api.GET("/libraries/{libraryId}/genres", { params: { path: { libraryId } } })),
  });
  const effectiveStatus: Status = tab === "favorites" ? "favorite" : status;
  const yearFrom = decade ? Number(decade) : undefined;
  const yearTo = decade ? Number(decade) + 9 : undefined;
  const items = useInfiniteQuery({
    queryKey: ["library-items", libraryId, sort, genre, effectiveStatus, decade, letter],
    enabled: tab === "all" || tab === "favorites",
    initialPageParam: 0,
    queryFn: async ({ pageParam }) =>
      unwrap(await api.GET("/libraries/{libraryId}/items", {
        params: { path: { libraryId }, query: {
          limit: PAGE, offset: pageParam, sort, genre: genre || undefined, letter: letter || undefined, yearFrom, yearTo,
          unplayed: effectiveStatus === "unplayed" || undefined, resumable: effectiveStatus === "resumable" || undefined,
          favorite: effectiveStatus === "favorite" || undefined,
        } },
      })),
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
  });
  const all = items.data?.pages.flatMap((p) => p.items) ?? [];
  const total = items.data?.pages[0]?.total;
  const [pool, setPool] = useState<ItemSummary[]>();
  const [libTotal, setLibTotal] = useState<number>();
  // The stage keeps the newest titles even while filters change the grid.
  useEffect(() => {
    if (!pool && items.data) { setPool(all.slice(0, 10)); setLibTotal(total); }
  }, [all, pool, items.data, total]);
  const cols = useCollections();
  const collections = Array.isArray(cols.data) ? cols.data : [];
  const isMusic = lib?.kind === "music";
  const filtered = !!(genre || status || decade || letter);
  const tabs: { id: LibTab; label: string }[] = [
    { id: "all", label: t("library.tabAll") },
    { id: "suggested", label: t("library.tabSuggested") },
    { id: "genres", label: t("library.genres") },
    { id: "collections", label: t("collections.title") },
    { id: "favorites", label: t("nav.myList") },
  ];
  const tabClass = "relative h-11 shrink-0 px-1 text-[15px] font-medium text-text/60 transition-colors hover:text-text after:absolute after:inset-x-0 after:bottom-0 after:h-[2px] after:scale-x-0 after:bg-[var(--amb-a)] after:transition-transform aria-selected:text-text aria-selected:after:scale-x-100";
  const decades = [2020, 2010, 2000, 1990, 1980, 1970, 1960, 1950];

  if (isMusic && lib) return <MusicLibrary library={lib} />;
  if (lib && (lib.kind === "books" || lib.kind === "audiobooks")) return <BooksLibrary key={lib.id} library={lib} />;
  if (lib?.kind === "photos") return <PhotosLibrary key={lib.id} library={lib} />;
  return (
    <div className="flex flex-col gap-6">
      <Billboard pool={pool} />
      <div className={`sticky top-0 z-30 border-b border-border bg-bg/88 pt-[4.5rem] backdrop-blur-xl md:pt-20 ${gutter}`}>
        <div className="flex flex-wrap items-end gap-x-5 gap-y-2">
          <h1 className="font-display text-4xl sm:text-5xl">{lib?.name ?? "…"}</h1>
          {libTotal !== undefined && <span className="pb-1 text-sm text-text/55 tabular">{t("libraries.items", { count: libTotal })}</span>}
        </div>
        <div className="mt-3 -mb-px flex items-center gap-4">
          <div role="tablist" aria-label={lib?.name} className="shelf-track flex min-w-0 flex-1 gap-6 overflow-x-auto">
            {tabs.map((x) => (
              <button key={x.id} type="button" role="tab" aria-selected={tab === x.id} onClick={() => setTab(x.id)} className={tabClass}>{x.label}</button>
            ))}
          </div>
          {enh.randomButton && (
            <button type="button" onClick={() => void surprise([libraryId])} className={`${pressable} h-9 shrink-0 rounded-[6px] px-3 text-sm text-text/75 hover:bg-white/10 hover:text-text`}>
              <Shuffle className="size-4" aria-hidden /><span className="max-sm:sr-only">{t("home.surprise")}</span>
            </button>
          )}
        </div>
      </div>

      {(tab === "all" || tab === "favorites") && (
        <div className={`flex flex-col gap-5 ${gutter}`}>
          <div className="flex flex-wrap items-center gap-2" role="group" aria-label={t("library.filters")}>
            {(genres.data?.length ?? 0) > 0 && (
              <Select value={genre} onChange={(e) => setGenre(e.target.value)} aria-label={t("library.genre")}>
                <option value="">{t("library.allGenres")}</option>
                {genres.data?.map((g) => <option key={g} value={g}>{g}</option>)}
              </Select>
            )}
            {tab === "all" && (
              <Select value={status} onChange={(e) => setStatus(e.target.value as Status)} aria-label={t("library.status")}>
                <option value="">{t("library.statusAll")}</option>
                <option value="unplayed">{t("library.unplayedOnly")}</option>
                <option value="resumable">{t("library.statusResumable")}</option>
                <option value="favorite">{t("nav.myList")}</option>
              </Select>
            )}
            <Select value={decade} onChange={(e) => setDecade(e.target.value)} aria-label={t("library.decade")}>
              <option value="">{t("library.allYears")}</option>
              {decades.map((d) => <option key={d} value={d}>{t("library.decadeLabel", { decade: d })}</option>)}
            </Select>
            <Select value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} aria-label={t("library.sort")}>
              <option value="added">{t("library.sortAdded")}</option>
              <option value="title">{t("library.sortTitle")}</option>
              <option value="year">{t("library.sortYear")}</option>
              <option value="rating">{t("library.sortRating")}</option>
            </Select>
            {filtered && (
              <button type="button" onClick={() => { setGenre(""); setStatus(""); setDecade(""); setLetter(""); }}
                className={`${pressable} h-9 rounded-[6px] px-3 text-sm text-text/70 hover:text-text`}>
                <X className="size-4" aria-hidden />{t("library.clearFilters")}
              </button>
            )}
            {filtered && total !== undefined && <span className="text-sm text-text/50 tabular sm:ml-2">{t("library.matches", { count: total })}</span>}
            <div className="ml-auto flex gap-1" role="group" aria-label={t("library.view")}>
              <IconButton label={t("library.viewPosters")} active={view === "posters"} onClick={() => setView("posters")}><LayoutGrid className="size-4" aria-hidden /></IconButton>
              <IconButton label={t("library.viewList")} active={view === "list"} onClick={() => setView("list")}><List className="size-4" aria-hidden /></IconButton>
            </div>
          </div>
          <nav aria-label={t("library.jumpTo")} className="shelf-track -mt-1 flex gap-0.5 overflow-x-auto">
            {LETTERS.map((l) => (
              <button key={l} type="button" aria-pressed={letter === l} onClick={() => setLetter(letter === l ? "" : l)}
                className="grid h-8 min-w-8 place-items-center rounded-[4px] text-[13px] font-semibold text-text/50 transition-colors hover:bg-white/[0.06] hover:text-text aria-pressed:bg-text aria-pressed:text-bg">
                {l}
              </button>
            ))}
          </nav>
          <ErrorText error={items.error} />
          {items.isPending && <div className="flex justify-center py-20"><Spinner /></div>}
          {total === 0 && (
            <p className="py-16 text-center text-sm text-muted">
              {tab === "favorites" ? t("library.myListEmpty") : filtered ? t("library.noMatches") : lib && ["queued", "scanning"].includes(lib.scan.state) ? t("library.scanning") : t("library.empty")}
            </p>
          )}
          {view === "posters" ? (
            <div className="grid grid-cols-2 gap-x-5 gap-y-8 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
              {all.map((it) => <ItemCard key={it.id} item={it} />)}
            </div>
          ) : (
            <ul className="flex flex-col border-t border-border">{all.map((it) => <ListRow key={it.id} it={it} />)}</ul>
          )}
          {items.hasNextPage && (
            <Button variant="secondary" className="self-center" disabled={items.isFetchingNextPage} onClick={() => void items.fetchNextPage()}>
              {items.isFetchingNextPage && <Spinner className="size-4" />}{t("library.more")}
            </Button>
          )}
        </div>
      )}
      {tab === "suggested" && lib && <LibrarySuggestions library={lib} />}
      {tab === "genres" && lib && <GenreGrid library={lib} onPick={(g) => { setGenre(g); setTab("all"); }} />}
      {tab === "collections" && (
        <div className={gutter}>
          {collections.length === 0
            ? <p className="py-16 text-center text-sm text-muted">{t("collections.empty")}</p>
            : <div className="grid grid-cols-2 gap-x-5 gap-y-8 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
                {collections.map((c) => <CollectionCard key={c.id} c={c} />)}
              </div>}
        </div>
      )}
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

const circle = "tv-focus grid size-13 shrink-0 place-items-center rounded-[6px] bg-white/[0.12] text-text backdrop-blur-xl transition-[background-color,transform] duration-150 hover:bg-white/[0.2] active:scale-95 disabled:opacity-50";

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
      <button type="button" className={`${circle} ${item.played ? "!bg-text !text-bg" : ""}`}
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
        className="tv-focus group flex items-center gap-4 rounded-[6px] px-2 py-4 transition-colors hover:bg-white/[0.04] sm:gap-6 sm:px-4">
        <span className="hidden w-10 shrink-0 text-center font-display text-3xl text-text/45 tabular sm:block">{c.episodeNumber ?? ""}</span>
        <div className="relative aspect-video w-32 shrink-0 overflow-hidden rounded-[6px] bg-surface-2 sm:w-44">
          {c.hasPoster && !broken
            ? <Art src={imageUrl(c.id, "still")} onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover" />
            : <ArtFallback item={c} wide />}
          {playable && (
            <span className="absolute inset-0 grid place-items-center bg-black/30 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
              <span className="grid size-10 place-items-center rounded-[6px] bg-text text-bg"><Play className="ml-0.5 size-4 fill-current" aria-hidden /></span>
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
      <h2 className="font-head text-xl sm:text-2xl">{t("library.moreLikeThis")}</h2>
      <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
        {more.data.map((it) => <TitleCard key={it.id} item={it} />)}
      </div>
    </section>
  );
}

type Trick = NonNullable<ItemDetail["trickplay"]>;

/** One frame of the trickplay sheets at a time, drawn as a sprite. */
function TrickFrame({ tp, ms, className = "" }: { tp: Trick; ms: number; className?: string }) {
  const idx = Math.min(tp.count - 1, Math.floor(ms / tp.intervalMs));
  const perSheet = tp.columns * tp.rows;
  const sheet = tp.sheetUrls[Math.floor(idx / perSheet)];
  if (!sheet) return null;
  const local = idx % perSheet;
  const col = local % tp.columns;
  const row = Math.floor(local / tp.columns);
  return (
    <div aria-hidden className={`bg-no-repeat ${className}`} style={{
      backgroundImage: `url(${sheet})`,
      backgroundSize: `${tp.columns * 100}% ${tp.rows * 100}%`,
      backgroundPosition: `${tp.columns > 1 ? (col / (tp.columns - 1)) * 100 : 0}% ${tp.rows > 1 ? (row / (tp.rows - 1)) * 100 : 0}%`,
    }} />
  );
}

/** A representative moment of a chapter: a little way in, past fades and titles. */
function frameAt(start: number, next?: number): number {
  const len = next ? next - start : 60_000;
  return start + Math.min(20_000, Math.max(1_000, len / 3));
}

/** Chapters with a frame from each, starting playback at that point. */
function Chapters({ d }: { d: ItemDetail }) {
  const { t } = useTranslation();
  const chapters = d.chapters ?? [];
  if (chapters.length < 2) return null;
  const tp = d.trickplay;
  return (
    <section className="flex flex-col gap-4" data-testid="chapters">
      <h2 className={`font-head text-xl sm:text-2xl ${gutter}`}>{t("library.chapters")}</h2>
      <ol className={`shelf-track flex gap-4 overflow-x-auto pb-4 ${gutter}`}>
        {chapters.map((c, i) => (
          <li key={c.startMs} className="w-[62vw] shrink-0 sm:w-[260px]">
            <Link to="/watch/$itemId" params={{ itemId: d.item.id }} search={{ start: c.startMs }}
              className="tv-focus group flex flex-col gap-2 rounded-[6px] outline-none">
              <div className="poster-lift relative aspect-video overflow-hidden rounded-[6px] bg-surface-2">
                {tp ? <TrickFrame tp={tp} ms={frameAt(c.startMs, chapters[i + 1]?.startMs)} className="absolute inset-0" /> : <ArtFallback item={d.item} wide />}
                <span className="absolute bottom-2 left-2 rounded-[3px] bg-black/75 px-1.5 py-0.5 text-xs font-semibold tabular">{formatClock(c.startMs)}</span>
              </div>
              <p className="truncate text-sm text-text/85 group-hover:text-text"><span className="mr-2 text-text/45 tabular">{i + 1}</span>{c.title}</p>
            </Link>
          </li>
        ))}
      </ol>
    </section>
  );
}

/** Links to the public databases the title is known by (and to Radarr/Sonarr for admins). */
function ExternalLinks({ d }: { d: ItemDetail }) {
  const ids = d.providerIds ?? {};
  const enh = useEnhancements();
  const isAdmin = !!useMe().data?.isAdmin;
  const arr = useQuery({
    queryKey: ["arr-link", d.item.id],
    enabled: enh.arrLinks && isAdmin && (d.item.kind === "movie" || d.item.kind === "series"),
    retry: false,
    queryFn: async () => (await api.GET("/items/{itemId}/arr-link", { params: { path: { itemId: d.item.id } } })).data ?? null,
  });
  const kind = d.item.kind === "series" ? "tv" : "movie";
  const links = [
    ids.imdb && { label: "IMDb", href: `https://www.imdb.com/title/${ids.imdb}/` },
    ids.tmdb && (d.item.kind === "movie" || d.item.kind === "series") && { label: "TMDb", href: `https://www.themoviedb.org/${kind}/${ids.tmdb}` },
    ids.tvdb && { label: "TheTVDB", href: `https://thetvdb.com/dereferrer/series/${ids.tvdb}` },
    arr.data && { label: arr.data.app === "radarr" ? "Radarr" : "Sonarr", href: arr.data.url },
  ].filter(Boolean) as { label: string; href: string }[];
  if (!links.length) return null;
  return (
    <div className="flex flex-wrap gap-2">
      {links.map((l) => (
        <a key={l.label} href={l.href} target="_blank" rel="noreferrer noopener"
          className={`${pressable} h-8 rounded-[4px] px-3 text-[13px] font-semibold text-text/80 ring-1 ring-inset ring-border hover:text-text hover:ring-border-strong`}>
          {l.label}
        </a>
      ))}
    </div>
  );
}

/** Choose the version, audio track and subtitles before playing, like on a disc menu. */
function PlayOptions({ d, value, onChange }: {
  d: ItemDetail; value: { file: number; audio?: number; sub?: number }; onChange: (v: { file: number; audio?: number; sub?: number }) => void;
}) {
  const { t, i18n } = useTranslation();
  const f = d.files[value.file];
  if (!f) return null;
  const names = new Intl.DisplayNames([i18n.resolvedLanguage ?? "en"], { type: "language" });
  const lang = (code?: string) => {
    if (!code) return t("library.unknownLanguage");
    try { return names.of(code.length === 3 ? ({ eng: "en", hrv: "hr", ger: "de", deu: "de", fre: "fr", fra: "fr", spa: "es", ita: "it", dut: "nl", nld: "nl", pol: "pl", por: "pt", rus: "ru", jpn: "ja", srp: "sr", bos: "bs", slv: "sl", vie: "vi", chi: "zh", zho: "zh" } as Record<string, string>)[code] ?? code : code) ?? code; } catch { return code; }
  };
  const audio = f.audio ?? [];
  const subs = f.subtitles ?? [];
  if (d.files.length < 2 && audio.length < 2 && subs.length === 0) return null;
  const sel = "h-10 min-w-0 max-w-[16rem] rounded-[6px] border-white/15 bg-black/45 text-sm backdrop-blur";
  return (
    <div className="flex flex-wrap items-end gap-3" data-testid="play-options">
      {d.files.length > 1 && (
        <label className="flex flex-col gap-1 text-[12px] font-semibold tracking-wide text-text/55 uppercase">
          {t("library.version")}
          <Select className={sel} value={value.file} onChange={(e) => onChange({ file: Number(e.target.value) })}>
            {d.files.map((x, i) => <option key={x.id} value={i}>{x.label || [resolutionLabel(x.width, x.height), x.hdr, x.videoCodec?.toUpperCase()].filter(Boolean).join(" ")}</option>)}
          </Select>
        </label>
      )}
      {audio.length > 1 && (
        <label className="flex flex-col gap-1 text-[12px] font-semibold tracking-wide text-text/55 uppercase">
          {t("library.audio")}
          <Select className={sel} value={value.audio ?? ""} onChange={(e) => onChange({ ...value, audio: e.target.value === "" ? undefined : Number(e.target.value) })}>
            <option value="">{t("library.automatic")}</option>
            {audio.map((a, i) => <option key={i} value={i}>{[lang(a.language), a.codec.toUpperCase(), channelsLabel(a.channels), a.title].filter(Boolean).join(" · ")}</option>)}
          </Select>
        </label>
      )}
      {subs.length > 0 && (
        <label className="flex flex-col gap-1 text-[12px] font-semibold tracking-wide text-text/55 uppercase">
          {t("library.subtitles")}
          <Select className={sel} value={value.sub ?? ""} onChange={(e) => onChange({ ...value, sub: e.target.value === "" ? undefined : Number(e.target.value) })}>
            <option value="">{t("library.automatic")}</option>
            <option value={-1}>{t("player.off")}</option>
            {subs.map((x, i) => <option key={i} value={i}>{[lang(x.language), x.forced ? t("library.forced") : "", x.external ? t("library.external") : ""].filter(Boolean).join(" · ")}</option>)}
          </Select>
        </label>
      )}
    </div>
  );
}

/** One label/value pair in the details column. */
function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="font-label text-text/50">{label}</dt>
      <dd className="text-text/90">{children}</dd>
    </div>
  );
}

export function ItemPage() {
  const { t, i18n } = useTranslation();
  const isAdmin = !!useMe().data?.isAdmin;
  const { itemId } = useParams({ from: "/app/item/$itemId" });
  const detail = useItemDetail(itemId);
  const d = detail.data;
  const seasons = d?.children.filter((c) => c.kind === "season") ?? [];
  const [season, setSeason] = useState("");
  const [choice, setChoice] = useState<{ file: number; audio?: number; sub?: number }>({ file: 0 });
  const seasonIds = seasons.map((s) => s.id).join();
  useEffect(() => {
    if (seasons.length && !seasons.some((s) => s.id === season)) {
      setSeason((seasons.find((s) => s.seasonNumber === 1) ?? seasons[0]!).id);
    }
  }, [seasonIds]); // eslint-disable-line react-hooks/exhaustive-deps

  useAmbientLight(detail.data?.item.colors);
  if (detail.error) return <div className={`pt-24 ${gutter}`}><ErrorText error={detail.error} /></div>;
  if (!d) return <div role="status" className="flex min-h-[70vh] items-center justify-center"><Spinner /></div>;
  if (d.item.kind === "album") return <AlbumView d={d} />;
  if (d.item.kind === "artist") return <ArtistView d={d} />;
  if (d.item.kind === "book" || d.item.kind === "audiobook") return <BookView key={d.item.id} d={d} />;
  if (d.item.kind === "photoalbum") return <PhotoAlbumView key={d.item.id} d={d} />;
  if (d.item.kind === "photo" || d.item.kind === "video") return <PhotoItemView key={d.item.id} d={d} />;
  const it = { ...d.item, hasBackdrop: d.hasBackdrop };
  const isEpisode = it.kind === "episode";
  const directors = d.crew.filter((p) => p.role === "Director").map((p) => p.name);
  const writers = d.crew.filter((p) => p.role !== "Director").map((p) => p.name);
  const rows = d.children.filter((c) => c.kind !== "season");
  const title = isEpisode ? `${episodeLabel(it)} · ${it.title}` : it.title;
  const playSearch = {
    ...(d.files.length > 1 && d.files[choice.file] ? { file: d.files[choice.file]!.id } : {}),
    ...(choice.audio !== undefined ? { audio: choice.audio } : {}),
    ...(choice.sub !== undefined ? { sub: choice.sub } : {}),
  };

  return (
    <div className="flex flex-col gap-14 pb-6">
      <Screen item={it} detail={d} still={isEpisode} className="min-h-[78svh] pt-[44svh] md:min-h-[min(92svh,900px)] md:pt-32" testId="item-hero">
        <div className={`pb-4 ${gutter}`}><div className="flex max-w-[52rem] min-w-0 flex-col gap-5">
          {isEpisode && it.seriesTitle && (
            <Link to="/item/$itemId" params={{ itemId: it.seriesId! }} className="enter font-head text-lg text-text/75 hover:text-text">{it.seriesTitle}</Link>
          )}
          <h1 className="enter">
            {isEpisode
              ? <span className="block font-display text-[clamp(2.4rem,5.5vw,5rem)] text-balance">{title}</span>
              : <TitleArt item={{ ...it, hasLogo: d.hasLogo }} className="max-h-[22vh] w-auto max-w-[min(34rem,82vw)]"
                  textClass="block font-display text-[clamp(2.9rem,7vw,6.6rem)] text-balance" />}
          </h1>
          {d.originalTitle && d.originalTitle !== it.title && <p className="-mt-2 text-sm text-text/55">{d.originalTitle}</p>}
          <Facts item={it} detail={d} className="enter-2" />
          {d.tagline && <p className="enter-2 text-lg font-medium text-text/80 italic">{d.tagline}</p>}
          <div className="enter-3 mt-2 flex flex-wrap items-center gap-3">
            {d.files.length > 0 && (
              <Link to="/watch/$itemId" params={{ itemId: it.id }} data-testid="play" search={playSearch}
                className={`${pressable} ${buttonVariants.primary} ${bigButton} max-sm:w-full`}>
                <Play className="size-5 fill-current" aria-hidden />
                {it.positionMs ? t("player.resume", { time: formatClock(it.positionMs) }) : t("player.play")}
              </Link>
            )}
            {d.files.length > 0 && it.positionMs ? (
              <Link to="/watch/$itemId" params={{ itemId: it.id }} search={{ ...playSearch, start: 0 }}
                className={`${pressable} ${buttonVariants.glass} ${bigButton}`}>
                <RotateCcw className="size-5" aria-hidden />{t("player.fromStart")}
              </Link>
            ) : null}
            {d.files.length > 0 && (it.kind === "movie" || it.kind === "episode") && <WatchTogetherButton itemId={it.id} />}
            <UserDataButtons item={it} />
            {it.kind !== "season" && <AddToMenu item={it} />}
            {isAdmin && <MetadataMenu d={d} />}
          </div>
          <PlayOptions d={d} value={choice} onChange={setChoice} />
          <Watched item={it} />
        </div></div>
      </Screen>

      <section className={`grid gap-10 md:grid-cols-[minmax(0,13rem)_minmax(0,1fr)] lg:grid-cols-[minmax(0,15rem)_minmax(0,1fr)_minmax(0,20rem)] lg:gap-14 ${gutter}`}>
        {!isEpisode && (
          <div className="hidden md:block">
            <div className="relative aspect-[2/3] overflow-hidden rounded-[6px] bg-surface-2 shadow-[0_30px_60px_-30px_rgb(0_0_0/0.9)]">
              <PortraitArt item={it} />
            </div>
          </div>
        )}
        <div className={`flex min-w-0 flex-col gap-3 ${isEpisode ? "md:col-span-2" : ""}`}>
          <h2 className="font-head text-xl">{t("library.story")}</h2>
          {d.overview
            ? <p className="max-w-[64ch] text-[17px] leading-[1.65] text-text/85" data-testid="overview">{d.overview}</p>
            : <p className="text-text/55">{t("library.noOverview")}</p>}
          {d.metadataError && <p className="text-xs text-muted">{t("library.noMetadata")}</p>}
        </div>
        <dl className="flex flex-col gap-4 text-[15px] md:col-start-2 lg:col-start-auto">
          {directors.length > 0 && <Fact label={t("library.directedBy")}>{directors.join(", ")}</Fact>}
          {d.cast.length > 0 && <Fact label={t("library.starring")}>{d.cast.slice(0, 5).map((p) => p.name).join(", ")}</Fact>}
          {d.genres.length > 0 && <Fact label={t("library.genres")}>{d.genres.join(", ")}</Fact>}
          {writers.length > 0 && <Fact label={t("library.writtenBy")}>{writers.join(", ")}</Fact>}
          {(d.studios?.length ?? 0) > 0 && <Fact label={t("library.studios")}>{d.studios!.join(", ")}</Fact>}
          {d.premiereDate && <Fact label={t("library.released")}>{new Date(d.premiereDate).toLocaleDateString(i18n.resolvedLanguage === "hr" ? "hr-HR" : "en-GB", { day: "numeric", month: "long", year: "numeric" })}</Fact>}
          {(d.collections?.length ?? 0) > 0 && (
            <Fact label={t("collections.partOf")}>{d.collections!.map((c, i) => (
              <span key={c.id}>{i > 0 && ", "}<Link to="/collection/$collectionId" params={{ collectionId: c.id }} className="underline underline-offset-4 decoration-text/30 hover:decoration-text">{c.name}</Link></span>
            ))}</Fact>
          )}
          <ExternalLinks d={d} />
        </dl>
      </section>

      <Chapters d={d} />

      {seasons.length > 0 && (
        <section className={`flex flex-col gap-4 ${gutter}`} data-testid="seasons">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="font-head text-xl sm:text-2xl">{t("library.episodes")}</h2>
            <Select value={season} onChange={(e) => setSeason(e.target.value)} aria-label={t("library.selectSeason")}
              className="h-11 border-border-strong bg-surface-2 px-4 text-base font-semibold">
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
          <h2 className="font-head text-xl sm:text-2xl">{t("library.episodes")}</h2>
          <ol className="flex flex-col border-t border-white/10">
            {rows.map((c) => <EpisodeRow key={c.id} item={c} />)}
          </ol>
        </section>
      )}

      {d.cast.length > 0 && (
        <section className="flex flex-col gap-4">
          <h2 className={`font-head text-xl sm:text-2xl ${gutter}`}>{t("library.cast")}</h2>
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
          <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="font-head text-xl sm:text-2xl">
            {t("library.mediaInfo")}
            <span className="ml-2 text-sm font-normal text-muted">{d.files.length > 1 ? t("library.versions", { count: d.files.length }) : t("library.file")}</span>
          </h2>
            {(it.kind === "movie" || it.kind === "episode") && <SubtitleFinder itemId={it.id} />}
          </div>
          <ul className="grid gap-3 lg:grid-cols-2">
            {d.files.map((f) => (
              <li key={f.id} className="flex min-w-0 flex-col gap-3 rounded-[6px] border border-border bg-surface p-4 text-sm" data-testid="media-file">
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
                {(it.kind === "movie" || it.kind === "episode") && (
                  <div><DownloadOriginalButton itemId={it.id} fileId={f.id} /></div>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
