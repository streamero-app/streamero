import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, Outlet, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Building2, Clapperboard, Film, Heart, House, Images, Lock, Play, RotateCcw, Search, Settings2, Tag, UserRound, X } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Badge, Button, ErrorText, Select, Spinner, TextField } from "@/components/ui";
import { canAdminister, useMe } from "@/features/auth/useAuth";
import { formatDuration, formatSize } from "@/features/library/format";
import {
  adultImage, fetchScenes, formatDate, isLocked, PAGE, sceneProgress, statusKey, useAdultStatus,
  type AdultPerformer, type AdultScene, type AdultStudio, type SceneQuery,
} from "./api";
import { BlurArt, Breadcrumbs, Monogram, SearchField, useBlur } from "./AdultCommon";
import { GalleryGrid } from "./AdultGalleries";

export function SceneCard({ scene }: { scene: AdultScene }) {
  const { t, i18n } = useTranslation();
  const blur = useBlur();
  const progress = sceneProgress(scene);
  return (
    <Link to="/adult/scene/$sceneId" params={{ sceneId: scene.id }} data-testid="adult-scene-card"
      className="group tv-focus flex flex-col gap-2 rounded-[6px]">
      <div className="relative aspect-video overflow-hidden rounded-[6px] bg-surface ring-1 ring-border transition-transform duration-200 group-hover:-translate-y-0.5">
        <BlurArt blur={blur} src={scene.hasPoster ? adultImage("scene", scene.id, "poster") : scene.hasBackdrop ? adultImage("scene", scene.id, "backdrop") : undefined}
          fallback={<Monogram name={scene.studio?.name ?? scene.title} />} />
        {scene.durationMs ? (
          <span className="pointer-events-none absolute right-1.5 bottom-1.5 rounded bg-black/70 px-1.5 py-0.5 text-[11px] font-semibold text-white tabular">
            {formatDuration(scene.durationMs, true)}
          </span>
        ) : null}
        {scene.played && (
          <span className="pointer-events-none absolute top-1.5 right-1.5 rounded bg-black/70 px-1.5 py-0.5 text-[11px] font-semibold text-white">{t("adult.watched")}</span>
        )}
        {progress > 0 && (
          <div className="pointer-events-none absolute inset-x-0 bottom-0 h-[3px] bg-white/25"><div className="h-full bg-accent" style={{ width: `${progress}%` }} /></div>
        )}
      </div>
      <div className="min-w-0 px-0.5">
        <p className="line-clamp-2 text-sm font-semibold leading-snug text-text">{scene.title}</p>
        <p className="mt-0.5 truncate text-xs text-muted">
          {[scene.studio?.name, scene.kind === "movie" ? scene.year : formatDate(scene.date, i18n.language)].filter(Boolean).join(" · ")}
        </p>
        {scene.performers.length > 0 && <p className="truncate text-xs text-text/60">{scene.performers.map((p) => p.name).join(", ")}</p>}
      </div>
    </Link>
  );
}

/** A grid of scenes with "load more". */
export function SceneGrid({ query, empty }: { query: SceneQuery; empty?: ReactNode }) {
  const { t } = useTranslation();
  const scenes = useInfiniteQuery({
    queryKey: ["adult", "scenes", query],
    queryFn: ({ pageParam }) => fetchScenes(query, pageParam),
    initialPageParam: 0,
    getNextPageParam: (last, all) => {
      const loaded = all.reduce((n, p) => n + p.items.length, 0);
      return loaded < last.total && last.items.length === PAGE ? loaded : undefined;
    },
  });
  const items = scenes.data?.pages.flatMap((p) => p.items) ?? [];
  if (scenes.isPending) return <div className="py-16 text-center"><Spinner /></div>;
  if (scenes.error) return <ErrorText error={scenes.error} />;
  if (items.length === 0) return <p className="py-16 text-center text-muted">{empty ?? t("adult.noScenes")}</p>;
  return (
    <>
      <p className="mb-3 text-xs text-muted tabular">{t("adult.sceneCount", { count: scenes.data?.pages[0]?.total ?? 0 })}</p>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,15rem),1fr))] gap-x-4 gap-y-6">
        {items.map((s) => <SceneCard key={s.id} scene={s} />)}
      </div>
      {scenes.hasNextPage && (
        <div className="mt-8 flex justify-center">
          <Button variant="secondary" disabled={scenes.isFetchingNextPage} onClick={() => void scenes.fetchNextPage()}>{t("adult.more")}</Button>
        </div>
      )}
    </>
  );
}

export type ScenesSearch = { q?: string; status?: "unwatched" | "inProgress" | "watched"; sort?: "oldest" | "added" | "title" | "played" | "duration"; tag?: string };

