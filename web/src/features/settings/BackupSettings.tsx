import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { AlertTriangle, ArchiveRestore, DatabaseBackup, Download, RefreshCw, Trash2, Upload } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Badge, Button, Card, ErrorText, SuccessText } from "@/components/ui";
import { formatSize } from "@/features/library/format";
import { SettingRow, Toggle } from "./SettingRow";
import { useDateTime } from "./SettingsLayout";

type Backup = components["schemas"]["Backup"];
type Overview = components["schemas"]["BackupOverview"];
type BackupSettingsBody = components["schemas"]["BackupSettings"];

const KEY = ["backups"];

function useBackups() {
  return useQuery({
    queryKey: KEY,
    queryFn: async () => unwrap(await api.GET("/backups")),
    // Poll while a backup is being written or a restore runs.
    refetchInterval: (q) => {
      const d = q.state.data as Overview | undefined;
      return d && (d.running || d.lastRestore?.state === "running") ? 3000 : false;
    },
  });
}

/** Asks for the backup's name before a restore: everything in the database is replaced. */
function RestoreConfirm({ backup, onDone }: { backup: Backup; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [typed, setTyped] = useState("");
  const restore = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/backups/{backupName}/restore", { params: { path: { backupName: backup.name } }, body: { confirm: typed } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: KEY });
      onDone();
    },
  });
  return (
    <form className="mt-3 flex flex-col gap-3 rounded-md border border-danger/40 bg-danger/[0.06] p-4" data-testid="restore-confirm"
      onSubmit={(e) => { e.preventDefault(); restore.mutate(); }}>
      <p className="flex items-start gap-2 text-sm text-text/90">
        <AlertTriangle className="mt-0.5 size-4 shrink-0 text-danger" aria-hidden />
        {t("backups.restoreWarning")}
      </p>
      <label className="flex flex-col gap-1.5 text-sm">
        <span className="font-medium text-text/90">{t("backups.typeName", { name: backup.name })}</span>
        <input value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" spellCheck={false}
          className="h-10 rounded-[6px] border border-border bg-black/30 px-3 font-mono text-[13px] text-text" />
      </label>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" variant="danger" disabled={typed !== backup.name || restore.isPending}>
          <ArchiveRestore className="size-4" aria-hidden />{t("backups.restoreConfirm")}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
      </div>
      <ErrorText error={restore.error} />
    </form>
  );
}

