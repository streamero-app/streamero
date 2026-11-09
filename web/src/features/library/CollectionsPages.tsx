import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowDown, ArrowUp, Globe, Library, ListPlus, Lock, Play, Plus, Trash2, X } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Art, Button, ErrorText, Spinner, TextField, buttonVariants, pressable } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { ArtFallback, ItemCard, isPlayable } from "./cards";
import { formatDuration, imageUrl, placeholderHue, type ItemSummary } from "./format";

type Collection = components["schemas"]["Collection"];
type Playlist = components["schemas"]["Playlist"];
const gutter = "px-4 sm:px-8 lg:px-14";

function collectionImage(id: string, type: "poster" | "backdrop") {
  return `/api/v1/collections/${id}/images/${type}`;
}

export function useCollections() {
  return useQuery({ queryKey: ["collections"], queryFn: async () => unwrap(await api.GET("/collections")) });
}

export function usePlaylists() {
  return useQuery({ queryKey: ["playlists"], queryFn: async () => unwrap(await api.GET("/playlists")) });
}

/** Poster tile of a collection. */
export function CollectionCard({ c, className = "" }: { c: Collection; className?: string }) {
  const { t } = useTranslation();
  const [broken, setBroken] = useState(false);
  return (
    <Link to="/collection/$collectionId" params={{ collectionId: c.id }} data-testid="collection-card"
      className={`tv-focus group flex flex-col gap-2 ${className}`}>
      <div className="relative aspect-[2/3] overflow-hidden rounded-md bg-surface-2 transition-transform duration-300 ease-out group-hover:scale-[1.04]">
        {c.hasPoster && !broken
          ? <Art src={collectionImage(c.id, "poster")} onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover" />
          : <ArtFallback item={{ id: c.id, title: c.name, kind: "movie", addedAt: "", hasPoster: false }} />}
        <span className="absolute top-2 left-2 rounded-sm bg-black/70 px-1.5 py-0.5 text-[11px] font-semibold text-white/90 backdrop-blur">
          {t("collections.count", { count: c.itemCount })}
        </span>
      </div>
      <p className="truncate px-0.5 text-[13px] font-medium text-white/85 group-hover:text-white">{c.name}</p>
    </Link>
  );
}

export function CollectionsPage() {
  const { t } = useTranslation();
  const cols = useCollections();
  return (
    <div className={`flex flex-col gap-6 pt-24 ${gutter}`}>
      <h1 className="text-3xl font-bold tracking-tight sm:text-4xl">{t("collections.title")}</h1>
      {cols.isPending && <Spinner />}
      {cols.data?.length === 0 && <p className="max-w-prose text-muted">{t("collections.empty")}</p>}
      <div className="grid grid-cols-3 gap-x-2 gap-y-6 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7">
        {cols.data?.map((c) => <CollectionCard key={c.id} c={c} />)}
      </div>
    </div>
  );
}

