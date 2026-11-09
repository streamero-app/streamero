import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, Card, ErrorText, Select, Spinner, SuccessText } from "@/components/ui";
import { SettingRow, Toggle } from "./SettingRow";
import { useDateTime } from "./SettingsLayout";

type Encoding = components["schemas"]["EncodingSettings"];
type Resume = components["schemas"]["ResumeSettings"];
type Branding = components["schemas"]["BrandingSettings"];

const num = "h-10 w-full rounded-[6px] border border-border bg-black/30 px-3 text-[15px] text-text tabular hover:border-border-strong focus:border-accent/70";

/** A settings document loaded from GET and saved with PUT on the same path. */
function useSettingsDoc<T>(path: "/settings/encoding" | "/settings/resume" | "/settings/branding", fallback: T) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["settings", path], queryFn: async () => unwrap(await api.GET(path)) as T });
  const [draft, setDraft] = useState<T>(fallback);
  useEffect(() => { if (q.data) setDraft(q.data); }, [q.data]);
  const save = useMutation({
    mutationFn: async (body: T) => unwrap(await api.PUT(path, { body } as never)) as T,
    onSuccess: (d) => { qc.setQueryData(["settings", path], d); void qc.invalidateQueries({ queryKey: ["system", "info"] }); },
  });
  return { q, draft, setDraft, save };
}

