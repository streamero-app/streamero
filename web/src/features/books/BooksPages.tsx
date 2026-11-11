import { useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { BookOpen, ChevronLeft, Download, Headphones, Moon, Pause, Play, RotateCcw, RotateCw } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, ErrorText, Select, Spinner, buttonVariants, pressable } from "@/components/ui";
import { formatDuration, imageUrl, type ItemSummary, type Library } from "@/features/library/format";
import { ArtFallback } from "@/features/library/cards";
import { useAmbientLight } from "@/features/ambient/Ambient";
import { useMusic, type QueueTrack } from "@/features/music/MusicPlayer";
import { SPEEDS, chapterAt, locate, previousChapterStart, type Part } from "./logic";

type ItemDetail = components["schemas"]["ItemDetail"];
type Manifest = components["schemas"]["AudiobookManifest"];
const gutter = "px-5 sm:px-10 lg:px-16";
const PAGE = 60;

/** A book cover (portrait) or an audiobook cover (square), else the title set on a tinted ground. */
export function BookCover({ item, square = false, className = "" }: { item: ItemSummary; square?: boolean; className?: string }) {
  const [broken, setBroken] = useState(false);
  return (
    <div className={`relative overflow-hidden rounded-[4px] bg-surface-2 shadow-[0_18px_40px_-22px_rgb(0_0_0/0.9)] ${square ? "aspect-square" : "aspect-[2/3]"} ${className}`}>
      {item.hasPoster && !broken
        ? <img src={imageUrl(item.id, "poster")} alt="" loading="lazy" draggable={false} onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover" />
        : <ArtFallback item={item} />}
      {/* the spine: a soft fold on the left edge makes a cover read as a book */}
      {!square && <span aria-hidden className="pointer-events-none absolute inset-y-0 left-0 w-[6%] bg-gradient-to-r from-black/35 via-white/10 to-transparent" />}
    </div>
  );
}

function progressPct(it: ItemSummary): number {
  if (it.played) return 100;
  return Math.round((it.progression ?? 0) * 100);
}

export function BookCard({ item }: { item: ItemSummary }) {
  const { t } = useTranslation();
  const pct = progressPct(item);
  const square = item.kind === "audiobook";
  return (
    <Link to="/item/$itemId" params={{ itemId: item.id }} data-testid="book-card" className="tv-focus group flex flex-col gap-2.5 outline-none">
      <div className="poster-lift relative">
        <BookCover item={item} square={square} />
        {item.seriesIndex !== undefined && item.seriesName && (
          <span className="absolute top-2 right-2 rounded-[3px] bg-black/70 px-1.5 py-0.5 text-[11px] font-semibold text-text tabular backdrop-blur">#{item.seriesIndex}</span>
        )}
        {pct > 0 && (
          <div className="absolute inset-x-0 bottom-0 h-[3px] bg-white/25" aria-label={t("books.progress", { pct })}>
            <div className="h-full bg-[var(--amb-a)]" style={{ width: `${pct}%` }} />
          </div>
        )}
      </div>
      <div className="min-w-0">
        <p className="line-clamp-2 text-[14px] leading-snug font-semibold text-text/90 group-hover:text-text">{item.title}</p>
        <p className="truncate text-[13px] text-text/50">{item.authors?.join(", ") || item.year}</p>
      </div>
    </Link>
  );
}

function BookGrid({ items }: { items: ItemSummary[] }) {
  return (
    <div className="grid grid-cols-2 gap-x-5 gap-y-8 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 2xl:grid-cols-8">
      {items.map((it) => <BookCard key={it.id} item={it} />)}
    </div>
  );
}

type Filter = { author?: string; series?: string };

function useBookPages(library: Library, sort: string, f: Filter) {
  return useInfiniteQuery({
    queryKey: ["books", library.id, sort, f.author ?? "", f.series ?? ""],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => unwrap(await api.GET("/libraries/{libraryId}/items", {
      params: { path: { libraryId: library.id }, query: {
        sort: (f.series ? "series" : sort) as "title", limit: PAGE, offset: pageParam,
        author: f.author, series: f.series,
      } },
    })),
    getNextPageParam: (last, pages) => { const n = pages.reduce((a, p) => a + p.items.length, 0); return n < last.total ? n : undefined; },
  });
}

