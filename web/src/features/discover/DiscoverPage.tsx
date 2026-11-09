import { useEffect, useState } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Check, Clock, Compass, Download, Play, Search } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, ErrorText, Spinner, pressable } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";

type Title = components["schemas"]["DiscoverTitle"];
type Status = Title["status"];

const gutter = "px-5 sm:px-10 lg:px-16";

function StatusBadge({ status }: { status: Status }) {
  const { t } = useTranslation();
  if (status === "none") return null;
  const tone = status === "available" ? "bg-success text-black" : status === "partial" ? "bg-text text-bg" : "bg-black/75 text-text ring-1 ring-white/25";
  return <span className={`absolute top-2 left-2 rounded-[3px] px-1.5 py-0.5 text-[11px] font-bold tracking-wide uppercase ${tone}`}>{t(`discover.status_${status}`)}</span>;
}

/** One searchable title: poster, name, year, and what can be done with it. */
export function DiscoverCard({ title, canRequest }: { title: Title; canRequest: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [status, setStatus] = useState<Status>(title.status);
  useEffect(() => setStatus(title.status), [title.status]);
  const request = useMutation({
    mutationFn: async () => unwrap(await api.POST("/discover/request", { body: { tmdbId: title.tmdbId, mediaType: title.mediaType } })),
    onSuccess: (r) => { setStatus(r.status); void qc.invalidateQueries({ queryKey: ["discover"] }); },
  });
  return (
    <div className="group flex flex-col gap-2" data-testid="discover-card">
      <div className="poster-lift relative aspect-[2/3] overflow-hidden rounded-[6px] bg-surface-2">
        {title.posterUrl
          ? <img src={title.posterUrl} alt="" loading="lazy" className="absolute inset-0 size-full object-cover" />
          : <span className="absolute inset-0 grid place-items-end p-3 font-display text-xl">{title.title}</span>}
        <StatusBadge status={status} />
        <div className="absolute inset-x-0 bottom-0 flex flex-col gap-2 bg-gradient-to-t from-black/95 via-black/70 to-transparent p-3 pt-16 opacity-0 transition-opacity duration-200 group-hover:opacity-100 group-focus-within:opacity-100 max-md:opacity-100">
          {title.itemId ? (
            <Link to="/item/$itemId" params={{ itemId: title.itemId }} className={`${pressable} h-9 rounded-[6px] bg-text text-sm font-semibold text-bg`}>
              <Play className="size-4 fill-current" aria-hidden />{t("discover.watch")}
            </Link>
          ) : status === "none" && canRequest ? (
            <button type="button" disabled={request.isPending} onClick={() => request.mutate()} data-testid="request"
              className={`${pressable} h-9 rounded-[6px] bg-text text-sm font-semibold text-bg`}>
              {request.isPending ? <Spinner className="size-4" /> : <Download className="size-4" aria-hidden />}
              {t(title.mediaType === "tv" ? "discover.requestSeries" : "discover.requestMovie")}
            </button>
          ) : status !== "none" ? (
            <span className="flex h-9 items-center justify-center gap-2 rounded-[6px] bg-white/15 text-sm font-semibold">
              {status === "available" ? <Check className="size-4" aria-hidden /> : <Clock className="size-4" aria-hidden />}{t(`discover.status_${status}`)}
            </span>
          ) : null}
          <ErrorText error={request.error} />
        </div>
      </div>
      <div className="min-w-0">
        <p className="truncate text-[14px] font-semibold">{title.title}</p>
        <p className="text-[13px] text-text/50 tabular">
          {[title.year, t(title.mediaType === "tv" ? "discover.series" : "discover.movie"), title.rating ? `★ ${title.rating.toFixed(1)}` : null].filter(Boolean).join(" · ")}
        </p>
      </div>
    </div>
  );
}

function Upcoming() {
  const { t, i18n } = useTranslation();
  const up = useQuery({ queryKey: ["upcoming"], queryFn: async () => unwrap(await api.GET("/upcoming", { params: { query: { days: 30 } } })) });
  const list = Array.isArray(up.data) ? up.data : [];
  if (!list.length) return null;
  const day = new Intl.DateTimeFormat(i18n.resolvedLanguage === "hr" ? "hr-HR" : "en-GB", { weekday: "short", day: "numeric", month: "short" });
  return (
    <section className={`flex flex-col gap-4 ${gutter}`} data-testid="upcoming">
      <h2 className="font-head text-xl sm:text-2xl">{t("discover.upcoming")}</h2>
      <ol className="shelf-track flex gap-4 overflow-x-auto pb-4">
        {list.slice(0, 40).map((u, i) => (
          <li key={i} className="flex w-[64vw] shrink-0 gap-3 rounded-[6px] border border-border bg-surface p-3 sm:w-[340px]">
            <div className="relative aspect-[2/3] w-16 shrink-0 overflow-hidden rounded-[4px] bg-surface-2">
              {u.posterUrl && <img src={u.posterUrl} alt="" loading="lazy" className="absolute inset-0 size-full object-cover" />}
            </div>
            <div className="min-w-0">
              <p className="text-[12px] font-semibold tracking-wide text-[var(--amb-a)] uppercase">{day.format(new Date(u.date))}</p>
              <p className="truncate text-[15px] font-semibold">{u.seriesTitle ?? u.title}</p>
              <p className="truncate text-[13px] text-text/60">
                {u.kind === "episode" ? `S${u.season}:E${u.episode} · ${u.title}` : t("discover.movie")}
              </p>
              {u.downloaded && <p className="mt-1 text-[12px] font-semibold text-success">{t("discover.downloaded")}</p>}
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}

export function DiscoverPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { q = "" } = useSearch({ from: "/app/discover" });
  const { data: me } = useMe();
  const [text, setText] = useState(q);
  useEffect(() => setText(q), [q]);
  // Searching follows typing after a short pause, like the search page.
  useEffect(() => {
    const id = window.setTimeout(() => { if (text.trim() !== q) void navigate({ to: "/discover", search: text.trim() ? { q: text.trim() } : {}, replace: true }); }, 350);
    return () => window.clearTimeout(id);
  }, [text]); // eslint-disable-line react-hooks/exhaustive-deps
  const results = useInfiniteQuery({
    queryKey: ["discover", q],
    initialPageParam: 1,
    queryFn: async ({ pageParam }) => unwrap(await api.GET("/discover", { params: { query: { q: q || undefined, page: pageParam } } })),
    getNextPageParam: (last) => (last.page < last.totalPages && last.page < 10 ? last.page + 1 : undefined),
  });
  const first = results.data?.pages[0];
  const titles = results.data?.pages.flatMap((p) => p.titles) ?? [];
  return (
    <div className="flex flex-col gap-10 pt-28 pb-6" data-testid="discover-page">
      <header className={`flex flex-col gap-5 ${gutter}`}>
        <h1 className="flex items-center gap-4 font-display text-4xl sm:text-6xl"><Compass className="size-9 sm:size-12" aria-hidden />{t("discover.title")}</h1>
        <p className="max-w-[60ch] text-text/65">{t("discover.intro")}</p>
        <label className="relative flex max-w-3xl items-center">
          <Search className="pointer-events-none absolute left-5 size-5 text-text/50" aria-hidden />
          <input type="search" value={text} onChange={(e) => setText(e.target.value)} autoFocus placeholder={t("discover.placeholder")} aria-label={t("discover.placeholder")}
            className="h-16 w-full rounded-[8px] border border-border bg-surface pr-5 pl-14 text-xl text-text placeholder:text-text/40 hover:border-border-strong focus:border-text/50" />
        </label>
      </header>

      {first && !first.configured && (
        <div className={gutter}>
          <div className="max-w-3xl rounded-[8px] border border-border bg-surface p-6">
            <p className="text-[15px]">{t("discover.notConfigured")}</p>
            {me?.isAdmin && <Link to="/settings/requests" className="mt-4 inline-flex text-sm font-semibold underline underline-offset-4">{t("discover.setUp")}</Link>}
          </div>
        </div>
      )}
      {!q && <Upcoming />}
      <section className={`flex flex-col gap-4 ${gutter}`}>
        {first?.configured && <h2 className="font-head text-xl sm:text-2xl">{q ? t("discover.resultsFor", { q }) : t("discover.trending")}</h2>}
        <ErrorText error={results.error} />
        {results.isPending && <div className="flex justify-center py-16"><Spinner /></div>}
        {first?.configured && titles.length === 0 && !results.isPending && <p className="py-10 text-text/55">{t("discover.nothing")}</p>}
        <div className="grid grid-cols-2 gap-x-5 gap-y-8 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          {titles.map((x) => <DiscoverCard key={`${x.mediaType}-${x.tmdbId}`} title={x} canRequest={!!first?.canRequest} />)}
        </div>
        {results.hasNextPage && (
          <Button variant="secondary" className="self-center" disabled={results.isFetchingNextPage} onClick={() => void results.fetchNextPage()}>
            {results.isFetchingNextPage && <Spinner className="size-4" />}{t("library.more")}
          </Button>
        )}
      </section>
    </div>
  );
}