export function CollectionPage() {
  const { t } = useTranslation();
  const { collectionId } = useParams({ from: "/app/collection/$collectionId" });
  const { data: me } = useMe();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const detail = useQuery({
    queryKey: ["collection", collectionId],
    queryFn: async () => unwrap(await api.GET("/collections/{collectionId}", { params: { path: { collectionId } } })),
  });
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/collections/{collectionId}", { params: { path: { collectionId } } })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["collections"] }); void navigate({ to: "/collections" }); },
  });
  const d = detail.data;
  if (detail.error) return <div className={`pt-24 ${gutter}`}><ErrorText error={detail.error} /></div>;
  if (!d) return <div className="flex min-h-[60vh] items-center justify-center"><Spinner /></div>;
  const c = d.collection;
  const first = d.items.find(isPlayable);
  const hue = placeholderHue(c.name);
  return (
    <div className="flex flex-col gap-8 pb-8">
      <section className="relative isolate flex min-h-[56vh] items-end pt-28 pb-10">
        <div className="absolute inset-0 -z-10 overflow-hidden" aria-hidden>
          {c.hasBackdrop
            ? <Art src={collectionImage(c.id, "backdrop")} loading="eager" className="hero-settle absolute inset-0 size-full object-cover" />
            : <div className="absolute inset-0" style={{ background: `radial-gradient(80% 90% at 75% 20%, hsl(${hue} 22% 22%), transparent 70%)` }} />}
          <div className="absolute inset-0 bg-gradient-to-r from-black/85 via-black/40 to-transparent" />
          <div className="absolute inset-x-0 bottom-0 h-1/2 bg-gradient-to-t from-bg to-transparent" />
        </div>
        <div className={`flex max-w-3xl flex-col gap-4 ${gutter}`}>
          <p className="flex items-center gap-2 text-sm font-semibold text-white/70"><Library className="size-4" aria-hidden />{t("collections.one")}</p>
          <h1 className="font-display text-4xl leading-[0.95] sm:text-6xl">{c.name}</h1>
          <p className="text-sm text-white/75">{t("collections.count", { count: c.itemCount })}</p>
          {c.overview && <p className="max-w-[65ch] text-white/85">{c.overview}</p>}
          <div className="flex flex-wrap gap-3">
            {first && (
              <Link to="/watch/$itemId" params={{ itemId: first.id }} className={`${pressable} ${buttonVariants.primary} h-12 rounded-[6px] px-7 text-base font-semibold`}>
                <Play className="size-5 fill-current" aria-hidden />{t("player.play")}
              </Link>
            )}
            {me?.isAdmin && !c.auto && (
              <Button variant="danger" onClick={() => { if (window.confirm(t("collections.confirmDelete", { name: c.name }))) remove.mutate(); }}>
                <Trash2 className="size-4" aria-hidden />{t("libraries.remove")}
              </Button>
            )}
          </div>
        </div>
      </section>
      <div className={`grid grid-cols-3 gap-x-2 gap-y-6 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7 ${gutter}`}>
        {d.items.map((it) => <ItemCard key={it.id} item={it} />)}
      </div>
    </div>
  );
}

/** Four artworks in a grid make the cover of a playlist. */
function PlaylistCover({ p }: { p: Playlist }) {
  const ids = p.coverItemIds ?? [];
  const hue = placeholderHue(p.name);
  if (ids.length === 0) {
    return <div className="absolute inset-0 grid place-items-center" style={{ background: `linear-gradient(135deg, hsl(${hue} 30% 26%), #151515)` }}>
      <ListPlus className="size-10 text-white/40" aria-hidden />
    </div>;
  }
  return (
    <div className={`absolute inset-0 grid ${ids.length > 1 ? "grid-cols-2 grid-rows-2" : ""}`}>
      {(ids.length > 1 ? [...ids, ...ids].slice(0, 4) : ids).map((id, i) => (
        <img key={i} src={imageUrl(id, "poster")} alt="" className="size-full object-cover" loading="lazy"
          onError={(e) => { e.currentTarget.src = imageUrl(id, "backdrop"); }} />
      ))}
    </div>
  );
}

