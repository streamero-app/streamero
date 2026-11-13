/** Collections and playlists pieces used outside their own pages (title page, library rows). */
import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ListPlus, Plus } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Art, ErrorText } from "@/components/ui";
import { usePrompt } from "@/components/Dialog";
import { canAdminister, useMe } from "@/features/auth/useAuth";
import { ArtFallback } from "./cards";
import { type ItemSummary } from "./format";

export type Collection = components["schemas"]["Collection"];
export type Playlist = components["schemas"]["Playlist"];
export const gutter = "px-4 sm:px-8 lg:px-14";

export function collectionImage(id: string, type: "poster" | "backdrop") {
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

/** "Add to playlist" (everyone) and "Add to collection" (admins) menu on a title page. */
export function AddToMenu({ item }: { item: ItemSummary }) {
  const { t } = useTranslation();
  const promptDialog = usePrompt();
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
  /** Asks for a name in the themed dialog; the menu closes first so the dialog has the stage. */
  const ask = (key: string, create: (name: string) => void) => {
    setOpen(false);
    void promptDialog({ message: t(key), confirmLabel: t("dialog.create") }).then((n) => { if (n) create(n); });
  };
  const row = "flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-white/85 hover:bg-white/[0.07] hover:text-white";
  return (
    <div className="relative" ref={ref}>
      <button type="button" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)} title={t("playlists.addTo")} aria-label={t("playlists.addTo")}
        className="tv-focus grid size-13 place-items-center rounded-[6px] bg-white/[0.12] text-text backdrop-blur-xl transition-colors hover:bg-white/[0.2]">
        <ListPlus className="size-5" aria-hidden />
      </button>
      {!open && (toPlaylist.error || toCollection.error) && (
        <div className="absolute top-full left-1/2 mt-2 w-max max-w-64 -translate-x-1/2 rounded bg-black/85 px-2 py-1"><ErrorText error={toPlaylist.error ?? toCollection.error} /></div>
      )}
      {done && <span role="status" className="absolute top-full left-1/2 mt-2 -translate-x-1/2 rounded bg-black/80 px-2 py-1 text-xs whitespace-nowrap">{t("playlists.added", { name: done })}</span>}
      {open && (
        <div role="menu" className="absolute bottom-full left-0 z-50 mb-2 max-h-80 w-64 overflow-y-auto rounded-md border border-white/10 bg-black/95 py-2 shadow-2xl backdrop-blur-xl">
          <p className="px-4 pb-1 text-[11px] font-semibold tracking-wider text-white/50 uppercase">{t("playlists.title")}</p>
          {lists.data?.filter((p) => p.mine).map((p) => (
            <button key={p.id} type="button" role="menuitem" className={row} onClick={() => toPlaylist.mutate({ id: p.id })}>{p.name}</button>
          ))}
          <button type="button" role="menuitem" className={row} onClick={() => ask("playlists.newName", (name) => toPlaylist.mutate({ name }))}>
            <Plus className="size-4" aria-hidden />{t("playlists.create")}
          </button>
          {canAdminister(me) && (
            <>
              <p className="mt-2 border-t border-white/10 px-4 pt-2 pb-1 text-[11px] font-semibold tracking-wider text-white/50 uppercase">{t("collections.title")}</p>
              {cols.data?.filter((c) => !c.auto).map((c) => (
                <button key={c.id} type="button" role="menuitem" className={row} onClick={() => toCollection.mutate({ id: c.id })}>{c.name}</button>
              ))}
              <button type="button" role="menuitem" className={row} onClick={() => ask("collections.newName", (name) => toCollection.mutate({ name }))}>
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
