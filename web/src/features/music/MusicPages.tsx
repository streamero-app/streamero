import { useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Disc3, Music, Play, Shuffle, User } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, ErrorText, Select, Spinner, buttonVariants, pressable } from "@/components/ui";
import { imageUrl, formatDuration, type ItemSummary, type Library } from "@/features/library/format";
import { useAmbientLight } from "@/features/ambient/Ambient";
import { useMusic, type QueueTrack } from "./MusicPlayer";

type ItemDetail = components["schemas"]["ItemDetail"];
const gutter = "px-5 sm:px-10 lg:px-16";

function Cover({ item, round = false, className = "" }: { item: ItemSummary; round?: boolean; className?: string }) {
  const [broken, setBroken] = useState(false);
  return (
    <div className={`relative aspect-square overflow-hidden bg-surface-2 ${round ? "rounded-full" : "rounded-[6px]"} ${className}`}>
      {item.hasPoster && !broken
        ? <img src={imageUrl(item.id, "poster")} alt="" loading="lazy" onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover" />
        : <span className="absolute inset-0 grid place-items-center text-text/30">{round ? <User className="size-1/3" aria-hidden /> : <Disc3 className="size-1/3" aria-hidden />}</span>}
    </div>
  );
}

export function AlbumCard({ item }: { item: ItemSummary }) {
  return (
    <Link to="/item/$itemId" params={{ itemId: item.id }} data-testid="album-card" className="tv-focus group flex flex-col gap-2.5 outline-none">
      <div className="poster-lift"><Cover item={item} /></div>
      <div className="min-w-0">
        <p className="truncate text-[14px] font-semibold text-text/90 group-hover:text-text">{item.title}</p>
        <p className="truncate text-[13px] text-text/50">{[item.artist, item.year].filter(Boolean).join(" · ")}</p>
      </div>
    </Link>
  );
}

export function ArtistCard({ item }: { item: ItemSummary }) {
  return (
    <Link to="/item/$itemId" params={{ itemId: item.id }} data-testid="artist-card" className="tv-focus group flex flex-col items-center gap-3 rounded-[6px] text-center outline-none">
      <div className="poster-lift w-full"><Cover item={item} round /></div>
      <p className="w-full truncate text-[14px] font-semibold text-text/90 group-hover:text-text">{item.title}</p>
    </Link>
  );
}

/** Tracks as a numbered list; a click plays from that song on. */
export function TrackList({ tracks, showAlbum = false, showArtist = false, albumArtist }: {
  tracks: QueueTrack[]; showAlbum?: boolean; showArtist?: boolean; albumArtist?: string;
}) {
  const m = useMusic();
  const discs = new Set(tracks.map((x) => x.seasonNumber ?? 1));
  let lastDisc: number | undefined;
  const { t } = useTranslation();
  return (
    <ol className="flex flex-col" data-testid="track-list">
      {tracks.map((tr, i) => {
        const disc = tr.seasonNumber ?? 1;
        const header = discs.size > 1 && disc !== lastDisc;
        lastDisc = disc;
        const active = m.current?.id === tr.id;
        return (
          <li key={tr.id}>
            {header && <p className="px-3 pt-5 pb-2 font-label text-text/50">{t("music.disc", { n: disc })}</p>}
            <button type="button" onClick={() => m.play(tracks, i)} data-testid="track-row"
              className={`tv-focus group grid w-full grid-cols-[2.25rem_minmax(0,1fr)_auto] items-center gap-3 rounded-[6px] px-3 py-2.5 text-left transition-colors hover:bg-white/[0.05] ${active ? "bg-white/[0.06]" : ""}`}>
              <span className="text-right text-sm text-text/45 tabular">
                <span className="group-hover:hidden">{active && m.playing ? <Music className="ml-auto size-4 text-[var(--amb-a)]" aria-hidden /> : tr.episodeNumber ?? i + 1}</span>
                <Play className="ml-auto hidden size-4 fill-current text-text group-hover:block" aria-hidden />
              </span>
              <span className="min-w-0">
                <span className={`block truncate text-[15px] ${active ? "font-semibold text-[var(--amb-a)]" : "text-text"}`}>{tr.title}</span>
                {(showArtist || (tr.artist && albumArtist && tr.artist !== albumArtist) || showAlbum) && (
                  <span className="block truncate text-[13px] text-text/50">{[showArtist || tr.artist !== albumArtist ? tr.artist : null, showAlbum ? tr.album : null].filter(Boolean).join(" · ")}</span>
                )}
              </span>
              <span className="text-sm text-text/50 tabular">{tr.durationMs ? formatDuration(tr.durationMs, true) : ""}</span>
            </button>
          </li>
        );
      })}
    </ol>
  );
}

const asQueue = (tracks: ItemSummary[], coverId?: string): QueueTrack[] => tracks.map((x) => ({ ...x, coverId: coverId ?? x.parentId }));

