import { useEffect, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Compass, Search } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Button, ErrorText, Spinner } from "@/components/ui";
import { canAdminister, useMe } from "@/features/auth/useAuth";
import { DiscoverCard } from "./DiscoverCard";

const gutter = "px-5 sm:px-10 lg:px-16";

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
                {u.kind === "episode" ? `${t("common.episodeCode", { season: u.season, episode: u.episode })} · ${u.title}` : t("discover.movie")}
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
            {canAdminister(me) && <Link to="/settings/requests" className="mt-4 inline-flex text-sm font-semibold underline underline-offset-4">{t("discover.setUp")}</Link>}
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
