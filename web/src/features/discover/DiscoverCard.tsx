import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Check, Clock, Download, Play } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { ErrorText, Spinner, pressable } from "@/components/ui";

export type Title = components["schemas"]["DiscoverTitle"];
type Status = Title["status"];

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