function BackupRow({ backup, busy }: { backup: Backup; busy: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const when = useDateTime();
  const [confirming, setConfirming] = useState(false);
  const del = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/backups/{backupName}", { params: { path: { backupName: backup.name } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  });
  const href = `/api/v1/backups/${encodeURIComponent(backup.name)}/download`;
  return (
    <li className="py-3" data-testid="backup-row">
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-0 flex-1">
          <p className="flex flex-wrap items-center gap-2 font-medium">
            {when(backup.createdAt)}
            <Badge>{t(`backups.kind_${backup.kind}`)}</Badge>
            {backup.includesImages && <Badge>{t("backups.withArtwork")}</Badge>}
            {backup.problem && <Badge tone="danger">{t(`backups.problem_${backup.problem}`, { defaultValue: backup.problem })}</Badge>}
          </p>
          <p className="truncate text-xs text-muted" title={backup.name}>
            {formatSize(backup.sizeBytes)}
            {backup.serverVersion ? ` · ${t("server.version", { version: backup.serverVersion })}` : ""}
            {backup.scanRunning ? ` · ${t("backups.duringScan")}` : ""}
            {backup.skippedTables?.length ? ` · ${t("backups.skipped", { tables: backup.skippedTables.join(", ") })}` : ""}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <a href={href} download={backup.name} aria-label={t("backups.download")} title={t("backups.download")}
            className="inline-flex size-10 items-center justify-center rounded-[6px] bg-white/[0.08] text-text ring-1 ring-inset ring-border transition-colors hover:bg-white/[0.13]">
            <Download className="size-4" aria-hidden />
          </a>
          <Button variant="secondary" disabled={!backup.restorable || busy || confirming} onClick={() => setConfirming(true)}>
            <ArchiveRestore className="size-4" aria-hidden />{t("backups.restore")}
          </Button>
          <Button variant="danger" aria-label={t("backups.delete")} title={t("backups.delete")} disabled={del.isPending}
            onClick={() => window.confirm(t("backups.deleteConfirm")) && del.mutate()}>
            <Trash2 className="size-4" aria-hidden />
          </Button>
        </div>
      </div>
      {confirming && <RestoreConfirm backup={backup} onDone={() => setConfirming(false)} />}
      <ErrorText error={del.error} />
    </li>
  );
}

function ScheduleCard({ initial }: { initial: BackupSettingsBody }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [st, setSt] = useState(initial);
  useEffect(() => setSt(initial), [initial]);
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/backups/settings", { body: st })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  });
  return (
    <Card title={t("backups.scheduleTitle")}>
      <form onSubmit={(e) => { e.preventDefault(); save.reset(); save.mutate(); }}>
        <SettingRow label={t("backups.enabled")} hint={t("backups.enabledHint")}>
          {(id) => <Toggle id={id} checked={st.enabled} onChange={(v) => setSt({ ...st, enabled: v })} />}
        </SettingRow>
        <SettingRow label={t("backups.keepLast")} hint={t("backups.keepLastHint")}>
          {(id) => (
            <input id={id} type="number" min={1} max={100} value={st.keepLast}
              onChange={(e) => setSt({ ...st, keepLast: Number(e.target.value) })}
              className="h-10 w-24 rounded-[6px] border border-border bg-black/30 px-3 text-[15px] text-text" />
          )}
        </SettingRow>
        <SettingRow label={t("backups.includeImages")} hint={t("backups.includeImagesHint")}>
          {(id) => <Toggle id={id} checked={st.includeImages} onChange={(v) => setSt({ ...st, includeImages: v })} />}
        </SettingRow>
        <div className="mt-4 flex items-center gap-3">
          <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
          {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
        </div>
        <ErrorText error={save.error} />
      </form>
    </Card>
  );
}

/** Backups of the database and settings: create, download, upload, restore, delete. */
export function BackupSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const when = useDateTime();
  const backups = useBackups();
  const fileRef = useRef<HTMLInputElement>(null);
  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/backups")),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  });
  const upload = useMutation({
    mutationFn: async (file: File) =>
      unwrap(await api.POST("/backups/upload", {
        body: file as unknown as string,
        bodySerializer: (b: unknown) => b as BodyInit,
        headers: { "Content-Type": "application/octet-stream" },
      })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  });
  const data = backups.data;
  const restore = data?.lastRestore;
  const restoring = restore?.state === "running";
  const busy = !!data?.running || restoring;
  return (
    <>
      <Card title={<span className="flex items-center gap-2"><DatabaseBackup className="size-5 text-accent" aria-hidden />{t("backups.title")}</span>}
        actions={
          <Button disabled={!data || busy || create.isPending} onClick={() => { create.reset(); create.mutate(); }}>
            {data?.running ? <RefreshCw className="size-4 animate-spin" aria-hidden /> : <DatabaseBackup className="size-4" aria-hidden />}
            {data?.running ? t("backups.running") : t("backups.now")}
          </Button>
        }>
        <p className="mb-4 text-sm text-muted">{t("backups.intro")}</p>
        {data && <p className="mb-4 text-xs text-faint">{t("backups.folder", { dir: data.dir })}</p>}
        {(data?.scanRunning || create.data?.scanRunning) && (
          <p className="mb-4 flex items-start gap-2 rounded-md border border-border bg-white/[0.03] p-3 text-sm text-text/85" role="status" data-testid="scan-warning">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-accent" aria-hidden />
            {t("backups.scanWarning")}
          </p>
        )}
        {restore && (
          <div className={`mb-4 rounded-md border p-3 text-sm ${restore.state === "failed" ? "border-danger/40 text-danger" : "border-border text-text/85"}`}
            role="status" data-testid="restore-status">
            <p>{t(`backups.restore_${restore.state}`, { name: restore.backup, when: when(restore.finishedAt ?? restore.startedAt) })}</p>
            {restore.error && <p className="mt-1 text-xs">{restore.error}</p>}
            {restore.safetyBackup && <p className="mt-1 text-xs text-muted">{t("backups.safetyBackup", { name: restore.safetyBackup })}</p>}
            {restore.warnings?.map((w) => <p key={w} className="mt-1 text-xs text-muted">{w}</p>)}
          </div>
        )}
        <ErrorText error={backups.error ?? create.error} />
        {data && data.backups.length === 0 && <p className="text-sm text-muted">{t("backups.none")}</p>}
        <ul className="flex flex-col divide-y divide-white/5">
          {data?.backups.map((b) => <BackupRow key={b.name} backup={b} busy={busy} />)}
        </ul>
        <div className="mt-5 flex flex-wrap items-center gap-3 border-t border-border pt-5">
          <input ref={fileRef} type="file" accept=".gz,application/gzip" className="sr-only" data-testid="backup-upload"
            aria-label={t("backups.upload")}
            onChange={(e) => { const f = e.target.files?.[0]; if (f) { upload.reset(); upload.mutate(f); } e.target.value = ""; }} />
          <Button variant="secondary" disabled={upload.isPending || restoring} onClick={() => fileRef.current?.click()}>
            {upload.isPending ? <RefreshCw className="size-4 animate-spin" aria-hidden /> : <Upload className="size-4" aria-hidden />}
            {t("backups.upload")}
          </Button>
          <span className="text-xs text-muted">{t("backups.uploadHint")}</span>
          {upload.isSuccess && <SuccessText>{t("backups.uploaded")}</SuccessText>}
          <ErrorText error={upload.error} />
        </div>
      </Card>
      {data && <ScheduleCard initial={data.settings} />}
    </>
  );
}
