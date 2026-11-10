import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Plus, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Badge, Button, Card, ErrorText, IconButton, Select, Spinner } from "@/components/ui";
import { SettingRow, Toggle } from "./SettingRow";
import { useDateTime } from "./SettingsLayout";

type SourceInfo = components["schemas"]["ImportSourceInfo"];
type ImportRun = components["schemas"]["ImportRun"];
type ImportReport = components["schemas"]["ImportReport"];

const field = "h-10 w-full rounded-[6px] border border-border bg-black/30 px-3 text-[15px] text-text placeholder:text-faint hover:border-border-strong focus:border-accent/70";
const active = (r?: ImportRun) => r?.status === "queued" || r?.status === "running";

/** Import watch history, favourites and playlists from another media server (admin). */
export function ImportSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [url, setUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [source, setSource] = useState<SourceInfo | null>(null);
  const [mapping, setMapping] = useState<Record<string, string>>({});
  const [paths, setPaths] = useState<{ from: string; to: string }[]>([]);
  const [playlists, setPlaylists] = useState(true);

  const users = useQuery({ queryKey: ["users"], queryFn: async () => unwrap(await api.GET("/users")) });
  const runs = useQuery({
    queryKey: ["import", "runs"],
    queryFn: async () => unwrap(await api.GET("/import/runs")),
    refetchInterval: (q) => (q.state.data?.some(active) ? 2000 : false),
  });

  const connect = useMutation({
    mutationFn: async () => unwrap(await api.POST("/import/source", { body: { url, apiKey } })),
    onSuccess: (d) => {
      setSource(d);
      setMapping(Object.fromEntries(d.users.map((u) => [u.id, u.suggestedUserId ?? ""])));
    },
  });
  const start = useMutation({
    mutationFn: async (dryRun: boolean) => unwrap(await api.POST("/import/runs", {
      body: {
        url, apiKey, dryRun, playlists,
        users: (source?.users ?? []).map((u) => ({ sourceUserId: u.id, sourceUserName: u.name, ...(mapping[u.id] ? { userId: mapping[u.id] } : {}) })),
        pathMappings: paths.filter((p) => p.from.trim() && p.to.trim()),
      },
    })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["import", "runs"] }),
  });

  // A changed address or key needs a new connection check.
  useEffect(() => { setSource(null); connect.reset(); }, [url, apiKey]);

  const latest = runs.data?.[0];
  const busy = active(latest) || start.isPending;
  const mapped = Object.values(mapping).filter(Boolean).length;

  return (
    <div className="flex flex-col gap-6">
      <Card title={t("import.title")}>
        <p className="mb-4 text-sm text-muted">{t("import.intro")}</p>
        <form className="flex flex-col" onSubmit={(e) => { e.preventDefault(); connect.mutate(); }}>
          <SettingRow label={t("import.url")} hint={t("import.urlHint")}>
            {(id) => <input id={id} className={field} placeholder="http://media:8096" value={url} onChange={(e) => setUrl(e.target.value)} />}
          </SettingRow>
          <SettingRow label={t("import.apiKey")} hint={t("import.apiKeyHint")}>
            {(id) => <input id={id} type="password" autoComplete="off" className={field} value={apiKey} onChange={(e) => setApiKey(e.target.value)} />}
          </SettingRow>
          <div className="flex flex-wrap items-center gap-3 pt-2">
            <Button type="submit" variant="secondary" disabled={!url.trim() || !apiKey.trim() || connect.isPending}>
              {connect.isPending && <Spinner className="size-4" />}{t("import.connect")}
            </Button>
            {source && <span className="text-sm text-text/70" data-testid="import-source">{t("import.connected", { name: source.serverName, version: source.version })}</span>}
            <ErrorText error={connect.error} />
          </div>
        </form>
      </Card>

      {source && (
        <>
          <Card title={t("import.users")}>
            <p className="mb-4 text-sm text-muted">{t("import.usersHint")}</p>
            {source.users.map((u) => (
              <SettingRow key={u.id} label={u.name} hint={u.isAdmin ? t("import.sourceAdmin") : undefined}>
                {(id) => (
                  <Select id={id} value={mapping[u.id] ?? ""} onChange={(e) => setMapping({ ...mapping, [u.id]: e.target.value })}>
                    <option value="">{t("import.skip")}</option>
                    {(users.data ?? []).map((s) => <option key={s.id} value={s.id}>{s.username}</option>)}
                  </Select>
                )}
              </SettingRow>
            ))}
          </Card>

          <Card title={t("import.options")}>
            <p className="mb-3 text-sm text-muted">{t("import.pathsHint")}</p>
            <div className="mb-4 flex flex-col gap-2">
              {paths.map((p, i) => (
                <div key={i} className="flex items-center gap-2">
                  <input aria-label={t("import.pathFrom")} className={field} placeholder="/data/movies" value={p.from}
                    onChange={(e) => setPaths(paths.map((x, j) => (j === i ? { ...x, from: e.target.value } : x)))} />
                  <span aria-hidden className="text-faint">→</span>
                  <input aria-label={t("import.pathTo")} className={field} placeholder="/media/movies" value={p.to}
                    onChange={(e) => setPaths(paths.map((x, j) => (j === i ? { ...x, to: e.target.value } : x)))} />
                  <IconButton label={t("import.removePath")} onClick={() => setPaths(paths.filter((_, j) => j !== i))}><Trash2 className="size-4" /></IconButton>
                </div>
              ))}
              <div>
                <Button type="button" variant="ghost" onClick={() => setPaths([...paths, { from: "", to: "" }])}><Plus className="size-4" aria-hidden />{t("import.addPath")}</Button>
              </div>
            </div>
            <SettingRow label={t("import.playlists")} hint={t("import.playlistsHint")}>
              {(id) => <Toggle id={id} checked={playlists} onChange={setPlaylists} />}
            </SettingRow>
          </Card>

          <div className="flex flex-wrap items-center gap-3">
            <Button type="button" variant="secondary" disabled={busy || mapped === 0} onClick={() => start.mutate(true)}>{t("import.dryRun")}</Button>
            <Button type="button" disabled={busy || mapped === 0} onClick={() => start.mutate(false)}>{t("import.run")}</Button>
            <span className="text-sm text-text/60">{t("import.safeAgain")}</span>
            <ErrorText error={start.error} />
          </div>
        </>
      )}

      {runs.isPending ? <Spinner /> : latest && <RunCard run={latest} />}
      <ErrorText error={runs.error} />
    </div>
  );
}

