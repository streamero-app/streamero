import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Search } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { ErrorText, Spinner } from "@/components/ui";
import { Page } from "@/components/Page";
import { ItemCard } from "./LibraryPages";

export function SearchPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { q = "" } = useSearch({ from: "/app/search" });
  const results = useQuery({
    queryKey: ["search", q],
    enabled: q.trim().length > 0,
    queryFn: async () => unwrap(await api.GET("/search", { params: { query: { q } } })),
  });
  return (
    <Page className="flex flex-col gap-8">
      <form role="search" className="enter" onSubmit={(e) => {
        e.preventDefault();
        const v = new FormData(e.currentTarget).get("q")?.toString().trim();
        if (v) void navigate({ to: "/search", search: { q: v } });
      }}>
        <label className="group relative flex max-w-2xl items-center">
          <Search className="pointer-events-none absolute left-5 z-10 size-5 text-faint transition-colors group-focus-within:text-accent" aria-hidden />
          <input key={q} name="q" type="search" defaultValue={q} autoFocus={!q} placeholder={t("search.start")} aria-label={t("search.title")}
            className="h-14 w-full rounded-md border border-border bg-surface pr-5 pl-13 text-lg text-text transition-colors placeholder:text-faint hover:border-border-strong focus:border-accent/60" />
        </label>
      </form>
      <div>
        <h1 className="font-display text-4xl leading-none sm:text-5xl">{q ? t("search.resultsFor", { q }) : t("search.title")}</h1>
        {!q && <p className="mt-2 text-sm text-muted">{t("search.hint")}</p>}
      </div>
      <ErrorText error={results.error} />
      {results.isFetching && !results.data && <div className="flex justify-center py-10"><Spinner /></div>}
      {results.data?.length === 0 && <p className="text-sm text-muted">{t("search.none")}</p>}
      <div className="grid grid-cols-[repeat(auto-fill,minmax(132px,1fr))] gap-x-4 gap-y-7 sm:grid-cols-[repeat(auto-fill,minmax(160px,1fr))]" data-testid="search-results">
        {results.data?.map((it) => <ItemCard key={it.id} item={it} />)}
      </div>
    </Page>
  );
}