export function PlaylistsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const lists = usePlaylists();
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/playlists", { body: { name } })),
    onSuccess: (p) => { setName(""); void qc.invalidateQueries({ queryKey: ["playlists"] }); void navigate({ to: "/playlist/$playlistId", params: { playlistId: p.id } }); },
  });
  return (
    <div className={`flex flex-col gap-6 pt-24 ${gutter}`}>
      <h1 className="text-3xl font-bold tracking-tight sm:text-4xl">{t("playlists.title")}</h1>
      <form className="flex max-w-md items-end gap-2" onSubmit={(e) => { e.preventDefault(); if (name.trim()) create.mutate(); }}>
        <div className="flex-1"><TextField label={t("playlists.newName")} value={name} maxLength={120} onChange={(e) => setName(e.target.value)} /></div>
        <Button type="submit" disabled={!name.trim() || create.isPending}><Plus className="size-4" aria-hidden />{t("playlists.create")}</Button>
      </form>
      <ErrorText error={create.error} />
      {lists.data?.length === 0 && <p className="text-muted">{t("playlists.empty")}</p>}
      <div className="grid grid-cols-2 gap-x-3 gap-y-6 sm:grid-cols-3 lg:grid-cols-5 2xl:grid-cols-6">
        {lists.data?.map((p) => (
          <Link key={p.id} to="/playlist/$playlistId" params={{ playlistId: p.id }} data-testid="playlist-card" className="tv-focus group flex flex-col gap-2">
            <div className="relative aspect-square overflow-hidden rounded-md bg-surface-2 transition-transform duration-300 ease-out group-hover:scale-[1.03]">
              <PlaylistCover p={p} />
            </div>
            <div className="px-0.5">
              <p className="flex items-center gap-1.5 truncate text-sm font-semibold">
                {p.public ? <Globe className="size-3.5 shrink-0 text-muted" aria-label={t("playlists.public")} /> : <Lock className="size-3.5 shrink-0 text-muted" aria-label={t("playlists.private")} />}
                {p.name}
              </p>
              <p className="text-xs text-muted tabular">{t("playlists.count", { count: p.itemCount })}{p.durationMs ? ` · ${formatDuration(p.durationMs)}` : ""}</p>
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}

export function PlaylistPage() {
  const { t } = useTranslation();
  const { playlistId } = useParams({ from: "/app/playlist/$playlistId" });
  const qc = useQueryClient();
  const navigate = useNavigate();
  const detail = useQuery({
    queryKey: ["playlist", playlistId],
    queryFn: async () => unwrap(await api.GET("/playlists/{playlistId}", { params: { path: { playlistId } } })),
  });
  const [name, setName] = useState("");
  useEffect(() => { if (detail.data) setName(detail.data.playlist.name); }, [detail.data]);
  const save = useMutation({
    mutationFn: async (v: { name?: string; public?: boolean; itemIds?: string[] }) => {
      const p = detail.data!.playlist;
      return unwrap(await api.PUT("/playlists/{playlistId}", { params: { path: { playlistId } },
        body: { name: v.name ?? p.name, public: v.public ?? p.public, itemIds: v.itemIds } }));
    },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["playlist", playlistId] }); void qc.invalidateQueries({ queryKey: ["playlists"] }); },
  });
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/playlists/{playlistId}", { params: { path: { playlistId } } })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["playlists"] }); void navigate({ to: "/playlists" }); },
  });
  const d = detail.data;
  if (detail.error) return <div className={`pt-24 ${gutter}`}><ErrorText error={detail.error} /></div>;
  if (!d) return <div className="flex min-h-[60vh] items-center justify-center"><Spinner /></div>;
  const p = d.playlist;
  const ids = d.items.map((i) => i.id);
  const move = (from: number, to: number) => {
    const next = [...ids];
    const [x] = next.splice(from, 1);
    next.splice(to, 0, x!);
    save.mutate({ itemIds: next });
  };
  const first = d.items.find(isPlayable);
  return (
    <div className={`flex flex-col gap-6 pt-24 pb-8 ${gutter}`}>
      <div className="flex flex-wrap items-end gap-6">
        <div className="relative aspect-square w-40 shrink-0 overflow-hidden rounded-md bg-surface-2 shadow-xl sm:w-52"><PlaylistCover p={p} /></div>
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          {p.mine ? (
            <input value={name} onChange={(e) => setName(e.target.value)} aria-label={t("playlists.name")} maxLength={120}
              onBlur={() => { if (name.trim() && name !== p.name) save.mutate({ name }); }}
              className="w-full bg-transparent font-display text-4xl leading-tight outline-none focus:shadow-none sm:text-5xl" />
          ) : <h1 className="font-display text-4xl sm:text-5xl">{p.name}</h1>}
          <p className="text-sm text-muted tabular">{t("playlists.count", { count: p.itemCount })}{p.durationMs ? ` · ${formatDuration(p.durationMs)}` : ""}</p>
          <div className="flex flex-wrap items-center gap-3">
            {first && (
              <Link to="/watch/$itemId" params={{ itemId: first.id }} search={{ playlist: p.id }}
                className={`${pressable} ${buttonVariants.primary} h-12 rounded-[6px] px-7 text-base font-semibold`} data-testid="play-all">
                <Play className="size-5 fill-current" aria-hidden />{t("playlists.playAll")}
              </Link>
            )}
            {p.mine && (
              <>
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" className="accent-[var(--color-accent)]" checked={p.public} onChange={(e) => save.mutate({ public: e.target.checked })} />
                  {t("playlists.shareAll")}
                </label>
                <Button variant="danger" onClick={() => { if (window.confirm(t("playlists.confirmDelete", { name: p.name }))) remove.mutate(); }}>
                  <Trash2 className="size-4" aria-hidden />{t("libraries.remove")}
                </Button>
              </>
            )}
          </div>
          <ErrorText error={save.error ?? remove.error} />
        </div>
      </div>
      {d.items.length === 0 && <p className="text-muted">{t("playlists.addHint")}</p>}
      <ol className="flex flex-col border-t border-white/10">
        {d.items.map((it, i) => (
          <li key={`${it.id}-${i}`} className="flex items-center gap-3 border-b border-white/10 py-2.5" data-testid="playlist-row">
            <span className="w-6 text-right text-sm text-muted tabular">{i + 1}</span>
            <Link to="/watch/$itemId" params={{ itemId: it.id }} search={{ playlist: p.id }} className="group flex min-w-0 flex-1 items-center gap-3">
              <div className="relative aspect-video w-28 shrink-0 overflow-hidden rounded bg-surface-2">
                {it.hasBackdrop || it.hasPoster
                  ? <img src={imageUrl(it.id, it.hasBackdrop ? "backdrop" : it.kind === "episode" ? "still" : "poster")} alt="" className="absolute inset-0 size-full object-cover" />
                  : <ArtFallback item={it} wide />}
              </div>
              <span className="min-w-0">
                <span className="block truncate font-medium group-hover:underline">{it.seriesTitle ?? it.title}</span>
                <span className="block truncate text-xs text-muted">
                  {it.seriesTitle ? `S${it.seasonNumber}:E${it.episodeNumber} · ${it.title}` : it.year}
                  {it.durationMs ? ` · ${formatDuration(it.durationMs)}` : ""}
                </span>
              </span>
            </Link>
            {p.mine && (
              <div className="flex gap-1">
                <button type="button" className="grid size-8 place-items-center rounded hover:bg-white/10 disabled:opacity-30" disabled={i === 0}
                  aria-label={t("playlists.moveUp")} onClick={() => move(i, i - 1)}><ArrowUp className="size-4" aria-hidden /></button>
                <button type="button" className="grid size-8 place-items-center rounded hover:bg-white/10 disabled:opacity-30" disabled={i === d.items.length - 1}
                  aria-label={t("playlists.moveDown")} onClick={() => move(i, i + 1)}><ArrowDown className="size-4" aria-hidden /></button>
                <button type="button" className="grid size-8 place-items-center rounded hover:bg-white/10"
                  aria-label={t("playlists.removeItem")} onClick={() => save.mutate({ itemIds: ids.filter((_, j) => j !== i) })}><X className="size-4" aria-hidden /></button>
              </div>
            )}
          </li>
        ))}
      </ol>
    </div>
  );
}

