import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Captions, Download, Sparkles, X } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, ErrorText, Spinner, SuccessText } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";

type Offer = components["schemas"]["SubtitleOffer"];

/** Search Bazarr's providers for subtitles and download one, or let Bazarr fetch what is missing. */
export function SubtitleFinder({ itemId }: { itemId: string }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const { data: me } = useMe();
  const [open, setOpen] = useState(false);
  const [lang, setLang] = useState("");
  const search = useQuery({
    queryKey: ["subtitle-search", itemId],
    enabled: open,
    retry: false,
    staleTime: 5 * 60_000,
    queryFn: async () => unwrap(await api.GET("/items/{itemId}/subtitles/search", { params: { path: { itemId } } })),
  });
  const download = useMutation({
    mutationFn: async (o: Offer) => unwrap(await api.POST("/items/{itemId}/subtitles/download", { params: { path: { itemId } }, body: o })),
    onSuccess: () => window.setTimeout(() => void qc.invalidateQueries({ queryKey: ["item", itemId] }), 4000),
  });
  const auto = useMutation({ mutationFn: async () => unwrap(await api.POST("/items/{itemId}/subtitles/search-missing", { params: { path: { itemId } } })) });
  if (!me || (!me.isAdmin && !me.permissions.editSubtitles) || me.currentProfile?.isKid) return null;
  const names = new Intl.DisplayNames([i18n.resolvedLanguage ?? "en"], { type: "language" });
  const langName = (code: string) => { try { return names.of(code) ?? code; } catch { return code; } };
  const offers = search.data?.offers ?? [];
  const langs = [...new Set(offers.map((o) => o.language))];
  const shown = lang ? offers.filter((o) => o.language === lang) : offers;
  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)} data-testid="find-subtitles"><Captions className="size-4" aria-hidden />{t("subs.find")}</Button>
      {open && (
        <div role="dialog" aria-modal aria-label={t("subs.find")} className="fixed inset-0 z-50 grid place-items-center bg-black/70 p-4 backdrop-blur-sm" onClick={() => setOpen(false)}>
          <div className="flex max-h-[85vh] w-[min(96vw,760px)] flex-col overflow-hidden rounded-[8px] border border-border bg-surface shadow-2xl" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center gap-3 border-b border-border px-5 py-4">
              <h2 className="font-head text-lg">{t("subs.find")}</h2>
              <button type="button" onClick={() => setOpen(false)} aria-label={t("common.close")} className="ml-auto grid size-8 place-items-center rounded-[6px] hover:bg-white/10"><X className="size-4" aria-hidden /></button>
            </div>
            <div className="flex flex-col gap-4 overflow-y-auto p-5">
              {search.isPending && <div className="flex flex-col items-center gap-3 py-10 text-sm text-text/60"><Spinner />{t("subs.searching")}</div>}
              <ErrorText error={search.error ?? download.error ?? auto.error} />
              {search.data && !search.data.configured && (
                <p className="text-sm text-text/70">{t("subs.notConfigured")} {me.isAdmin && <Link to="/settings/requests" className="font-semibold underline underline-offset-4">{t("subs.setUp")}</Link>}</p>
              )}
              {search.data?.configured && (
                <>
                  <div className="flex flex-wrap items-center gap-2">
                    <Button variant="secondary" disabled={auto.isPending} onClick={() => auto.mutate()}><Sparkles className="size-4" aria-hidden />{t("subs.auto")}</Button>
                    {auto.isSuccess && <SuccessText>{t("subs.autoStarted")}</SuccessText>}
                    {download.isSuccess && <SuccessText>{t("subs.downloaded")}</SuccessText>}
                  </div>
                  {langs.length > 1 && (
                    <div className="flex flex-wrap gap-1.5">
                      {["", ...langs].map((l) => (
                        <button key={l || "all"} type="button" onClick={() => setLang(l)} aria-pressed={lang === l}
                          className="h-8 rounded-[6px] px-3 text-sm text-text/70 ring-1 ring-inset ring-border aria-pressed:bg-text aria-pressed:text-bg">
                          {l ? langName(l) : t("subs.allLanguages")}
                        </button>
                      ))}
                    </div>
                  )}
                  {shown.length === 0 && !search.isPending && <p className="py-6 text-center text-sm text-text/55">{t("subs.none")}</p>}
                  <ul className="flex flex-col divide-y divide-border" data-testid="subtitle-offers">
                    {shown.map((o, i) => (
                      <li key={`${o.provider}-${i}`} className="flex items-center gap-3 py-3">
                        <span className="w-12 shrink-0 text-center text-sm font-semibold tabular text-text/80">{Math.round(o.score)}%</span>
                        <div className="min-w-0 flex-1">
                          <p className="truncate text-[15px] font-semibold">{langName(o.language)}{o.forced ? ` · ${t("library.forced")}` : ""}{o.hearingImpaired ? ` · ${t("subs.hi")}` : ""}</p>
                          <p className="truncate text-[13px] text-text/55">{[o.provider, o.uploader, o.release?.[0]].filter(Boolean).join(" · ")}</p>
                        </div>
                        <Button variant="secondary" disabled={download.isPending} onClick={() => download.mutate(o)} aria-label={t("subs.download")}>
                          {download.isPending && download.variables === o ? <Spinner className="size-4" /> : <Download className="size-4" aria-hidden />}
                        </Button>
                      </li>
                    ))}
                  </ul>
                </>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