/** A music library: albums, artists or every song. */
export function MusicLibrary({ library }: { library: Library }) {
  const { t } = useTranslation();
  const m = useMusic();
  const [tab, setTab] = useState<"album" | "artist" | "track">("album");
  const [sort, setSort] = useState<"title" | "added" | "year" | "random">("title");
  const items = useInfiniteQuery({
    queryKey: ["music", library.id, tab, sort],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => unwrap(await api.GET("/libraries/{libraryId}/items", {
      params: { path: { libraryId: library.id }, query: { kind: tab, sort: tab === "artist" && sort === "year" ? "title" : sort, limit: tab === "track" ? 200 : 60, offset: pageParam } },
    })),
    getNextPageParam: (last, pages) => { const n = pages.reduce((a, p) => a + p.items.length, 0); return n < last.total ? n : undefined; },
  });
  const all = items.data?.pages.flatMap((p) => p.items) ?? [];
  const total = items.data?.pages[0]?.total;
  const shuffleAll = async () => {
    const page = unwrap(await api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId: library.id }, query: { kind: "track", sort: "random", limit: 200 } } }));
    m.play(asQueue(page.items), 0);
  };
  const tabClass = "relative h-11 shrink-0 px-1 text-[15px] font-medium text-text/60 transition-colors hover:text-text after:absolute after:inset-x-0 after:bottom-0 after:h-[2px] after:scale-x-0 after:bg-[var(--amb-a)] after:transition-transform aria-selected:text-text aria-selected:after:scale-x-100";
  return (
    <div className="flex flex-col gap-6 pt-24" data-testid="music-library">
      <header className={`flex flex-col gap-4 border-b border-border ${gutter}`}>
        <div className="flex flex-wrap items-end gap-x-5 gap-y-3">
          <h1 className="font-display text-4xl sm:text-6xl">{library.name}</h1>
          <span className="pb-1 text-sm text-text/55 tabular">{total !== undefined && t(`music.count_${tab}`, { count: total })}</span>
          <div className="ml-auto flex gap-2">
            <Button variant="primary" onClick={() => void shuffleAll()}><Shuffle className="size-4" aria-hidden />{t("music.shuffleAll")}</Button>
          </div>
        </div>
        <div className="-mb-px flex items-center gap-4">
          <div role="tablist" className="flex flex-1 gap-6">
            {(["album", "artist", "track"] as const).map((k) => (
              <button key={k} type="button" role="tab" aria-selected={tab === k} onClick={() => setTab(k)} className={tabClass}>{t(`music.tab_${k}`)}</button>
            ))}
          </div>
          <Select value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} aria-label={t("library.sort")} className="mb-2">
            <option value="title">{t("library.sortTitle")}</option>
            <option value="added">{t("library.sortAdded")}</option>
            {tab !== "artist" && <option value="year">{t("library.sortYear")}</option>}
            <option value="random">{t("music.sortRandom")}</option>
          </Select>
        </div>
      </header>
      <div className={`flex flex-col gap-6 ${gutter}`}>
        <ErrorText error={items.error} />
        {items.isPending && <div className="flex justify-center py-20"><Spinner /></div>}
        {total === 0 && <p className="py-16 text-center text-sm text-muted">{library.scan.state === "idle" ? t("music.empty") : t("library.scanning")}</p>}
        {tab === "track"
          ? <TrackList tracks={asQueue(all)} showAlbum showArtist />
          : (
            <div className={`grid gap-x-5 gap-y-8 ${tab === "artist" ? "grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 2xl:grid-cols-8" : "grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 2xl:grid-cols-7"}`}>
              {all.map((it) => tab === "artist" ? <ArtistCard key={it.id} item={it} /> : <AlbumCard key={it.id} item={it} />)}
            </div>
          )}
        {items.hasNextPage && (
          <Button variant="secondary" className="self-center" disabled={items.isFetchingNextPage} onClick={() => void items.fetchNextPage()}>
            {items.isFetchingNextPage && <Spinner className="size-4" />}{t("library.more")}
          </Button>
        )}
      </div>
    </div>
  );
}