function Saved({ save, error }: { save: { isPending: boolean; isSuccess: boolean; error: unknown }; error?: unknown }) {
  const { t } = useTranslation();
  return (
    <div className="mt-5 flex flex-wrap items-center gap-4">
      <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
      {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
      <ErrorText error={save.error ?? error} />
    </div>
  );
}

/** Transcoding (hardware, quality, limits) and when titles count as started or watched. */
export function TranscodingSettings() {
  const { t } = useTranslation();
  const enc = useSettingsDoc<Encoding>("/settings/encoding", {
    hardwareAcceleration: "none", preset: "veryfast", quality: 22, audioBitrateKbps: 192, segmentSeconds: 6, maxSessions: 0, toneMapping: true,
  });
  const res = useSettingsDoc<Resume>("/settings/resume", { minResumePercent: 5, maxResumePercent: 90, minResumeDurationSeconds: 300 });
  const e = enc.draft;
  const setE = <K extends keyof Encoding>(k: K, v: Encoding[K]) => enc.setDraft({ ...e, [k]: v });
  const r = res.draft;
  const setR = <K extends keyof Resume>(k: K, v: Resume[K]) => res.setDraft({ ...r, [k]: v });
  if (enc.q.isPending || res.q.isPending) return <Spinner />;
  return (
    <div className="flex flex-col gap-6">
      <form onSubmit={(ev) => { ev.preventDefault(); enc.save.reset(); enc.save.mutate(e); }}>
        <Card title={t("transcoding.title")}>
          <p className="mb-4 text-sm text-muted">{t("transcoding.intro")}</p>
          <SettingRow label={t("transcoding.hardware")} hint={t(`transcoding.hardware_${e.hardwareAcceleration}_hint`)}>
            {(id) => (
              <Select id={id} className="h-10 w-full" value={e.hardwareAcceleration} onChange={(x) => setE("hardwareAcceleration", x.target.value as Encoding["hardwareAcceleration"])}>
                {(["none", "vaapi", "qsv", "nvenc"] as const).map((v) => <option key={v} value={v}>{t(`transcoding.hardware_${v}`)}</option>)}
              </Select>
            )}
          </SettingRow>
          {e.hardwareAcceleration === "vaapi" && (
            <SettingRow label={t("transcoding.vaapiDevice")}>
              {(id) => <input id={id} className={num} value={e.vaapiDevice ?? "/dev/dri/renderD128"} onChange={(x) => setE("vaapiDevice", x.target.value)} />}
            </SettingRow>
          )}
          <SettingRow label={t("transcoding.preset")} hint={t("transcoding.presetHint")}>
            {(id) => (
              <Select id={id} className="h-10 w-full" value={e.preset} onChange={(x) => setE("preset", x.target.value as Encoding["preset"])}>
                {(["ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow"] as const).map((v) => <option key={v} value={v}>{t(`transcoding.preset_${v}`)}</option>)}
              </Select>
            )}
          </SettingRow>
          <SettingRow label={t("transcoding.quality", { value: e.quality })} hint={t("transcoding.qualityHint")}>
            {(id) => <input id={id} type="range" min={14} max={35} value={e.quality} onChange={(x) => setE("quality", Number(x.target.value))} className="w-full accent-[var(--color-text)]" />}
          </SettingRow>
          <SettingRow label={t("transcoding.audioBitrate")}>
            {(id) => (
              <Select id={id} className="h-10 w-full" value={e.audioBitrateKbps} onChange={(x) => setE("audioBitrateKbps", Number(x.target.value))}>
                {[96, 128, 160, 192, 256, 320].map((v) => <option key={v} value={v}>{v} kbit/s</option>)}
              </Select>
            )}
          </SettingRow>
          <SettingRow label={t("transcoding.segment")} hint={t("transcoding.segmentHint")}>
            {(id) => (
              <Select id={id} className="h-10 w-full" value={e.segmentSeconds} onChange={(x) => setE("segmentSeconds", Number(x.target.value))}>
                {[2, 3, 4, 6, 8, 10].map((v) => <option key={v} value={v}>{t("playbackPrefs.seconds", { count: v })}</option>)}
              </Select>
            )}
          </SettingRow>
          <SettingRow label={t("transcoding.maxSessions")} hint={t("transcoding.maxSessionsHint")}>
            {(id) => (
              <Select id={id} className="h-10 w-full" value={e.maxSessions} onChange={(x) => setE("maxSessions", Number(x.target.value))}>
                {[0, 1, 2, 3, 4, 6, 8, 12, 16].map((v) => <option key={v} value={v}>{v === 0 ? t("playbackPrefs.noLimit") : v}</option>)}
              </Select>
            )}
          </SettingRow>
          <SettingRow label={t("transcoding.toneMapping")} hint={t("transcoding.toneMappingHint")}>
            {(id) => <Toggle id={id} checked={e.toneMapping} onChange={(v) => setE("toneMapping", v)} />}
          </SettingRow>
          <Saved save={enc.save} error={enc.q.error} />
        </Card>
      </form>

      <form onSubmit={(ev) => { ev.preventDefault(); res.save.reset(); res.save.mutate(r); }}>
        <Card title={t("resume.title")}>
          <p className="mb-4 text-sm text-muted">{t("resume.intro")}</p>
          <SettingRow label={t("resume.min")} hint={t("resume.minHint")}>
            {(id) => <input id={id} type="number" min={0} max={50} className={num} value={r.minResumePercent} onChange={(x) => setR("minResumePercent", Number(x.target.value))} />}
          </SettingRow>
          <SettingRow label={t("resume.max")} hint={t("resume.maxHint")}>
            {(id) => <input id={id} type="number" min={50} max={100} className={num} value={r.maxResumePercent} onChange={(x) => setR("maxResumePercent", Number(x.target.value))} />}
          </SettingRow>
          <SettingRow label={t("resume.duration")} hint={t("resume.durationHint")}>
            {(id) => <input id={id} type="number" min={0} max={3600} step={30} className={num} value={r.minResumeDurationSeconds} onChange={(x) => setR("minResumeDurationSeconds", Number(x.target.value))} />}
          </SettingRow>
          <Saved save={res.save} error={res.q.error} />
        </Card>
      </form>
    </div>
  );
}

const LEVEL_TONE: Record<string, string> = { debug: "text-text/45", info: "text-text/80", warn: "text-[#ffc46b]", error: "text-danger" };

/** Recent log lines of the server and the worker, filterable, refreshing on its own. */
export function LogsSettings() {
  const { t } = useTranslation();
  const [level, setLevel] = useState<"debug" | "info" | "warn" | "error">("info");
  const [q, setQ] = useState("");
  const [live, setLive] = useState(true);
  const logs = useQuery({
    queryKey: ["admin", "logs", level, q],
    queryFn: async () => unwrap(await api.GET("/admin/logs", { params: { query: { level, q: q || undefined, limit: 500 } } })),
    refetchInterval: live ? 5000 : false,
  });
  const time = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit", day: "2-digit", month: "2-digit" });
  return (
    <Card title={t("logs.title")} actions={
      <button type="button" onClick={() => void logs.refetch()} aria-label={t("logs.refresh")} className="grid size-9 place-items-center rounded-[6px] text-text/70 hover:bg-white/10">
        <RefreshCw className={`size-4 ${logs.isFetching ? "animate-spin" : ""}`} aria-hidden />
      </button>}>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Select value={level} onChange={(e) => setLevel(e.target.value as typeof level)} aria-label={t("logs.level")}>
          {(["debug", "info", "warn", "error"] as const).map((l) => <option key={l} value={l}>{t(`logs.level_${l}`)}</option>)}
        </Select>
        <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("logs.search")} aria-label={t("logs.search")}
          className="h-9 min-w-0 flex-1 rounded-[6px] border border-border bg-black/30 px-3 text-sm text-text placeholder:text-faint" />
        <label className="flex items-center gap-2 text-sm text-text/70"><Toggle checked={live} onChange={setLive} label={t("logs.live")} />{t("logs.live")}</label>
      </div>
      <ErrorText error={logs.error} />
      {logs.isPending && <Spinner />}
      {logs.data?.length === 0 && <p className="py-8 text-center text-sm text-muted">{t("logs.empty")}</p>}
      <div className="max-h-[65vh] overflow-auto rounded-[6px] border border-border bg-black/40">
        <table className="w-full border-collapse font-mono text-[12px] leading-relaxed">
          <tbody>
            {logs.data?.map((l, i) => (
              <tr key={i} className="border-b border-white/[0.04] align-top hover:bg-white/[0.03]" data-testid="log-row">
                <td className="px-3 py-1.5 whitespace-nowrap text-text/40 tabular">{time.format(new Date(l.time))}</td>
                <td className={`px-2 py-1.5 font-semibold uppercase ${LEVEL_TONE[l.level]}`}>{l.level}</td>
                <td className="px-2 py-1.5 text-text/45">{l.source}</td>
                <td className="px-3 py-1.5 break-words text-text/90">{l.message}{l.attrs && <span className="ml-2 text-text/45">{l.attrs}</span>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

/** Who signed in, what changed and when. */
export function ActivitySettings() {
  const { t } = useTranslation();
  const fmt = useDateTime();
  const [page, setPage] = useState(0);
  const size = 50;
  const act = useQuery({
    queryKey: ["admin", "activity", page],
    queryFn: async () => unwrap(await api.GET("/admin/activity", { params: { query: { limit: size, offset: page * size } } })),
  });
  const pages = Math.max(1, Math.ceil((act.data?.total ?? 0) / size));
  return (
    <Card title={t("activityLog.title")}>
      <p className="mb-4 text-sm text-muted">{t("activityLog.intro")}</p>
      <ErrorText error={act.error} />
      {act.isPending && <Spinner />}
      <ol className="flex flex-col">
        {act.data?.entries.map((e, i) => {
          const detail = e.detail ? Object.entries(e.detail).map(([k, v]) => `${k}: ${typeof v === "string" ? v : JSON.stringify(v)}`).join(" · ") : "";
          return (
            <li key={i} className="grid gap-x-6 gap-y-0.5 border-t border-border py-3 first:border-t-0 sm:grid-cols-[11rem_minmax(0,1fr)]" data-testid="activity-row">
              <span className="text-[13px] text-text/50 tabular">{fmt(e.at)}</span>
              <span className="min-w-0">
                <span className="text-[15px]">{t(`activity.${e.action}`, { defaultValue: e.action })}</span>
                {e.user && <span className="ml-2 text-[13px] font-semibold text-text/70">{e.user}</span>}
                {detail && <span className="mt-0.5 block truncate text-[12px] text-text/45">{detail}</span>}
              </span>
            </li>
          );
        })}
      </ol>
      {pages > 1 && (
        <div className="mt-4 flex items-center gap-3">
          <Button variant="secondary" disabled={page === 0} onClick={() => setPage(page - 1)}>{t("activityLog.newer")}</Button>
          <span className="text-sm text-text/55 tabular">{page + 1} / {pages}</span>
          <Button variant="secondary" disabled={page + 1 >= pages} onClick={() => setPage(page + 1)}>{t("activityLog.older")}</Button>
        </div>
      )}
    </Card>
  );
}

/** A message on the sign-in page and extra CSS for every page. */
export function BrandingSettings() {
  const { t } = useTranslation();
  const b = useSettingsDoc<Branding>("/settings/branding", { loginDisclaimer: "", customCss: "" });
  if (b.q.isPending) return <Spinner />;
  const area = "w-full rounded-[6px] border border-border bg-black/30 p-3 text-sm text-text placeholder:text-faint hover:border-border-strong focus:border-accent/70";
  return (
    <form onSubmit={(e) => { e.preventDefault(); b.save.reset(); b.save.mutate(b.draft); }}>
      <Card title={t("branding.title")}>
        <div className="flex flex-col gap-5">
          <label className="flex flex-col gap-1.5">
            <span className="text-[15px] font-medium">{t("branding.disclaimer")}</span>
            <span className="text-[13px] text-text/55">{t("branding.disclaimerHint")}</span>
            <textarea rows={3} maxLength={2000} className={area} value={b.draft.loginDisclaimer}
              onChange={(e) => b.setDraft({ ...b.draft, loginDisclaimer: e.target.value })} />
          </label>
          <label className="flex flex-col gap-1.5">
            <span className="text-[15px] font-medium">{t("branding.css")}</span>
            <span className="text-[13px] text-text/55">{t("branding.cssHint")}</span>
            <textarea rows={10} maxLength={20000} spellCheck={false} className={`${area} font-mono text-[12px]`} placeholder={":root { --amb-a: #d6763f; }"}
              value={b.draft.customCss} onChange={(e) => b.setDraft({ ...b.draft, customCss: e.target.value })} />
          </label>
        </div>
        <Saved save={b.save} error={b.q.error} />
      </Card>
    </form>
  );
}
