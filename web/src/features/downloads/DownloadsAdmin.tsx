import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Badge, Button, Card, ErrorText, IconButton, Select, Spinner, SuccessText } from "@/components/ui";
import { formatSize } from "@/features/library/format";
import { SettingRow } from "@/features/settings/SettingRow";

type Settings = components["schemas"]["DownloadSettings"];

const KEY = ["admin", "downloads"];
const num = "h-10 w-full rounded-[6px] border border-border bg-black/30 px-3 text-[15px] text-text tabular hover:border-border-strong focus:border-accent/70";

/** Offline downloads for the apps: limits of the download transcodes and what is in the cache (D-040). */
export function DownloadsAdmin() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: KEY,
    queryFn: async () => unwrap(await api.GET("/admin/downloads")),
    refetchInterval: (query) => ((query.state.data?.running ?? 0) + (query.state.data?.queued ?? 0) > 0 ? 3000 : false),
  });
  const [draft, setDraft] = useState<Settings>({ maxTranscodes: 1, cacheLimitGb: 50, keepDays: 7 });
  useEffect(() => { if (q.data?.settings) setDraft(q.data.settings); }, [q.data]);
  const save = useMutation({
    mutationFn: async (body: Settings) => unwrap(await api.PUT("/settings/downloads", { body })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  });
  const remove = useMutation({
    mutationFn: async (transcodeId: string) => unwrap(await api.DELETE("/admin/downloads/{transcodeId}", { params: { path: { transcodeId } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  });
  if (q.isPending) return <Spinner />;
  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => { save.reset(); setDraft({ ...draft, [k]: v }); };
  const d = q.data;
  return (
    <form data-testid="downloads-admin" onSubmit={(e) => { e.preventDefault(); save.mutate(draft); }}>
      <Card title={t("downloads.adminTitle")} actions={d?.settings && (
        <Badge tone={d.running > 0 ? "success" : "muted"}>{t("downloads.adminStatus", { running: d.running, queued: d.queued })}</Badge>
      )}>
        <p className="mb-4 max-w-prose text-sm text-muted">{t("downloads.adminIntro")}</p>
        <SettingRow label={t("downloads.maxTranscodes")} hint={t("downloads.maxTranscodesHint")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={draft.maxTranscodes} onChange={(e) => set("maxTranscodes", Number(e.target.value))}>
              {[1, 2, 3, 4, 6, 8, 12, 16].map((v) => <option key={v} value={v}>{v}</option>)}
            </Select>
          )}
        </SettingRow>
        <SettingRow label={t("downloads.cacheLimit")} hint={t("downloads.cacheLimitHint", { used: formatSize(d?.cacheBytes ?? 0) })}>
          {(id) => <input id={id} type="number" min={1} max={100000} className={num} value={draft.cacheLimitGb}
            onChange={(e) => set("cacheLimitGb", Math.max(1, Number(e.target.value) || 1))} />}
        </SettingRow>
        <SettingRow label={t("downloads.keepDays")} hint={t("downloads.keepDaysHint")}>
          {(id) => <input id={id} type="number" min={1} max={90} className={num} value={draft.keepDays}
            onChange={(e) => set("keepDays", Math.min(90, Math.max(1, Number(e.target.value) || 1)))} />}
        </SettingRow>
        <div className="mt-5 flex flex-wrap items-center gap-4">
          <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
          {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
          <ErrorText error={save.error ?? q.error ?? remove.error} />
        </div>

        <h3 className="mt-8 mb-3 font-head text-lg">{t("downloads.transcodes")}</h3>
        {!d?.transcodes?.length ? <p className="text-sm text-muted">{t("downloads.noTranscodes")}</p> : (
          <ul className="flex flex-col divide-y divide-border rounded-[6px] border border-border">
            {d.transcodes.map((x) => (
              <li key={x.id} className="flex flex-wrap items-center gap-3 px-4 py-3 text-sm" data-testid="download-transcode">
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-semibold">{x.title}</span>
                  <span className="block text-xs text-muted">
                    {t(`downloads.quality_${x.quality}`)} · {x.users.join(", ") || "—"}{x.sizeBytes > 0 ? ` · ${formatSize(x.sizeBytes)}` : ""}
                  </span>
                  {x.error && <span className="block text-xs text-danger">{x.error}</span>}
                </span>
                <Badge tone={x.state === "ready" ? "success" : x.state === "failed" ? "danger" : "accent"}>
                  {x.state === "transcoding" ? t("downloads.state_transcodingPct", { pct: Math.round(x.progress * 100) }) : t(`downloads.state_${x.state}`)}
                </Badge>
                <IconButton type="button" label={t("downloads.removeTranscode")} onClick={() => remove.mutate(x.id)} disabled={remove.isPending}>
                  <Trash2 className="size-4" aria-hidden />
                </IconButton>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </form>
  );
}