/** Scenes or movies with search, filters and sort (kept in the address). */
function ScenesBrowser({ kind }: { kind: "scene" | "movie" }) {
  const { t } = useTranslation();
  const search = useSearch({ strict: false }) as ScenesSearch;
  const navigate = useNavigate();
  const set = (patch: Partial<ScenesSearch>) => void navigate({ to: ".", search: ((s: ScenesSearch) => ({ ...s, ...patch })) as never, replace: true });
  return (
    <section>
      <div className="mb-5 flex flex-wrap items-center gap-2">
        <SearchField value={search.q ?? ""} onChange={(q) => set({ q: q || undefined })} label={t("adult.searchScenes")} />
        <Select aria-label={t("adult.status")} value={search.status ?? ""} onChange={(e) => set({ status: (e.target.value || undefined) as ScenesSearch["status"] })}>
          <option value="">{t("adult.statusAll")}</option>
          <option value="unwatched">{t("adult.statusUnwatched")}</option>
          <option value="inProgress">{t("adult.statusInProgress")}</option>
          <option value="watched">{t("adult.statusWatched")}</option>
        </Select>
        <Select aria-label={t("adult.sort")} value={search.sort ?? "date"} onChange={(e) => set({ sort: e.target.value === "date" ? undefined : e.target.value as ScenesSearch["sort"] })}>
          <option value="date">{t("adult.sortDate")}</option>
          <option value="oldest">{t("adult.sortOldest")}</option>
          <option value="added">{t("adult.sortAdded")}</option>
          <option value="title">{t("adult.sortTitle")}</option>
          <option value="duration">{t("adult.sortDuration")}</option>
          <option value="played">{t("adult.sortPlayed")}</option>
        </Select>
        {search.tag && (
          <button type="button" onClick={() => set({ tag: undefined })} className="inline-flex h-9 items-center gap-1.5 rounded-[6px] bg-white/[0.08] px-3 text-sm">
            #{search.tag}<X className="size-3.5" aria-label={t("adult.clearTag")} />
          </button>
        )}
      </div>
      <SceneGrid query={{ kind, q: search.q, status: search.status, sort: search.sort, tag: search.tag }}
        empty={kind === "movie" ? t("adult.noMovies") : undefined} />
    </section>
  );
}

export function AdultScenesPage() {
  return <ScenesBrowser kind="scene" />;
}

export function AdultMoviesPage() {
  return <ScenesBrowser kind="movie" />;
}

export function NamedTile({ to, id, name, count, galleries = 0, image, round, blur, favorite }: {
  to: "/adult/studio/$studioId" | "/adult/performer/$performerId"; id: string; name: string; count: number; galleries?: number; image?: string;
  round?: boolean; blur: boolean; favorite?: boolean;
}) {
  const { t } = useTranslation();
  const params = to === "/adult/studio/$studioId" ? { studioId: id } : { performerId: id };
  return (
    <Link to={to} params={params as never} className="group tv-focus flex flex-col items-center gap-2 rounded-[6px] text-center" data-testid="adult-tile">
      <div className={`relative w-full overflow-hidden bg-surface ring-1 ring-border transition-transform duration-200 group-hover:-translate-y-0.5 ${round ? "aspect-square rounded-full" : "aspect-[16/9] rounded-[6px]"}`}>
        <BlurArt blur={blur} src={image} className={round ? "rounded-full" : ""} fallback={<Monogram name={name} round={round} />} />
      </div>
      <div className="min-w-0">
        <p className="flex items-center justify-center gap-1 truncate text-sm font-semibold">
          {favorite && <Heart className="size-3.5 shrink-0 fill-current text-accent" aria-label={t("adult.favorite")} />}{name}
        </p>
        <p className="text-xs text-muted tabular">
          {[count > 0 && t("adult.sceneCount", { count }), galleries > 0 && t("adult.galleryCount", { count: galleries })].filter(Boolean).join(" · ")}
        </p>
      </div>
    </Link>
  );
}