function RunCard({ run }: { run: ImportRun }) {
  const { t } = useTranslation();
  const when = useDateTime();
  const tone = run.status === "failed" ? "danger" : run.status === "done" ? "success" : "accent";
  const pct = run.progress.usersTotal > 0 ? Math.round((run.progress.usersDone / run.progress.usersTotal) * 100) : 0;
  return (
    <Card title={run.dryRun ? t("import.lastDryRun") : t("import.lastRun")}
      actions={<Badge tone={tone}>{t(`import.status.${run.status}`)}</Badge>}>
      <div className="flex flex-col gap-4" data-testid="import-run">
        <p className="text-sm text-text/60">{run.sourceUrl} · {when(run.createdAt)}</p>
        {active(run) && (
          <div>
            <div className="h-1.5 overflow-hidden rounded-full bg-white/10" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
              <div className="h-full bg-accent transition-[width] duration-500" style={{ width: `${pct}%` }} />
            </div>
            <p className="mt-2 text-sm text-text/70">
              {run.progress.currentUser ? t("import.working", { user: run.progress.currentUser }) : t("import.waiting")}
            </p>
          </div>
        )}
        {run.error && <p role="alert" className="text-sm text-danger">{t("import.failed")} {run.error}</p>}
        {run.report && <Report report={run.report} dryRun={run.dryRun} />}
      </div>
    </Card>
  );
}

function Report({ report, dryRun }: { report: ImportReport; dryRun: boolean }) {
  const { t } = useTranslation();
  const th = "py-2 pr-4 text-left text-[12px] font-medium tracking-[0.06em] text-faint uppercase";
  const td = "py-2 pr-4 tabular-nums";
  return (
    <div className="flex flex-col gap-4" data-testid="import-report">
      {dryRun && <p className="text-sm text-text/70">{t("import.dryRunNote")}</p>}
      <p className="text-sm text-text/80">
        {t("import.matchedSummary", { provider: report.matchedByProvider, path: report.matchedByPath, title: report.matchedByTitle, unmatched: report.unmatched })}
      </p>
      {report.users.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr>
                <th className={th}>{t("import.colUser")}</th>
                <th className={th}>{t("import.colMatched")}</th>
                <th className={th}>{t("import.colPlayed")}</th>
                <th className={th}>{t("import.colResume")}</th>
                <th className={th}>{t("import.colFavorites")}</th>
                <th className={th}>{t("import.colPlaylists")}</th>
              </tr>
            </thead>
            <tbody>
              {report.users.map((u) => (
                <tr key={u.userId + u.sourceUserName} className="border-t border-border" data-testid="import-user-row">
                  <td className="py-2 pr-4">{u.sourceUserName} → {u.username}</td>
                  <td className={td}>{u.matched} / {u.items}</td>
                  <td className={td}>{u.played}</td>
                  <td className={td}>{u.resume}</td>
                  <td className={td}>{u.favorites}</td>
                  <td className={td}>{u.playlists}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <ul className="flex flex-col gap-1 text-sm text-text/70">
        <li>{t("import.playlistSummary", { created: report.playlistsCreated, updated: report.playlistsUpdated, missing: report.playlistItemsMissing })}</li>
        {report.keptNewer > 0 && <li>{t("import.keptNewer", { count: report.keptNewer })}</li>}
        {report.skippedUsers.length > 0 && <li>{t("import.skipped", { users: report.skippedUsers.join(", ") })}</li>}
      </ul>
      {report.unmatchedSample.length > 0 && (
        <details className="text-sm">
          <summary className="cursor-pointer text-text/80">{t("import.unmatchedTitle", { count: report.unmatched })}</summary>
          <p className="mt-2 text-text/60">{t("import.unmatchedHint")}</p>
          <ul className="mt-2 flex flex-col gap-1" data-testid="import-unmatched">
            {report.unmatchedSample.map((u, i) => (
              <li key={i} className="text-text/60">
                <span className="text-text/85">{u.name}</span> · {u.user}{u.path ? <span className="block truncate text-xs text-faint">{u.path}</span> : null}
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}