/** A book or audiobook library: every title, or by author, or by series. */
export function BooksLibrary({ library }: { library: Library }) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"all" | "authors" | "series">("all");
  const [sort, setSort] = useState<"title" | "added" | "series" | "year">("added");
  const [filter, setFilter] = useState<Filter>({});
  const filtered = !!(filter.author || filter.series);
  const items = useBookPages(library, sort, filter);
  const names = useQuery({
    queryKey: ["book-names", library.id, tab],
    enabled: tab !== "all",
    queryFn: async () => tab === "authors"
      ? unwrap(await api.GET("/libraries/{libraryId}/authors", { params: { path: { libraryId: library.id } } }))
      : unwrap(await api.GET("/libraries/{libraryId}/series", { params: { path: { libraryId: library.id } } })),
  });
  const all = items.data?.pages.flatMap((p) => p.items) ?? [];
  const total = items.data?.pages[0]?.total;
  const audio = library.kind === "audiobooks";
  const tabClass = "relative h-11 shrink-0 px-1 text-[15px] font-medium text-text/60 transition-colors hover:text-text after:absolute after:inset-x-0 after:bottom-0 after:h-[2px] after:scale-x-0 after:bg-[var(--amb-a)] after:transition-transform aria-selected:text-text aria-selected:after:scale-x-100";
  const showGrid = tab === "all" || filtered;
  return (
    <div className="flex flex-col gap-6 pt-24" data-testid="books-library">
      <header className={`flex flex-col gap-4 border-b border-border ${gutter}`}>
        <div className="flex flex-wrap items-end gap-x-5 gap-y-3">
          <h1 className="font-display text-4xl sm:text-6xl">{library.name}</h1>
          <span className="pb-1 text-sm text-text/55 tabular">{total !== undefined && !filtered && tab === "all" && t(audio ? "books.count_audiobook" : "books.count_book", { count: total })}</span>
        </div>
        <div className="-mb-px flex items-center gap-4">
          <div role="tablist" className="flex flex-1 gap-6">
            {(["all", "authors", "series"] as const).map((k) => (
              <button key={k} type="button" role="tab" aria-selected={tab === k} className={tabClass}
                onClick={() => { setTab(k); setFilter({}); }}>{t(`books.tab_${k}`)}</button>
            ))}
          </div>
          {showGrid && !filter.series && (
            <Select value={sort} onChange={(e) => setSort(e.target.value as typeof sort)} aria-label={t("library.sort")} className="mb-2">
              <option value="added">{t("library.sortAdded")}</option>
              <option value="title">{t("library.sortTitle")}</option>
              <option value="series">{t("books.sortSeries")}</option>
              <option value="year">{t("library.sortYear")}</option>
            </Select>
          )}
        </div>
      </header>
      <div className={`flex flex-col gap-6 ${gutter}`}>
        {filtered && (
          <div className="flex items-center gap-3">
            <button type="button" onClick={() => setFilter({})} className={`${pressable} ${buttonVariants.glass} tv-focus h-10 rounded-[6px] px-3 text-sm`}>
              <ChevronLeft className="size-4" aria-hidden />{t("library.back")}
            </button>
            <h2 className="font-head text-2xl">{filter.author ?? filter.series}</h2>
          </div>
        )}
        <ErrorText error={items.error ?? names.error} />
        {showGrid && items.isPending && <div className="flex justify-center py-20"><Spinner /></div>}
        {showGrid && total === 0 && <p className="py-16 text-center text-sm text-muted">{library.scan.state === "idle" ? t(audio ? "books.emptyAudio" : "books.empty") : t("library.scanning")}</p>}
        {showGrid && <BookGrid items={all} />}
        {showGrid && items.hasNextPage && (
          <Button variant="secondary" className="self-center" disabled={items.isFetchingNextPage} onClick={() => void items.fetchNextPage()}>
            {items.isFetchingNextPage && <Spinner className="size-4" />}{t("library.more")}
          </Button>
        )}
        {!showGrid && (
          <ul className="grid grid-cols-2 gap-x-5 gap-y-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6" data-testid="book-names">
            {names.isPending && <li><Spinner /></li>}
            {(names.data ?? []).map((n) => (
              <li key={n.name}>
                <button type="button" onClick={() => setFilter(tab === "authors" ? { author: n.name } : { series: n.name })}
                  className="tv-focus group flex w-full flex-col gap-2.5 text-left outline-none">
                  {n.itemId
                    ? <div className="poster-lift"><BookCover item={{ id: n.itemId, kind: "book", title: n.name, addedAt: "", hasPoster: true }} square={audio} /></div>
                    : null}
                  <span>
                    <span className="block truncate text-[14px] font-semibold text-text/90 group-hover:text-text">{n.name}</span>
                    <span className="block text-[13px] text-text/50 tabular">{t(audio ? "books.count_audiobook" : "books.count_book", { count: n.count })}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

/** More titles by the same author or in the same series. */
function RelatedRow({ libraryId, title, filter, exclude }: { libraryId: string; title: string; filter: Filter; exclude: string }) {
  const q = useQuery({
    queryKey: ["books-related", libraryId, filter.author ?? "", filter.series ?? ""],
    queryFn: async () => unwrap(await api.GET("/libraries/{libraryId}/items", {
      params: { path: { libraryId }, query: { author: filter.author, series: filter.series, sort: "series", limit: 24 } },
    })),
  });
  const items = (q.data?.items ?? []).filter((x) => x.id !== exclude);
  if (!items.length) return null;
  return (
    <section className={`flex flex-col gap-4 ${gutter}`}>
      <h2 className="font-head text-xl sm:text-2xl">{title}</h2>
      <BookGrid items={items} />
    </section>
  );
}

/** Builds the audiobook queue and moves around the whole book. */
export function useAudiobook(item: ItemSummary) {
  const m = useMusic();
  const manifest = useQuery({
    queryKey: ["listen", item.id],
    queryFn: async () => unwrap(await api.GET("/items/{itemId}/listen", { params: { path: { itemId: item.id } } })),
  });
  const active = m.current?.itemId === item.id;
  const position = active ? (m.current?.offsetMs ?? 0) + m.position : (manifest.data?.positionMs ?? 0);
  const entries = (mf: Manifest): QueueTrack[] => mf.parts.map((p: Part) => ({
    ...item, id: `${item.id}:${p.fileId}`, kind: "track", title: mf.parts.length > 1 ? p.title : item.title,
    artist: item.authors?.join(", "), album: item.title, coverId: item.hasPoster ? item.id : undefined,
    itemId: item.id, fileId: p.fileId, offsetMs: p.offsetMs, durationMs: p.durationMs, bookDurationMs: mf.durationMs,
  }));
  const playAt = (ms: number) => {
    const mf = manifest.data;
    if (!mf?.parts.length) return;
    const { index, localMs } = locate(mf.parts, ms);
    if (active && m.current?.fileId === mf.parts[index]!.fileId) {
      m.seek(localMs);
      if (!m.playing) m.toggle();
      return;
    }
    if (m.shuffle) m.setShuffle(false);
    const list = entries(mf);
    list[index] = { ...list[index]!, startMs: localMs };
    m.play(list, index);
  };
  return { manifest, active, position, playAt, m };
}

function clock(ms: number): string {
  return formatDuration(Math.max(1000, ms), true);
}

/** Listening controls on the audiobook page: chapters, ±30 s, speed and sleep timer. */
function AudiobookPanel({ item }: { item: ItemSummary }) {
  const { t } = useTranslation();
  const { manifest, active, position, playAt, m } = useAudiobook(item);
  const mf = manifest.data;
  if (manifest.error) return <ErrorText error={manifest.error} />;
  if (!mf) return <Spinner />;
  const chapters = mf.chapters;
  const cur = chapterAt(chapters, position);
  const started = position > 0;
  const sleepValue = m.sleep.kind === "chapter" ? "chapter" : m.sleep.kind === "at" ? "on" : "off";
  return (
    <div className="flex flex-col gap-6" data-testid="audiobook-panel">
      <div className="flex flex-wrap items-center gap-3">
        <button type="button" data-testid="listen" onClick={() => (active ? m.toggle() : playAt(position))}
          className={`${pressable} ${buttonVariants.primary} tv-focus h-13 rounded-[6px] px-7 text-base font-semibold`}>
          {active && m.playing ? <Pause className="size-5 fill-current" aria-hidden /> : <Headphones className="size-5" aria-hidden />}
          {active && m.playing ? t("player.pause") : started ? t("books.continueListening", { time: clock(position) }) : t("books.listen")}
        </button>
        <button type="button" aria-label={t("books.back30")} onClick={() => playAt(Math.max(0, position - 30000))}
          className={`${pressable} ${buttonVariants.glass} tv-focus grid size-13 place-items-center rounded-[6px]`}><RotateCcw className="size-5" aria-hidden /></button>
        <button type="button" aria-label={t("books.forward30")} onClick={() => playAt(Math.min(mf.durationMs - 1000, position + 30000))}
          className={`${pressable} ${buttonVariants.glass} tv-focus grid size-13 place-items-center rounded-[6px]`}><RotateCw className="size-5" aria-hidden /></button>
        <label className="flex items-center gap-2 text-sm text-text/70">
          {t("books.speed")}
          <Select value={String(m.rate)} onChange={(e) => m.setRate(Number(e.target.value))} data-testid="speed">
            {SPEEDS.map((s) => <option key={s} value={s}>{s}×</option>)}
          </Select>
        </label>
        <label className="flex items-center gap-2 text-sm text-text/70">
          <Moon className="size-4" aria-hidden /><span className="sr-only">{t("books.sleep")}</span>
          <Select value={sleepValue} data-testid="sleep" onChange={(e) => {
            const v = e.target.value;
            if (v === "off") m.setSleep({ kind: "off" });
            else if (v === "chapter") m.setSleep({ kind: "chapter", endMs: chapters[cur]?.endMs ?? mf.durationMs });
            else if (v !== "on") m.setSleep({ kind: "at", at: Date.now() + Number(v) * 60000 });
          }}>
            <option value="off">{t("books.sleepOff")}</option>
            {sleepValue === "on" && <option value="on">{t("books.sleepOn")}</option>}
            {[15, 30, 45, 60].map((n) => <option key={n} value={n}>{t("books.sleepMinutes", { count: n })}</option>)}
            <option value="chapter">{t("books.sleepChapter")}</option>
          </Select>
        </label>
      </div>
      <div className="flex items-center gap-3 text-sm text-text/60 tabular">
        <span>{clock(position)}</span>
        <div className="h-1 flex-1 rounded-full bg-white/15">
          <div className="h-full rounded-full bg-[var(--amb-a)]" style={{ width: `${Math.min(100, (position / Math.max(1, mf.durationMs)) * 100)}%` }} />
        </div>
        <span>{clock(mf.durationMs)}</span>
      </div>
      {chapters.length > 1 && (
        <section className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <h2 className="font-head text-xl">{t("books.chapters")}</h2>
            <button type="button" className="text-sm text-text/60 hover:text-text" onClick={() => playAt(previousChapterStart(chapters, position))}>{t("books.chapterStart")}</button>
          </div>
          <ol className="flex flex-col" data-testid="chapter-list">
            {chapters.map((c, i) => (
              <li key={`${c.startMs}-${i}`}>
                <button type="button" onClick={() => playAt(c.startMs)}
                  className={`tv-focus grid w-full grid-cols-[2.25rem_minmax(0,1fr)_auto] items-center gap-3 rounded-[6px] px-3 py-2.5 text-left hover:bg-white/[0.05] ${i === cur && started ? "bg-white/[0.06]" : ""}`}>
                  <span className="text-right text-sm text-text/45 tabular">{i === cur && active && m.playing ? <Play className="ml-auto size-4 fill-current text-[var(--amb-a)]" aria-hidden /> : i + 1}</span>
                  <span className={`truncate text-[15px] ${i === cur && started ? "font-semibold text-[var(--amb-a)]" : "text-text"}`}>{c.title}</span>
                  <span className="text-sm text-text/50 tabular">{clock(c.endMs - c.startMs)}</span>
                </button>
              </li>
            ))}
          </ol>
        </section>
      )}
    </div>
  );
}

/** A book or audiobook page: cover, facts, description, then read or listen. */
export function BookView({ d }: { d: ItemDetail }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const it = d.item;
  const b = d.book;
  useAmbientLight(it.colors);
  const audio = it.kind === "audiobook";
  const pct = progressPct(it);
  const download = async () => {
    const mf = unwrap(await api.GET("/items/{itemId}/read", { params: { path: { itemId: it.id } } }));
    window.location.assign(mf.fileUrl);
  };
  const markRead = async (played: boolean) => {
    await api.PUT("/items/{itemId}/user-data", { params: { path: { itemId: it.id } }, body: { played } });
    void qc.invalidateQueries({ queryKey: ["item", it.id] });
  };
  const facts = [
    b?.seriesName ? `${b.seriesName}${b.seriesIndex !== undefined ? ` #${b.seriesIndex}` : ""}` : null,
    it.year,
    b?.pageCount ? t("books.pages", { count: b.pageCount }) : null,
    audio && it.durationMs ? formatDuration(it.durationMs) : null,
    b?.format && !audio ? b.format.toUpperCase() : null,
    b?.publisher, b?.language?.toUpperCase(),
  ].filter(Boolean);
  return (
    <div className="flex flex-col gap-12 pt-24 pb-6" data-testid="book-page">
      <section className={`relative isolate grid gap-8 md:grid-cols-[minmax(0,16rem)_minmax(0,1fr)] lg:grid-cols-[minmax(0,19rem)_minmax(0,1fr)] ${gutter}`}>
        <div aria-hidden className="absolute inset-x-0 -top-24 -z-10 h-[34rem] bg-[radial-gradient(70%_80%_at_20%_30%,color-mix(in_oklab,var(--amb-a)_28%,transparent),transparent_70%)]" />
        <BookCover item={it} square={audio} className="w-48 md:w-full" />
        <div className="flex min-w-0 flex-col gap-4">
          <p className="font-label text-text/55">{t(audio ? "library.kind_audiobook" : "library.kind_book")}</p>
          <h1 className="font-display text-[clamp(2.2rem,5vw,4.5rem)] leading-[1.02] text-balance">{it.title}</h1>
          {!!b?.authors.length && <p className="text-[17px] text-text/85">{t("books.by", { names: b.authors.join(", ") })}</p>}
          {!!b?.narrators?.length && <p className="text-[15px] text-text/65">{t("books.narratedBy", { names: b.narrators.join(", ") })}</p>}
          {facts.length > 0 && <p className="text-[14px] text-text/55">{facts.join(" · ")}</p>}
          {!audio && (
            <div className="mt-2 flex flex-wrap gap-3">
              <Link to="/read/$itemId" params={{ itemId: it.id }} data-testid="read"
                className={`${pressable} ${buttonVariants.primary} tv-focus h-13 rounded-[6px] px-7 text-base font-semibold`}>
                <BookOpen className="size-5" aria-hidden />{pct > 0 && pct < 100 ? t("books.continueReading", { pct }) : t("books.read")}
              </Link>
              <button type="button" onClick={() => void download()} className={`${pressable} ${buttonVariants.glass} tv-focus h-13 rounded-[6px] px-6 text-base font-semibold`}>
                <Download className="size-5" aria-hidden />{t("books.download")}
              </button>
              <button type="button" onClick={() => void markRead(!it.played)} className={`${pressable} ${buttonVariants.glass} tv-focus h-13 rounded-[6px] px-6 text-base font-semibold`}>
                {it.played ? t("books.markUnread") : t("books.markRead")}
              </button>
            </div>
          )}
          {audio && <AudiobookPanel item={it} />}
          {d.overview && <p className="max-w-[68ch] text-[15px] leading-relaxed whitespace-pre-line text-text/75">{d.overview}</p>}
          {!!d.genres.length && <p className="text-[13px] text-text/50">{d.genres.join(" · ")}</p>}
          {b?.isbn && <p className="text-[12px] text-text/40 tabular">ISBN {b.isbn}</p>}
        </div>
      </section>
      {b?.seriesName && <RelatedRow libraryId={d.libraryId} title={t("books.moreInSeries", { name: b.seriesName })} filter={{ series: b.seriesName }} exclude={it.id} />}
      {b?.authors[0] && <RelatedRow libraryId={d.libraryId} title={t("books.moreBy", { name: b.authors[0] })} filter={{ author: b.authors[0] }} exclude={it.id} />}
    </div>
  );
}