function NamedList<T extends { id: string; name: string; sceneCount: number; galleryCount: number; hasImage: boolean; favorite?: boolean }>({ kind }: { kind: "studios" | "performers" }) {
  const { t } = useTranslation();
  const blur = useBlur();
  const [q, setQ] = useState("");
  const [sort, setSort] = useState<"count" | "name">("count");
  const [favorites, setFavorites] = useState(false);
  const list = useInfiniteQuery({
    queryKey: ["adult", kind, q, sort, favorites],
    queryFn: async ({ pageParam }) => {
      const query = { q: q || undefined, offset: pageParam, limit: 120, sort, ...(kind === "performers" && favorites ? { favorites: true } : {}) };
      return (kind === "studios"
        ? unwrap(await api.GET("/adult/studios", { params: { query } }))
        : unwrap(await api.GET("/adult/performers", { params: { query } }))) as { items: T[]; total: number };
    },
    initialPageParam: 0,
    getNextPageParam: (last, all) => {
      const loaded = all.reduce((n, p) => n + p.items.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section>
      <div className="mb-5 flex flex-wrap items-center gap-2">
        <SearchField value={q} onChange={setQ} label={t(kind === "studios" ? "adult.searchStudios" : "adult.searchPerformers")} />
        <Select aria-label={t("adult.sort")} value={sort} onChange={(e) => setSort(e.target.value as "count" | "name")}>
          <option value="count">{t("adult.sortCount")}</option>
          <option value="name">{t("adult.sortName")}</option>
        </Select>
        {kind === "performers" && (
          <button type="button" aria-pressed={favorites} onClick={() => setFavorites(!favorites)} data-testid="adult-favorites-only"
            className={`inline-flex h-9 items-center gap-1.5 rounded-[6px] px-3 text-sm ring-1 ring-inset ring-border ${favorites ? "bg-white/[0.14] text-text" : "bg-white/[0.04] text-text/75 hover:bg-white/[0.1]"}`}>
            <Heart className={`size-4 ${favorites ? "fill-current text-accent" : ""}`} aria-hidden />{t("adult.favoritesOnly")}
          </button>
        )}
      </div>
      {list.isPending ? <div className="py-16 text-center"><Spinner /></div> : <ErrorText error={list.error} />}
      {list.isSuccess && items.length === 0 && <p className="py-16 text-center text-muted">{t("adult.nothing")}</p>}
      <div className={`grid gap-x-4 gap-y-6 ${kind === "studios" ? "grid-cols-[repeat(auto-fill,minmax(min(100%,12rem),1fr))]" : "grid-cols-[repeat(auto-fill,minmax(min(100%,8.5rem),1fr))]"}`}>
        {items.map((it) => kind === "studios"
          ? <NamedTile key={it.id} to="/adult/studio/$studioId" id={it.id} name={it.name} count={it.sceneCount} galleries={it.galleryCount} blur={blur}
              image={it.hasImage ? adultImage("studio", it.id, "image") : undefined} />
          : <NamedTile key={it.id} to="/adult/performer/$performerId" id={it.id} name={it.name} count={it.sceneCount} galleries={it.galleryCount}
              round blur={blur} favorite={it.favorite} image={it.hasImage ? adultImage("performer", it.id, "image") : undefined} />)}
      </div>
      {list.hasNextPage && (
        <div className="mt-8 flex justify-center">
          <Button variant="secondary" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>{t("adult.more")}</Button>
        </div>
      )}
    </section>
  );
}

export function AdultStudiosPage() {
  return <NamedList<AdultStudio> kind="studios" />;
}

export function AdultPerformersPage() {
  return <NamedList<AdultPerformer> kind="performers" />;
}

/** Scenes and galleries tabs of a studio or performer page (kept in the address). */
function DetailTabs({ scenes, galleries, children }: { scenes: number; galleries: number; children: (tab: "scenes" | "galleries") => ReactNode }) {
  const { t } = useTranslation();
  const search = useSearch({ strict: false }) as { tab?: "scenes" | "galleries" };
  const navigate = useNavigate();
  const tab = search.tab ?? (scenes === 0 && galleries > 0 ? "galleries" : "scenes");
  const btn = (id: "scenes" | "galleries", label: string, n: number) => (
    <button type="button" role="tab" aria-selected={tab === id} data-testid={`adult-tab-${id}`}
      onClick={() => void navigate({ to: ".", search: (s: Record<string, unknown>) => ({ ...s, tab: id === "scenes" ? undefined : id }), replace: true })}
      className={`relative inline-flex h-10 items-center gap-2 px-1 text-sm font-medium transition-colors ${tab === id ? "text-text after:absolute after:inset-x-0 after:-bottom-px after:h-[2px] after:bg-text" : "text-text/55 hover:text-text"}`}>
      {label}<span className="text-xs text-muted tabular">{n}</span>
    </button>
  );
  return (
    <>
      <div role="tablist" className="flex gap-6 border-b border-border">
        {btn("scenes", t("adult.scenesAndMovies"), scenes)}
        {btn("galleries", t("adult.galleries"), galleries)}
      </div>
      {children(tab)}
    </>
  );
}

export function AdultStudioPage() {
  const { t } = useTranslation();
  const { studioId } = useParams({ from: "/app/adult/studio/$studioId" });
  const blur = useBlur();
  const studio = useQuery({ queryKey: ["adult", "studio", studioId],
    queryFn: async () => unwrap(await api.GET("/adult/studios/{studioId}", { params: { path: { studioId } } })) });
  if (studio.error) return <ErrorText error={studio.error} />;
  const s = studio.data;
  return (
    <div className="flex flex-col gap-6">
      <Breadcrumbs items={[{ label: t("adult.studios"), to: "/adult/studios" }, { label: s?.name ?? "…" }]} />
      {s && (
        <header className="flex items-center gap-5">
          <div className="relative aspect-video w-40 shrink-0 overflow-hidden rounded-[6px] bg-surface ring-1 ring-border">
            <BlurArt blur={blur} src={s.hasImage ? adultImage("studio", s.id, "image") : undefined} fallback={<Monogram name={s.name} />} />
          </div>
          <div className="min-w-0">
            <p className="text-xs font-semibold tracking-[0.14em] text-muted uppercase">{t("adult.studio")}</p>
            <h2 className="font-display text-3xl">{s.name}</h2>
            <p className="text-sm text-muted">{[s.parent, t("adult.sceneCount", { count: s.sceneCount }), s.galleryCount > 0 && t("adult.galleryCount", { count: s.galleryCount })].filter(Boolean).join(" · ")}</p>
            {s.url && <a href={s.url} target="_blank" rel="noreferrer noopener" className="text-xs text-muted underline underline-offset-4 hover:text-text">{s.url}</a>}
          </div>
        </header>
      )}
      {s && (
        <DetailTabs scenes={s.sceneCount} galleries={s.galleryCount}>
          {(tab) => tab === "scenes" ? <SceneGrid query={{ studioId }} /> : <GalleryGrid query={{ studioId }} />}
        </DetailTabs>
      )}
    </div>
  );
}

export function AdultPerformerPage() {
  const { t } = useTranslation();
  const { performerId } = useParams({ from: "/app/adult/performer/$performerId" });
  const blur = useBlur();
  const qc = useQueryClient();
  const performer = useQuery({ queryKey: ["adult", "performer", performerId],
    queryFn: async () => unwrap(await api.GET("/adult/performers/{performerId}", { params: { path: { performerId } } })) });
  const fav = useMutation({
    mutationFn: async (on: boolean) => {
      const params = { params: { path: { performerId } } };
      unwrap(on ? await api.PUT("/adult/performers/{performerId}/favorite", params) : await api.DELETE("/adult/performers/{performerId}/favorite", params));
      return on;
    },
    onSuccess: (on) => {
      qc.setQueryData(["adult", "performer", performerId], (p: AdultPerformer | undefined) => (p ? { ...p, favorite: on } : p));
      void qc.invalidateQueries({ queryKey: ["adult", "performers"] });
      void qc.invalidateQueries({ queryKey: ["adult", "home"] });
    },
  });
  if (performer.error) return <ErrorText error={performer.error} />;
  const p = performer.data;
  const facts: [string, string | undefined][] = p ? [[t("adult.gender"), p.gender], [t("adult.birthdate"), p.birthdate], [t("adult.country"), p.country]] : [];
  return (
    <div className="flex flex-col gap-6">
      <Breadcrumbs items={[{ label: t("adult.performersTab"), to: "/adult/performers" }, { label: p?.name ?? "…" }]} />
      {p && (
        <header className="flex flex-col gap-5 sm:flex-row sm:items-center">
          <div className="relative size-28 shrink-0 overflow-hidden rounded-full bg-surface ring-1 ring-border">
            <BlurArt blur={blur} className="rounded-full" src={p.hasImage ? adultImage("performer", p.id, "image") : undefined} fallback={<Monogram name={p.name} round />} />
          </div>
          <div className="min-w-0">
            <p className="text-xs font-semibold tracking-[0.14em] text-muted uppercase">{t("adult.performer")}</p>
            <div className="flex flex-wrap items-center gap-3">
              <h2 className="font-display text-3xl">{p.name}</h2>
              <button type="button" aria-pressed={p.favorite} disabled={fav.isPending} onClick={() => fav.mutate(!p.favorite)} data-testid="adult-favorite"
                className="inline-flex h-8 items-center gap-1.5 rounded-full bg-white/[0.07] px-3 text-sm ring-1 ring-inset ring-border hover:bg-white/[0.13]">
                <Heart className={`size-4 ${p.favorite ? "fill-current text-accent" : ""}`} aria-hidden />{p.favorite ? t("adult.unfavorite") : t("adult.makeFavorite")}
              </button>
            </div>
            <dl className="mt-1 flex flex-wrap gap-x-4 text-sm text-muted">
              {facts.filter(([, v]) => !!v).map(([k, v]) => <div key={k} className="flex gap-1"><dt>{k}:</dt><dd className="text-text/80">{v}</dd></div>)}
            </dl>
            {p.aliases.length > 0 && <p className="mt-1 text-xs text-text/60">{t("adult.aliases")}: {p.aliases.join(", ")}</p>}
            {p.description && <p className="mt-2 line-clamp-4 max-w-prose text-sm text-text/80">{p.description}</p>}
          </div>
        </header>
      )}
      {p && (
        <DetailTabs scenes={p.sceneCount} galleries={p.galleryCount}>
          {(tab) => tab === "scenes" ? <SceneGrid query={{ performerId }} /> : <GalleryGrid query={{ performerId }} />}
        </DetailTabs>
      )}
    </div>
  );
}

export function AdultScenePage() {
  const { t, i18n } = useTranslation();
  const { sceneId } = useParams({ from: "/app/adult/scene/$sceneId" });
  const { data: me } = useMe();
  const qc = useQueryClient();
  const blur = useBlur();
  const scene = useQuery({ queryKey: ["adult", "scene", sceneId],
    queryFn: async () => unwrap(await api.GET("/adult/scenes/{sceneId}", { params: { path: { sceneId } } })) });
  const refresh = useMutation({
    mutationFn: async () => unwrap(await api.POST("/adult/scenes/{sceneId}/refresh", { params: { path: { sceneId } } })),
    onSuccess: (data) => {
      qc.setQueryData(["adult", "scene", sceneId], data);
      void qc.invalidateQueries({ queryKey: ["adult", "scenes"] });
    },
  });
  if (scene.isPending) return <div className="py-16 text-center"><Spinner /></div>;
  if (scene.error) return <ErrorText error={scene.error} />;
  const s = scene.data;
  const resume = s.positionMs > 0 && !s.played;
  const art = s.hasBackdrop ? adultImage("scene", s.id, "backdrop") : s.hasPoster ? adultImage("scene", s.id, "poster") : undefined;
  return (
    <article className="flex flex-col gap-8">
      <Breadcrumbs items={[s.kind === "movie" ? { label: t("adult.movies"), to: "/adult/movies" } : { label: t("adult.scenes"), to: "/adult/scenes" },
        ...(s.studio ? [{ label: s.studio.name, to: `/adult/studio/${s.studio.id}` }] : []), { label: s.title }]} />
      <div className="grid gap-8 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <div className="relative aspect-video overflow-hidden rounded-[8px] bg-surface ring-1 ring-border">
          <BlurArt blur={blur} src={art} fallback={<Monogram name={s.studio?.name ?? s.title} />} />
        </div>
        <div className="flex min-w-0 flex-col gap-4">
          <div>
            {s.studio && (
              <Link to="/adult/studio/$studioId" params={{ studioId: s.studio.id }} className="text-xs font-semibold tracking-[0.14em] text-muted uppercase hover:text-text">
                {s.studio.name}
              </Link>
            )}
            <h2 className="mt-1 font-display text-[clamp(1.8rem,3.4vw,2.8rem)] leading-tight">{s.title}</h2>
            <p className="mt-2 flex flex-wrap gap-x-3 text-sm text-muted tabular">
              {s.kind === "movie" ? <span>{s.year}</span> : s.date && <span>{formatDate(s.date, i18n.language)}</span>}
              {s.durationMs ? <span>{formatDuration(s.durationMs)}</span> : null}
              {s.height ? <span>{s.height}p</span> : null}
              {s.played && <span>{t("adult.watched")}</span>}
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Link to="/adult/watch/$sceneId" params={{ sceneId: s.id }} data-testid="adult-play"
              className="inline-flex h-12 items-center gap-2 rounded-[6px] bg-text px-6 text-[15px] font-semibold text-bg transition-colors hover:bg-white active:scale-[0.97]">
              <Play className="size-5 fill-current" aria-hidden />{resume ? t("adult.resume", { time: formatDuration(s.positionMs, true) }) : t("adult.play")}
            </Link>
            {resume && (
              <Link to="/adult/watch/$sceneId" params={{ sceneId: s.id }} search={{ start: 0 }}
                className="inline-flex h-12 items-center gap-2 rounded-[6px] bg-white/[0.08] px-4 text-sm font-medium ring-1 ring-inset ring-border hover:bg-white/[0.13]">
                <RotateCcw className="size-4" aria-hidden />{t("adult.fromStart")}
              </Link>
            )}
          </div>
          {s.performers.length > 0 && (
            <div>
              <p className="mb-2 text-xs font-semibold tracking-[0.14em] text-muted uppercase">{t("adult.performers")}</p>
              <ul className="flex flex-wrap gap-2">
                {s.performers.map((p) => (
                  <li key={p.id}>
                    <Link to="/adult/performer/$performerId" params={{ performerId: p.id }}
                      className="inline-flex h-8 items-center gap-1.5 rounded-full bg-white/[0.07] pr-3 pl-2 text-sm ring-1 ring-inset ring-border hover:bg-white/[0.13]">
                      <UserRound className="size-4 text-muted" aria-hidden />{p.name}
                    </Link>
                  </li>
                ))}
              </ul>
            </div>
          )}
          {s.description && <p className="max-w-prose text-[15px] leading-relaxed text-text/80">{s.description}</p>}
          {s.tags.length > 0 && (
            <ul className="flex flex-wrap gap-1.5">
              {s.tags.map((tag) => (
                <li key={tag}>
                  <Link to="/adult/tag/$tag" params={{ tag }} className="inline-flex h-7 items-center rounded-[4px] bg-white/[0.05] px-2 text-xs text-text/75 hover:bg-white/[0.1]">#{tag}</Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
      {s.galleries.length > 0 && (
        <section>
          <h3 className="mb-3 font-head text-lg">{t("adult.sceneGalleries")}</h3>
          <GalleryGrid items={s.galleries} />
        </section>
      )}
      <section className="flex flex-col gap-2 border-t border-border pt-5 text-sm text-muted">
        {s.files.map((f) => (
          <p key={f.id} className="flex flex-wrap gap-x-3 tabular">
            <span className="truncate text-text/70">{f.name}</span>
            <span>{formatSize(f.size)}</span>
            {f.width && f.height ? <span>{f.width}×{f.height}</span> : null}
            {f.videoCodec && <span>{f.videoCodec.toUpperCase()}</span>}
          </p>
        ))}
        {s.metadataError && <p className="text-xs">{t("adult.noMatch")}</p>}
        {canAdminister(me) && (
          <div className="mt-2 flex items-center gap-3">
            <Button variant="secondary" disabled={refresh.isPending} onClick={() => refresh.mutate()}>{refresh.isPending ? t("adult.lookingUp") : t("adult.lookUp")}</Button>
            <ErrorText error={refresh.error} />
          </div>
        )}
      </section>
    </article>
  );
}

/** The PIN asked once per session before the section opens. */
function PinGate() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [pin, setPin] = useState("");
  const unlock = useMutation({
    mutationFn: async () => unwrap(await api.POST("/adult/unlock", { body: { pin } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["adult"] }),
    onError: () => setPin(""),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    unlock.mutate();
  };
  return (
    <div className="mx-auto flex max-w-sm flex-col items-center gap-5 py-20 text-center">
      <span className="grid size-14 place-items-center rounded-full bg-white/[0.07] ring-1 ring-border"><Lock className="size-6" aria-hidden /></span>
      <div>
        <h1 className="font-head text-xl">{t("adult.lockedTitle")}</h1>
        <p className="mt-1 text-sm text-muted">{t("adult.lockedText")}</p>
      </div>
      <form className="flex w-full flex-col gap-3" onSubmit={submit}>
        <input autoFocus inputMode="numeric" autoComplete="off" type="password" pattern="[0-9]*" maxLength={12} value={pin}
          aria-label={t("adult.pin")} data-testid="adult-pin" onChange={(e) => setPin(e.target.value.replace(/\D/g, ""))}
          className="h-12 rounded-[6px] border border-border bg-black/30 text-center text-2xl tracking-[0.5em] focus:border-accent/70 focus-visible:outline-none" />
        <ErrorText error={unlock.error} />
        <Button type="submit" size="lg" disabled={pin.length < 4 || unlock.isPending}>{t("adult.unlock")}</Button>
      </form>
    </div>
  );
}

/** Blurring and the PIN of this account. */
function PrefsPanel({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: status } = useAdultStatus();
  const [pin, setPin] = useState("");
  const [current, setCurrent] = useState("");
  const save = useMutation({
    mutationFn: async (body: { blur?: boolean; pin?: string; currentPin?: string; hideTagged?: boolean }) => unwrap(await api.PUT("/adult/preferences", { body })),
    onSuccess: (data, body) => {
      qc.setQueryData(statusKey, data);
      setPin("");
      setCurrent("");
      if (body.hideTagged !== undefined) void qc.invalidateQueries({ predicate: (q) => q.queryKey[0] === "adult" && q.queryKey[1] !== "status" });
    },
  });
  if (!status) return null;
  return (
    <div className="rounded-[8px] border border-border bg-surface p-5" data-testid="adult-prefs">
      <div className="mb-4 flex items-center">
        <h2 className="font-head text-[17px]">{t("adult.preferences")}</h2>
        <button type="button" onClick={onClose} aria-label={t("common.close")} className="ml-auto grid size-8 place-items-center rounded-[6px] hover:bg-white/10"><X className="size-4" /></button>
      </div>
      <label className="flex items-start gap-3 text-sm">
        <input type="checkbox" className="mt-1 accent-[var(--color-accent)]" checked={status.blur} disabled={save.isPending}
          data-testid="adult-blur" onChange={(e) => save.mutate({ blur: e.target.checked })} />
        <span><span className="block font-medium">{t("adult.blur")}</span><span className="text-muted">{t("adult.blurHint")}</span></span>
      </label>
      <label className="mt-4 flex items-start gap-3 text-sm">
        <input type="checkbox" className="mt-1 accent-[var(--color-accent)]" checked={status.hideTagged} disabled={save.isPending}
          data-testid="adult-hide-tagged" onChange={(e) => save.mutate({ hideTagged: e.target.checked })} />
        <span><span className="block font-medium">{t("adult.hideTagged")}</span>
          <span className="text-muted">{t("adult.hideTaggedHint", { count: status.hiddenTags.length })}</span></span>
      </label>
      <form className="mt-5 flex flex-col gap-3 border-t border-border pt-5" onSubmit={(e) => {
        e.preventDefault();
        save.mutate({ pin, currentPin: status.pinSet ? current : undefined });
      }}>
        <p className="text-sm"><span className="font-medium">{t("adult.pinTitle")}</span> <span className="text-muted">{status.pinSet ? t("adult.pinOn") : t("adult.pinOff")}</span></p>
        <p className="text-xs text-muted">{t("adult.pinHint")}</p>
        <div className="grid gap-3 sm:grid-cols-2">
          {status.pinSet && (
            <TextField label={t("adult.currentPin")} type="password" inputMode="numeric" autoComplete="off" maxLength={12}
              value={current} onChange={(e) => setCurrent(e.target.value.replace(/\D/g, ""))} />
          )}
          <TextField label={status.pinSet ? t("adult.newPin") : t("adult.pin")} type="password" inputMode="numeric" autoComplete="off" maxLength={12}
            value={pin} onChange={(e) => setPin(e.target.value.replace(/\D/g, ""))} />
        </div>
        <ErrorText error={save.error} />
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={pin.length < 4 || save.isPending}>{status.pinSet ? t("adult.changePin") : t("adult.setPin")}</Button>
          {status.pinSet && (
            <Button type="button" variant="danger" disabled={current.length < 4 || save.isPending}
              onClick={() => save.mutate({ pin: "", currentPin: current })}>{t("adult.removePin")}</Button>
          )}
        </div>
      </form>
    </div>
  );
}

type NavTo = "/adult" | "/adult/scenes" | "/adult/movies" | "/adult/galleries" | "/adult/performers" | "/adult/studios" | "/adult/tags";

const navItems: { to: NavTo; key: string; icon: typeof House }[] = [
  { to: "/adult", key: "adult.home", icon: House },
  { to: "/adult/scenes", key: "adult.scenes", icon: Clapperboard },
  { to: "/adult/movies", key: "adult.movies", icon: Film },
  { to: "/adult/galleries", key: "adult.galleries", icon: Images },
  { to: "/adult/performers", key: "adult.performersTab", icon: UserRound },
  { to: "/adult/studios", key: "adult.studios", icon: Building2 },
  { to: "/adult/tags", key: "adult.tags", icon: Tag },
];

const navLink = "tv-focus flex shrink-0 items-center gap-2.5 rounded-[6px] px-3 py-2 text-sm font-medium text-text/60 transition-colors hover:bg-white/[0.06] hover:text-text [&.active]:bg-white/[0.09] [&.active]:text-text";

/** Search inside the adult section (never the regular search). */
function SectionSearch() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [q, setQ] = useState("");
  return (
    <form role="search" className="relative flex w-full items-center lg:max-w-xs" onSubmit={(e) => {
      e.preventDefault();
      if (q.trim()) void navigate({ to: "/adult/search", search: { q: q.trim() } });
    }}>
      <Search className="pointer-events-none absolute left-3 size-4 text-muted" aria-hidden />
      <input type="search" value={q} onChange={(e) => setQ(e.target.value)} aria-label={t("adult.searchSection")} placeholder={t("adult.searchSection")}
        data-testid="adult-search" maxLength={200}
        className="h-9 w-full rounded-[6px] border border-border bg-white/[0.05] pr-3 pl-9 text-sm text-text placeholder:text-faint focus:border-accent/70 focus-visible:outline-none" />
    </form>
  );
}

/** The adult section: only for accounts given adult access (D-053), behind the PIN when one is set. */
export function AdultLayout() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: me } = useMe();
  const allowed = !!me?.adultAccess;
  const status = useAdultStatus(allowed);
  const [prefs, setPrefs] = useState(false);
  const locked = useMemo(() => !!status.data && status.data.pinSet && !status.data.unlocked, [status.data]);
  const lock = useMutation({
    mutationFn: async () => unwrap(await api.POST("/adult/lock")),
    onSuccess: () => {
      qc.removeQueries({ queryKey: ["adult"] });
      void qc.invalidateQueries({ queryKey: statusKey });
    },
  });
  // The tab title never names what is on screen.
  useEffect(() => {
    const before = document.title;
    document.title = "Streamero";
    return () => { document.title = before; };
  }, []);
  if (!allowed || status.error) return <Navigate to="/" />;
  if (status.isPending) return <div className="pt-32 text-center"><Spinner /></div>;
  const empty = status.data && status.data.libraries.every((l) => l.sceneCount === 0 && l.galleryCount === 0);
  return (
    <div className="px-4 pt-24 sm:px-10 lg:px-16" data-testid="adult-section">
      <header className="mb-6 flex flex-wrap items-center gap-x-6 gap-y-3">
        <h1 className="font-display text-[clamp(1.6rem,3vw,2.2rem)]">{t("adult.title")}</h1>
        {!locked && <div className="order-last w-full lg:order-none lg:ml-auto lg:w-auto lg:flex-1 lg:justify-end lg:flex"><SectionSearch /></div>}
        <div className="ml-auto flex items-center gap-1 lg:ml-0">
          {!locked && (
            <button type="button" onClick={() => setPrefs(!prefs)} aria-label={t("adult.preferences")} title={t("adult.preferences")}
              className="tv-focus grid size-9 place-items-center rounded-[6px] text-text/75 hover:bg-white/10 hover:text-text"><Settings2 className="size-[18px]" /></button>
          )}
          {status.data?.pinSet && !locked && (
            <button type="button" onClick={() => lock.mutate()} aria-label={t("adult.lock")} title={t("adult.lock")} data-testid="adult-lock"
              className="tv-focus grid size-9 place-items-center rounded-[6px] text-text/75 hover:bg-white/10 hover:text-text"><Lock className="size-[18px]" /></button>
          )}
        </div>
      </header>
      {locked ? <PinGate /> : (
        <div className="flex flex-col gap-6 lg:flex-row lg:gap-10">
          <nav aria-label={t("adult.title")} data-testid="adult-nav"
            className="-mx-4 flex gap-1 overflow-x-auto border-b border-border px-4 pb-3 [scrollbar-width:none] lg:sticky lg:top-24 lg:mx-0 lg:w-48 lg:shrink-0 lg:flex-col lg:self-start lg:overflow-visible lg:border-b-0 lg:px-0 lg:pb-0">
            {navItems.map(({ to, key, icon: Icon }) => (
              <Link key={to} to={to} className={navLink} activeOptions={{ exact: to === "/adult", includeSearch: false }}>
                <Icon className="size-4" aria-hidden />{t(key)}
              </Link>
            ))}
          </nav>
          <main className="min-w-0 flex-1 pb-16">
            {empty && <p className="mb-6 text-sm text-muted">{t("adult.empty")}</p>}
            {!status.data?.metadataConfigured && canAdminister(me) && (
              <p className="mb-6 flex items-center gap-2 text-sm text-muted"><Badge>{t("adult.noKeysBadge")}</Badge>
                <Link to="/settings/metadata" className="underline underline-offset-4 hover:text-text">{t("adult.noKeys")}</Link></p>
            )}
            {prefs && <div className="mb-8"><PrefsPanel onClose={() => setPrefs(false)} /></div>}
            <LockedBoundary><Outlet /></LockedBoundary>
          </main>
        </div>
      )}
    </div>
  );
}

/** A content call that finds the section locked (another tab locked it) refreshes the status. */
function LockedBoundary({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  useEffect(() => qc.getQueryCache().subscribe((e) => {
    if (e.type === "updated" && e.query.queryKey[0] === "adult" && isLocked(e.query.state.error)) {
      void qc.invalidateQueries({ queryKey: statusKey });
    }
  }), [qc]);
  return <>{children}</>;
}

