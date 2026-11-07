import { useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Card, ErrorText } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { channelsLabel, formatDuration, formatSize, imageUrl, placeholderHue, resolutionLabel, type ItemSummary } from "./format";

const PAGE = 60;

export function useLibraries() {
  return useQuery({ queryKey: ["libraries"], queryFn: async () => unwrap(await api.GET("/libraries")) });
}

/** Poster card; a coloured placeholder when there is no artwork. */
export function ItemCard({ item }: { item: ItemSummary }) {
  const hue = placeholderHue(item.title);
  return (
    <Link to="/item/$itemId" params={{ itemId: item.id }} className="group flex flex-col gap-2" data-testid="item-card">
      {item.hasPoster ? (
        <img src={imageUrl(item.id, "poster")} alt="" loading="lazy" decoding="async"
          className="aspect-[2/3] w-full rounded-card border border-border object-cover transition group-hover:border-accent" />
      ) : (
        <div className="flex aspect-[2/3] items-end overflow-hidden rounded-card border border-border p-3 transition group-hover:border-accent"
          style={{ background: `linear-gradient(160deg, hsl(${hue} 45% 28%), hsl(${(hue + 40) % 360} 35% 12%))` }}>
          <span className="line-clamp-3 text-lg leading-tight font-semibold text-white drop-shadow">{item.title}</span>
        </div>
      )}
      {item.positionMs && item.durationMs ? (
        <div className="-mt-1 h-1 overflow-hidden rounded-full bg-surface-2" aria-hidden>
          <div className="h-full bg-accent" style={{ width: `${Math.min(100, (item.positionMs / item.durationMs) * 100)}%` }} />
        </div>
      ) : null}
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{item.played ? "✓ " : ""}{item.title}</p>
        <p className="text-xs text-muted">
          {item.year ?? "—"}{item.rating ? ` · ★ ${item.rating.toFixed(1)}` : ""}
        </p>
      </div>
    </Link>
  );
}

/** "Continue watching" row on the home page. */
export function ContinueShelf() {
  const { t } = useTranslation();
  const items = useQuery({ queryKey: ["continue"], queryFn: async () => unwrap(await api.GET("/home/continue")) });
  if (!items.data?.length) return null;
  return (
    <section className="flex flex-col gap-3" data-testid="continue">
      <h2 className="text-xl font-semibold">{t("home.continue")}</h2>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
        {items.data.map((it) => <ItemCard key={it.id} item={it} />)}
      </div>
    </section>
  );
}

/** Libraries on the home page. */
export function LibraryShelf() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const libraries = useLibraries();
  if (libraries.data?.length === 0) {
    return (
      <Card title={t("home.librariesTitle")}>
        <p className="mb-3 text-sm text-muted">{me?.isAdmin ? t("home.noLibrariesAdmin") : t("home.noLibraries")}</p>
        {me?.isAdmin && <Link to="/settings/libraries" className="text-sm font-medium text-accent">{t("libraries.add")} →</Link>}
      </Card>
    );
  }
  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-xl font-semibold">{t("home.librariesTitle")}</h2>
      <ErrorText error={libraries.error} />
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {libraries.data?.map((l) => (
          <Link key={l.id} to="/library/$libraryId" params={{ libraryId: l.id }} data-testid="library-card"
            className="rounded-card border border-border bg-surface p-5 transition hover:border-accent">
            <p className="text-lg font-semibold">{l.name}</p>
            <p className="text-sm text-muted">{t(`libraries.kind_${l.kind}`)} · {t("libraries.items", { count: l.itemCount })}</p>
          </Link>
        ))}
      </div>
    </section>
  );
}