/** "Add to playlist" (everyone) and "Add to collection" (admins) menu on a title page. */
export function AddToMenu({ item }: { item: ItemSummary }) {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [done, setDone] = useState<string | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const lists = usePlaylists();
  const cols = useCollections();
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) setOpen(false); };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);
  const toPlaylist = useMutation({
    mutationFn: async (v: { id?: string; name?: string }) => {
      const id = v.id ?? unwrap(await api.POST("/playlists", { body: { name: v.name! } })).id;
      return unwrap(await api.POST("/playlists/{playlistId}/items", { params: { path: { playlistId: id } }, body: { itemIds: [item.id] } }));
    },
    onSuccess: (p) => { setDone(p.name); setOpen(false); void qc.invalidateQueries({ queryKey: ["playlists"] }); },
  });
  const toCollection = useMutation({
    mutationFn: async (v: { id?: string; name?: string }) => {
      if (!v.id) return unwrap(await api.POST("/collections", { body: { name: v.name!, itemIds: [item.id] } }));
      const c = unwrap(await api.GET("/collections/{collectionId}", { params: { path: { collectionId: v.id } } }));
      return unwrap(await api.PUT("/collections/{collectionId}", { params: { path: { collectionId: v.id } },
        body: { name: c.collection.name, overview: c.collection.overview, itemIds: [...c.items.map((i) => i.id), item.id] } }));
    },
    onSuccess: (c) => { setDone(c.name); setOpen(false); void qc.invalidateQueries({ queryKey: ["collections"] }); void qc.invalidateQueries({ queryKey: ["item"] }); },
  });
  const ask = (key: string) => window.prompt(t(key))?.trim();
  const row = "flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-white/85 hover:bg-white/[0.07] hover:text-white";
  return (
    <div className="relative" ref={ref}>
      <button type="button" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)} title={t("playlists.addTo")} aria-label={t("playlists.addTo")}
        className="tv-focus grid size-13 place-items-center rounded-[6px] bg-white/[0.12] text-text backdrop-blur-xl transition-colors hover:bg-white/[0.2]">
        <ListPlus className="size-5" aria-hidden />
      </button>
      {done && <span role="status" className="absolute top-full left-1/2 mt-2 -translate-x-1/2 rounded bg-black/80 px-2 py-1 text-xs whitespace-nowrap">{t("playlists.added", { name: done })}</span>}
      {open && (
        <div role="menu" className="absolute bottom-full left-0 z-50 mb-2 max-h-80 w-64 overflow-y-auto rounded-md border border-white/10 bg-black/95 py-2 shadow-2xl backdrop-blur-xl">
          <p className="px-4 pb-1 text-[11px] font-semibold tracking-wider text-white/50 uppercase">{t("playlists.title")}</p>
          {lists.data?.filter((p) => p.mine).map((p) => (
            <button key={p.id} type="button" role="menuitem" className={row} onClick={() => toPlaylist.mutate({ id: p.id })}>{p.name}</button>
          ))}
          <button type="button" role="menuitem" className={row} onClick={() => { const n = ask("playlists.newName"); if (n) toPlaylist.mutate({ name: n }); }}>
            <Plus className="size-4" aria-hidden />{t("playlists.create")}
          </button>
          {me?.isAdmin && (
            <>
              <p className="mt-2 border-t border-white/10 px-4 pt-2 pb-1 text-[11px] font-semibold tracking-wider text-white/50 uppercase">{t("collections.title")}</p>
              {cols.data?.filter((c) => !c.auto).map((c) => (
                <button key={c.id} type="button" role="menuitem" className={row} onClick={() => toCollection.mutate({ id: c.id })}>{c.name}</button>
              ))}
              <button type="button" role="menuitem" className={row} onClick={() => { const n = ask("collections.newName"); if (n) toCollection.mutate({ name: n }); }}>
                <Plus className="size-4" aria-hidden />{t("collections.create")}
              </button>
            </>
          )}
          <div className="px-4"><ErrorText error={toPlaylist.error ?? toCollection.error} /></div>
        </div>
      )}
    </div>
  );
}
