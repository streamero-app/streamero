import { useQuery } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { ErrorText } from "@/components/ui";
import { ItemCard } from "./LibraryPages";

export function SearchPage() {
  const { t } = useTranslation();
  const { q = "" } = useSearch({ from: "/app/search" });
  const results = useQuery({
    queryKey: ["search", q],
    enabled: q.trim().length > 0,
    queryFn: async () => unwrap(await api.GET("/search", { params: { query: { q } } })),
  });
  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold">{q ? t("search.resultsFor", { q }) : t("search.title")}</h1>
      <ErrorText error={results.error} />
      {results.data?.length === 0 && <p className="text-sm text-muted">{t("search.none")}</p>}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6" data-testid="search-results">
        {results.data?.map((it) => <ItemCard key={it.id} item={it} />)}
      </div>
    </div>
  );
}
