import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Activity, Cpu, HardDrive, MessageSquare, MonitorPlay, Play, RefreshCw, Square } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Badge, Button, Card, ErrorText, Spinner } from "@/components/ui";
import { formatSize } from "@/features/library/format";
import { useDateTime } from "./SettingsLayout";

type Task = components["schemas"]["Task"];

function clock(ms: number): string {
  const s = Math.floor(ms / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  return h ? `${h}:${String(m).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}` : `${m}:${String(s % 60).padStart(2, "0")}`;
}

/** Admin overview: who is watching, free space, recent activity. Refreshes itself. */
export function DashboardSettings() {
  const { t } = useTranslation();
  const when = useDateTime();
  const dash = useQuery({
    queryKey: ["dashboard"],
    refetchInterval: 5000,
    queryFn: async () => unwrap(await api.GET("/dashboard")),
  });
  const d = dash.data;
  if (dash.error) return <ErrorText error={dash.error} />;
  if (!d) return <Spinner />;
  return (
    <div className="flex flex-col gap-6" data-testid="dashboard">
      <Card title={<span className="flex items-center gap-2"><MonitorPlay className="size-5 text-accent" aria-hidden />{t("dashboard.nowPlaying")}</span>}
        actions={d.activeTranscodes > 0 ? <Badge tone="accent"><Cpu className="size-3" aria-hidden />{t("dashboard.transcodes", { count: d.activeTranscodes })}</Badge> : undefined}>
        {d.nowPlaying.length === 0 && <p className="text-sm text-muted">{t("dashboard.nobody")}</p>}
        <ul className="flex flex-col gap-3">
          {d.nowPlaying.map((s) => {
            const pct = s.durationMs ? Math.min(100, (s.positionMs / s.durationMs) * 100) : 0;
            return (
              <li key={s.user + s.device + s.itemId} className="flex flex-col gap-2 rounded-md bg-white/[0.04] p-3" data-testid="now-playing">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <Play className="size-4 fill-accent text-accent" aria-hidden />
                  <Link to="/item/$itemId" params={{ itemId: s.itemId }} className="font-semibold hover:underline">{s.title}</Link>
                  <Badge tone={s.method === "transcode" ? "danger" : s.method === "remux" ? "accent" : "success"}>{t(`player.method_${s.method}`)}</Badge>
                  <span className="ml-auto text-xs text-muted tabular">{clock(s.positionMs)}{s.durationMs ? ` / ${clock(s.durationMs)}` : ""}</span>
                </div>
                <p className="text-xs text-muted">
                  {s.user}{s.profile ? ` · ${s.profile}` : ""} · {s.device}{s.client ? ` (${s.client})` : ""}
                  {s.reason ? ` · ${t("dashboard.reason")}: ${s.reason}` : ""}
                </p>
                {pct > 0 && <div className="h-1 overflow-hidden rounded-full bg-white/10"><div className="h-full bg-accent" style={{ width: `${pct}%` }} /></div>}
                <SessionActions sessionKey={s.key} />
              </li>
            );
          })}
        </ul>
      </Card>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card title={<span className="flex items-center gap-2"><HardDrive className="size-5 text-accent" aria-hidden />{t("dashboard.storage")}</span>}>
          <ul className="flex flex-col gap-4">
            {d.storage.map((s) => {
              const used = s.totalBytes - s.freeBytes;
              const pct = s.totalBytes ? (used / s.totalBytes) * 100 : 0;
              return (
                <li key={s.kind + s.path} className="flex flex-col gap-1.5 text-sm">
                  <div className="flex items-baseline gap-2">
                    <span className="font-medium">{t(`dashboard.storage_${s.kind}`)}</span>
                    <code className="truncate text-xs text-faint">{s.path}</code>
                    <span className="ml-auto shrink-0 text-xs text-muted tabular">{t("dashboard.free", { free: formatSize(s.freeBytes), total: formatSize(s.totalBytes) })}</span>
                  </div>
                  <div className="h-1.5 overflow-hidden rounded-full bg-white/10">
                    <div className={`h-full ${pct > 90 ? "bg-danger" : "bg-accent"}`} style={{ width: `${pct}%` }} />
                  </div>
                </li>
              );
            })}
          </ul>
        </Card>
        <Card title={t("dashboard.server")}>
          <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
            <dt className="text-muted">{t("setup.serverName")}</dt><dd>{d.server.name}</dd>
            <dt className="text-muted">{t("dashboard.version")}</dt><dd className="tabular">{d.server.version}{d.server.commit ? ` (${d.server.commit})` : ""}</dd>
            <dt className="text-muted">{t("dashboard.platform")}</dt><dd>{d.server.os}</dd>
            <dt className="text-muted">{t("dashboard.started")}</dt><dd>{when(d.server.startedAt)}</dd>
          </dl>
        </Card>
      </div>

      <Card title={<span className="flex items-center gap-2"><Activity className="size-5 text-accent" aria-hidden />{t("dashboard.activity")}</span>}>
        <ul className="flex flex-col divide-y divide-white/5 text-sm" data-testid="activity">
          {d.activity.map((a, i) => (
            <li key={i} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 py-2">
              <span className="w-36 shrink-0 text-xs text-muted tabular">{when(a.at)}</span>
              <span className="font-medium">{t(`activity.${a.action}`, { defaultValue: a.action })}</span>
              {a.user && <span className="text-muted">{a.user}</span>}
              {a.detail && "ip" in a.detail && <span className="text-xs text-faint">{String(a.detail.ip)}</span>}
            </li>
          ))}
        </ul>
      </Card>
    </div>
  );
}

/** Maintenance tasks with their last run and a Run now button. */
export function TasksSettings() {
  const { t } = useTranslation();
  const when = useDateTime();
  const qc = useQueryClient();
  const tasks = useQuery({
    queryKey: ["tasks"],
    refetchInterval: 4000,
    queryFn: async () => unwrap(await api.GET("/tasks")),
  });
  const run = useMutation({
    mutationFn: async (key: Task["key"]) => unwrap(await api.POST("/tasks/{taskKey}/run", { params: { path: { taskKey: key } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["tasks"] }),
  });
  return (
    <Card title={t("tasks.title")}>
      <p className="mb-4 text-sm text-muted">{t("tasks.intro")}</p>
      <ErrorText error={tasks.error ?? run.error} />
      <ul className="flex flex-col divide-y divide-white/5">
        {tasks.data?.map((task) => (
          <li key={task.key} className="flex flex-wrap items-center gap-3 py-3" data-testid="task-row">
            <div className="min-w-0 flex-1">
              <p className="font-medium">{t(`tasks.${task.key}`)}</p>
              <p className="text-xs text-muted">
                {t(`tasks.${task.key}_hint`)}
                {task.interval ? ` · ${t("tasks.every", { interval: task.interval })}` : ""}
                {task.lastRunAt ? ` · ${t("tasks.lastRun", { when: when(task.lastRunAt) })}` : ` · ${t("tasks.never")}`}
              </p>
            </div>
            {task.running ? (
              <Badge tone="accent"><RefreshCw className="size-3 animate-spin" aria-hidden />{t("tasks.running")}</Badge>
            ) : task.lastState === "failed" ? <Badge tone="danger">{t("tasks.failed")}</Badge> : null}
            <Button variant="secondary" disabled={task.running || run.isPending} onClick={() => run.mutate(task.key)}>
              {t("tasks.runNow")}
            </Button>
          </li>
        ))}
      </ul>
    </Card>
  );
}

/** Send a message to a playing client, or stop its playback. */
function SessionActions({ sessionKey }: { sessionKey: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [writing, setWriting] = useState(false);
  const [text, setText] = useState("");
  const send = useMutation({
    mutationFn: async (body: { kind: "message" | "stop"; text?: string }) =>
      unwrap(await api.POST("/sessions/{sessionKey}/command", { params: { path: { sessionKey } }, body })),
    onSuccess: (_d, v) => {
      setWriting(false);
      setText("");
      if (v.kind === "stop") void qc.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });
  return (
    <div className="flex flex-wrap items-center gap-2">
      {writing ? (
        <form className="flex min-w-0 flex-1 gap-2" onSubmit={(e) => { e.preventDefault(); if (text.trim()) send.mutate({ kind: "message", text: text.trim() }); }}>
          <input autoFocus maxLength={500} value={text} onChange={(e) => setText(e.target.value)} placeholder={t("dashboard.messagePlaceholder")}
            aria-label={t("dashboard.message")} className="h-9 min-w-0 flex-1 rounded-[6px] border border-border bg-black/30 px-3 text-sm text-text" />
          <Button type="submit" disabled={send.isPending || !text.trim()}>{t("dashboard.send")}</Button>
          <Button type="button" variant="ghost" onClick={() => setWriting(false)}>{t("common.cancel")}</Button>
        </form>
      ) : (
        <>
          <Button variant="secondary" onClick={() => setWriting(true)}><MessageSquare className="size-4" aria-hidden />{t("dashboard.message")}</Button>
          <Button variant="danger" disabled={send.isPending}
            onClick={() => window.confirm(t("dashboard.stopConfirm")) && send.mutate({ kind: "stop", text: t("dashboard.stoppedByAdmin") })}>
            <Square className="size-3.5 fill-current" aria-hidden />{t("dashboard.stop")}
          </Button>
        </>
      )}
      {send.isSuccess && <span className="text-xs text-success">{t("dashboard.sent")}</span>}
      <ErrorText error={send.error} />
    </div>
  );
}