/** An album: big cover in its own light, the facts, and the tracks. */
export function AlbumView({ d }: { d: ItemDetail }) {
  const { t } = useTranslation();
  const m = useMusic();
  const it = d.item;
  useAmbientLight(it.colors);
  const tracks = asQueue(d.children.filter((c) => c.kind === "track"), it.id);
  const total = tracks.reduce((a, x) => a + (x.durationMs ?? 0), 0);
  return (
    <div className="flex flex-col gap-10 pt-24 pb-6" data-testid="album-page">
      <section className={`relative isolate grid gap-8 md:grid-cols-[minmax(0,18rem)_minmax(0,1fr)] md:items-end lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)] ${gutter}`}>
        <div aria-hidden className="absolute inset-x-0 -top-24 -z-10 h-[34rem] bg-[radial-gradient(70%_80%_at_20%_30%,color-mix(in_oklab,var(--amb-a)_32%,transparent),transparent_70%)]" />
        <Cover item={it} className="shadow-[0_40px_80px_-30px_rgb(0_0_0/0.9)]" />
        <div className="flex min-w-0 flex-col gap-4">
          <p className="font-label text-text/55">{t("music.album")}</p>
          <h1 className="font-display text-[clamp(2.4rem,6vw,5.5rem)] text-balance">{it.title}</h1>
          <p className="text-[15px] text-text/70">
            {d.parentId && it.artist ? <Link to="/item/$itemId" params={{ itemId: d.parentId }} className="font-semibold text-text hover:underline">{it.artist}</Link> : it.artist}
            {[it.year, d.genres.slice(0, 2).join(", ") || null, t("music.count_track", { count: tracks.length }), total ? formatDuration(total) : null].filter(Boolean).map((x) => ` · ${x}`)}
          </p>
          <div className="mt-2 flex flex-wrap gap-3">
            <button type="button" onClick={() => m.play(tracks, 0)} disabled={!tracks.length} data-testid="play-album"
              className={`${pressable} ${buttonVariants.primary} tv-focus h-13 rounded-[6px] px-7 text-base font-semibold`}>
              <Play className="size-5 fill-current" aria-hidden />{t("player.play")}
            </button>
            <button type="button" onClick={() => { m.setShuffle(true); m.play(tracks, Math.floor(Math.random() * tracks.length)); }} disabled={!tracks.length}
              className={`${pressable} ${buttonVariants.glass} tv-focus h-13 rounded-[6px] px-6 text-base font-semibold`}>
              <Shuffle className="size-5" aria-hidden />{t("music.shuffle")}
            </button>
            <button type="button" onClick={() => m.enqueue(tracks)} disabled={!tracks.length}
              className={`${pressable} ${buttonVariants.glass} tv-focus h-13 rounded-[6px] px-6 text-base font-semibold`}>
              {t("music.addToQueue")}
            </button>
          </div>
        </div>
      </section>
      <section className={gutter}><TrackList tracks={tracks} albumArtist={it.artist} /></section>
    </div>
  );
}

/** An artist: picture, their albums, and every song to play at once. */
export function ArtistView({ d }: { d: ItemDetail }) {
  const { t } = useTranslation();
  const m = useMusic();
  const it = d.item;
  useAmbientLight(it.colors);
  const albums = d.children.filter((c) => c.kind === "album").sort((a, b) => (b.year ?? 0) - (a.year ?? 0));
  const songs = useQuery({
    queryKey: ["artist-songs", it.id, albums.map((a) => a.id).join()],
    enabled: albums.length > 0,
    queryFn: async () => {
      const lists = await Promise.all(albums.map(async (a) => unwrap(await api.GET("/items/{itemId}", { params: { path: { itemId: a.id } } }))));
      return lists.flatMap((l) => asQueue(l.children.filter((c) => c.kind === "track"), l.item.id));
    },
  });
  return (
    <div className="flex flex-col gap-10 pt-24 pb-6" data-testid="artist-page">
      <section className={`relative isolate flex flex-wrap items-end gap-8 ${gutter}`}>
        <div aria-hidden className="absolute inset-x-0 -top-24 -z-10 h-[30rem] bg-[radial-gradient(70%_80%_at_15%_30%,color-mix(in_oklab,var(--amb-a)_30%,transparent),transparent_70%)]" />
        <Cover item={it} round className="w-44 sm:w-56" />
        <div className="flex min-w-0 flex-col gap-4">
          <p className="font-label text-text/55">{t("music.artist")}</p>
          <h1 className="font-display text-[clamp(2.4rem,6vw,5.5rem)] text-balance">{it.title}</h1>
          <p className="text-[15px] text-text/65">{t("music.count_album", { count: albums.length })}{songs.data ? ` · ${t("music.count_track", { count: songs.data.length })}` : ""}</p>
          <div className="flex flex-wrap gap-3">
            <button type="button" disabled={!songs.data?.length} onClick={() => songs.data && m.play(songs.data, 0)}
              className={`${pressable} ${buttonVariants.primary} tv-focus h-13 rounded-[6px] px-7 text-base font-semibold`}>
              <Play className="size-5 fill-current" aria-hidden />{t("music.playAll")}
            </button>
            <button type="button" disabled={!songs.data?.length} onClick={() => { if (songs.data) { m.setShuffle(true); m.play(songs.data, Math.floor(Math.random() * songs.data.length)); } }}
              className={`${pressable} ${buttonVariants.glass} tv-focus h-13 rounded-[6px] px-6 text-base font-semibold`}>
              <Shuffle className="size-5" aria-hidden />{t("music.shuffle")}
            </button>
          </div>
        </div>
      </section>
      <section className={`flex flex-col gap-4 ${gutter}`}>
        <h2 className="font-head text-xl sm:text-2xl">{t("music.albums")}</h2>
        <div className="grid grid-cols-2 gap-x-5 gap-y-8 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 2xl:grid-cols-7">
          {albums.map((a) => <AlbumCard key={a.id} item={{ ...a, artist: it.title }} />)}
        </div>
      </section>
    </div>
  );
}