export function LibraryPage() {
  const { t } = useTranslation();
  const { libraryId } = useParams({ from: "/app/library/$libraryId" });
  const libraries = useLibraries();
  const lib = libraries.data?.find((l) => l.id === libraryId);
  const [sort, setSort] = useState<"title" | "year" | "added">("title");
  const items = useInfiniteQuery({
    queryKey: ["library-items", libraryId, sort],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) =>
      unwrap(await api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId }, query: { limit: PAGE, offset: pageParam, sort } } })),
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
  });
  const all = items.data?.pages.flatMap((p) => p.items) ?? [];
  const total = items.data?.pages[0]?.total;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex-1">
          <h1 className="text-3xl font-semibold">{lib?.name ?? "…"}</h1>
          {total !== undefined && <p className="text-sm text-muted">{t("libraries.items", { count: total })}</p>}
        </div>
        <label className="flex items-center gap-2 text-sm text-muted">
          {t("library.sort")}
          <select value={sort} onChange={(e) => setSort(e.target.value as typeof sort)}
            className="rounded-md border border-border bg-surface px-2 py-1 text-text">
            <option value="title">{t("library.sortTitle")}</option>
            <option value="year">{t("library.sortYear")}</option>
            <option value="added">{t("library.sortAdded")}</option>
          </select>
        </label>
      </div>
      <ErrorText error={items.error} />
      {total === 0 && (
        <p className="text-sm text-muted">{lib && ["queued", "scanning"].includes(lib.scan.state) ? t("library.scanning") : t("library.empty")}</p>
      )}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
        {all.map((it) => <ItemCard key={it.id} item={it} />)}
      </div>
      {items.hasNextPage && (
        <Button variant="secondary" className="self-center" disabled={items.isFetchingNextPage} onClick={() => void items.fetchNextPage()}>
          {t("library.more")}
        </Button>
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

export function ItemPage() {
  const { t } = useTranslation();
  const { itemId } = useParams({ from: "/app/item/$itemId" });
  const detail = useQuery({
    queryKey: ["item", itemId],
    queryFn: async () => unwrap(await api.GET("/items/{itemId}", { params: { path: { itemId } } })),
  });
  const d = detail.data;
  if (detail.error) return <ErrorText error={detail.error} />;
  if (!d) return <p role="status" className="text-muted">…</p>;
  const it = d.item;
  const back = d.parentId
    ? <Link to="/item/$itemId" params={{ itemId: d.parentId }} className="text-sm text-muted hover:text-text">← {t("library.back")}</Link>
    : <Link to="/library/$libraryId" params={{ libraryId: d.libraryId }} className="text-sm text-muted hover:text-text">← {t("library.back")}</Link>;

  const posterType = it.kind === "episode" ? "still" : "poster";
  const directors = d.crew.filter((p) => p.role === "Director").map((p) => p.name);
  const writers = d.crew.filter((p) => p.role !== "Director").map((p) => p.name);
  return (
    <div className="flex flex-col gap-6">
      {back}
      <section className="relative overflow-hidden rounded-card border border-border" data-testid="item-hero">
        {d.hasBackdrop && (
          <img src={imageUrl(it.id, "backdrop")} alt="" className="absolute inset-0 h-full w-full object-cover opacity-30" />
        )}
        <div className="relative flex flex-col gap-5 bg-gradient-to-r from-bg via-bg/90 to-bg/40 p-5 sm:flex-row sm:p-8">
          {it.hasPoster && (
            <img src={imageUrl(it.id, posterType)} alt=""
              className={`shrink-0 rounded-card border border-border object-cover shadow-2xl ${posterType === "still" ? "w-72 aspect-video" : "w-40 aspect-[2/3] sm:w-52"}`} />
          )}
          <div className="flex min-w-0 flex-col gap-3">
            <h1 className="text-3xl font-semibold sm:text-4xl">{it.kind === "episode" ? `${episodeLabel(it)} · ${it.title}` : it.title}</h1>
            {d.originalTitle && d.originalTitle !== it.title && <p className="text-sm text-muted">{d.originalTitle}</p>}
            <p className="flex flex-wrap items-center gap-2 text-sm text-muted">
              {[it.year, t(`library.kind_${it.kind}`), formatDuration(it.durationMs)].filter(Boolean).join(" · ")}
              {it.rating ? <Badge tone="accent">★ {it.rating.toFixed(1)}</Badge> : null}
              {d.officialRating && <Badge>{d.officialRating}</Badge>}
            </p>
            {d.genres.length > 0 && <p className="flex flex-wrap gap-1">{d.genres.map((g) => <Badge key={g}>{g}</Badge>)}</p>}
            {d.tagline && <p className="text-lg italic">{d.tagline}</p>}
            {d.overview && <p className="max-w-3xl leading-relaxed" data-testid="overview">{d.overview}</p>}
            {directors.length > 0 && <p className="text-sm"><span className="text-muted">{t("library.directedBy")}:</span> {directors.join(", ")}</p>}
            {writers.length > 0 && <p className="text-sm"><span className="text-muted">{t("library.writtenBy")}:</span> {writers.join(", ")}</p>}
            {d.metadataError && <p className="text-xs text-muted">{t("library.noMetadata")}</p>}
            {d.files.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-2">
                <Link to="/watch/$itemId" params={{ itemId: it.id }} data-testid="play"
                  className="rounded-md bg-accent px-5 py-2.5 font-semibold text-accent-contrast hover:opacity-90">
                  ▶ {it.positionMs ? t("player.resume", { time: formatClock(it.positionMs) }) : t("player.play")}
                </Link>
                {it.positionMs ? (
                  <Link to="/watch/$itemId" params={{ itemId: it.id }} search={{ start: 0 }}
                    className="rounded-md bg-surface-2 px-4 py-2.5 text-sm hover:bg-border">{t("player.fromStart")}</Link>
                ) : null}
              </div>
            )}
          </div>
        </div>
      </section>

      {d.cast.length > 0 && (
        <Card title={t("library.cast")}>
          <ul className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-3 lg:grid-cols-4" data-testid="cast">
            {d.cast.map((p) => (
              <li key={p.name + (p.role ?? "")} className="min-w-0">
                <p className="truncate font-medium">{p.name}</p>
                {p.role && <p className="truncate text-xs text-muted">{p.role}</p>}
              </li>
            ))}
          </ul>
        </Card>
      )}

      {d.children.length > 0 && (
        <Card title={it.kind === "series" ? t("library.seasons") : t("library.episodes")}>
          <ul className="divide-y divide-border">
            {d.children.map((c) => (
              <li key={c.id}>
                <Link to="/item/$itemId" params={{ itemId: c.id }} className="flex items-center gap-3 py-3 hover:text-accent" data-testid="child-row">
                  {c.kind === "episode" && <span className="w-16 shrink-0 font-mono text-xs text-muted">{episodeLabel(c)}</span>}
                  <span className="flex-1">{c.kind === "season" && c.seasonNumber === 0 ? t("library.specials") : c.kind === "season" ? t("library.season", { n: c.seasonNumber }) : c.title}</span>
                  <span className="text-xs text-muted">{formatDuration(c.durationMs)}</span>
                </Link>
              </li>
            ))}
          </ul>
        </Card>
      )}

      {d.files.length > 0 && (
        <Card title={d.files.length > 1 ? t("library.versions", { count: d.files.length }) : t("library.file")}>
          <ul className="flex flex-col gap-4">
            {d.files.map((f) => (
              <li key={f.id} className="flex flex-col gap-2 text-sm" data-testid="media-file">
                <div className="flex flex-wrap items-center gap-2">
                  {f.label && <span className="font-medium">{f.label}</span>}
                  {resolutionLabel(f.width, f.height) && <Badge tone="accent">{resolutionLabel(f.width, f.height)}</Badge>}
                  {f.hdr && <Badge tone="success">{f.hdr}</Badge>}
                  {f.videoCodec && <Badge>{f.videoCodec.toUpperCase()}</Badge>}
                  <Badge>{f.container.toUpperCase()}</Badge>
                  <span className="text-xs text-muted">{formatSize(f.size)} · {formatDuration(f.durationMs)}</span>
                </div>
                {f.audio && f.audio.length > 0 && (
                  <p className="text-xs text-muted">
                    {t("library.audio")}: {f.audio.map((a) => [a.language?.toUpperCase(), a.codec.toUpperCase(), channelsLabel(a.channels), a.title].filter(Boolean).join(" ")).join(" · ")}
                  </p>
                )}
                {f.subtitles && f.subtitles.length > 0 && (
                  <p className="text-xs text-muted">
                    {t("library.subtitles")}: {f.subtitles.map((s) => [s.language?.toUpperCase() ?? "?", s.forced ? t("library.forced") : "", s.external ? t("library.external") : ""].filter(Boolean).join(" ")).join(" · ")}
                  </p>
                )}
                <code className="truncate text-xs text-muted" title={f.path}>{f.path}</code>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </div>
  );
}
