import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { CircleDot, Repeat } from "lucide-react";
import { Button, ErrorText, Spinner } from "@/components/ui";
import { formatSize } from "@/features/library/format";
import { useMe } from "@/features/auth/useAuth";
import { canManageLiveTv, useDeleteRecording, useRecordings, useUpdateRecording, type Recording, type RecordingStatus } from "./api";
import { StatusBadge } from "./ProgramDialog";
import { dayOf, episodeTag, timeOf } from "./time";

const FINISHED: RecordingStatus[] = ["recording", "completed", "failed"];
const UPCOMING: RecordingStatus[] = ["recording", "scheduled", "conflict"];

const numField = "h-9 w-20 rounded-[6px] border border-border bg-black/30 px-2 text-sm text-text tabular focus:border-accent/70";

/** Priority and padding of one scheduled recording. */
function EditRecording({ rec, onDone }: { rec: Recording; onDone: () => void }) {
  const { t } = useTranslation();
  const update = useUpdateRecording();
  const [priority, setPriority] = useState(rec.priority);
  const [pre, setPre] = useState(Math.round(rec.prePaddingSeconds / 60));
  const [post, setPost] = useState(Math.round(rec.postPaddingSeconds / 60));
  return (
    <form className="mt-3 flex flex-wrap items-end gap-3" data-testid="edit-recording"
      onSubmit={(e) => {
        e.preventDefault();
        update.mutate({ id: rec.id, body: { priority, prePaddingSeconds: pre * 60, postPaddingSeconds: post * 60 } }, { onSuccess: onDone });
      }}>
      <label className="flex flex-col gap-1 text-xs text-muted">{t("livetv.priority")}
        <input type="number" min={-100} max={100} value={priority} onChange={(e) => setPriority(Number(e.target.value))} className={numField} />
      </label>
      <label className="flex flex-col gap-1 text-xs text-muted">{t("livetv.prePadding")}
        <input type="number" min={0} max={60} value={pre} onChange={(e) => setPre(Number(e.target.value))} className={numField} />
      </label>
      <label className="flex flex-col gap-1 text-xs text-muted">{t("livetv.postPadding")}
        <input type="number" min={0} max={120} value={post} onChange={(e) => setPost(Number(e.target.value))} className={numField} />
      </label>
      <Button type="submit" disabled={update.isPending}>{t("common.save")}</Button>
      <Button type="button" variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
      <ErrorText error={update.error} />
    </form>
  );
}

function RecordingRow({ rec, upcoming }: { rec: Recording; upcoming: boolean }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.resolvedLanguage;
  const { data: me } = useMe();
  const manage = canManageLiveTv(me);
  const remove = useDeleteRecording();
  const [editing, setEditing] = useState(false);
  const ep = episodeTag(rec);
  const title = rec.itemId ? (
    <Link to="/item/$itemId" params={{ itemId: rec.itemId }} className="hover:underline">{rec.title}</Link>
  ) : rec.title;
  const action = rec.status === "recording" ? t("livetv.stopRecording")
    : upcoming ? t("livetv.cancelRecording") : t("livetv.removeFromList");
  return (
    <li className="flex flex-col px-4 py-4 sm:px-5" data-testid="recording-row">
      <div className="flex flex-wrap items-start gap-x-4 gap-y-2">
        <div className="min-w-0 flex-1">
          <p className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{title}</span>
            {rec.seriesRuleId && <Repeat className="size-3.5 text-text/50" aria-label={t("livetv.fromSeries")} />}
            <StatusBadge status={rec.status} />
          </p>
          {(ep || rec.subtitle) && <p className="text-sm text-text/75">{[ep, rec.subtitle].filter(Boolean).join(" · ")}</p>}
          <p className="text-[13px] text-muted tabular">
            {dayOf(rec.start, lang)} · {timeOf(rec.start, lang)}–{timeOf(rec.end, lang)}
            {rec.channelName ? ` · ${[rec.channelNumber, rec.channelName].filter(Boolean).join(" ")}` : ""}
            {upcoming && (rec.prePaddingSeconds > 0 || rec.postPaddingSeconds > 0)
              ? ` · ${t("livetv.paddingShort", { pre: Math.round(rec.prePaddingSeconds / 60), post: Math.round(rec.postPaddingSeconds / 60) })}` : ""}
            {upcoming && rec.priority !== 0 ? ` · ${t("livetv.priorityShort", { n: rec.priority })}` : ""}
            {rec.sizeBytes ? ` · ${formatSize(rec.sizeBytes)}` : ""}
          </p>
          {rec.error && <p className="mt-1 text-[13px] text-danger">{rec.error}</p>}
          {rec.postProcessError && <p className="mt-1 text-[13px] text-danger">{t("livetv.postProcessFailed", { error: rec.postProcessError })}</p>}
        </div>
        {manage && (
          <div className="flex shrink-0 gap-2">
            {upcoming && rec.status !== "recording" && !editing && (
              <Button variant="secondary" onClick={() => setEditing(true)}>{t("common.edit")}</Button>
            )}
            <Button variant={rec.status === "completed" ? "ghost" : "danger"} disabled={remove.isPending} onClick={() => remove.mutate(rec.id)}>{action}</Button>
          </div>
        )}
      </div>
      {editing && <EditRecording rec={rec} onDone={() => setEditing(false)} />}
      <ErrorText error={remove.error} />
    </li>
  );
}

/** Finished recordings (and ones in progress), or the upcoming schedule. */
function RecordingList({ upcoming }: { upcoming: boolean }) {
  const { t, i18n } = useTranslation();
  const q = useRecordings(upcoming ? UPCOMING : FINISHED);
  const list = q.data ?? [];
  const conflicts = list.filter((r) => r.status === "conflict").length;
  // The schedule reads by day; the finished list newest first as the server sends it.
  const groups: { day: string; items: Recording[] }[] = [];
  for (const r of list) {
    const day = upcoming ? dayOf(r.start, i18n.resolvedLanguage) : "";
    const last = groups.at(-1);
    if (last && last.day === day) last.items.push(r);
    else groups.push({ day, items: [r] });
  }
  return (
    <section className="flex flex-col gap-4" data-testid={upcoming ? "schedule-page" : "recordings-page"}>
      {!upcoming && <p className="max-w-prose text-sm text-muted">{t("livetv.recordingsIntro")}</p>}
      {conflicts > 0 && (
        <p role="alert" className="rounded-card border border-danger/40 bg-danger/10 px-4 py-3 text-sm text-danger">
          {t("livetv.conflicts", { count: conflicts })}
        </p>
      )}
      <ErrorText error={q.error} />
      {q.isPending ? <Spinner /> : list.length === 0 ? (
        <p className="flex items-center gap-2 rounded-card border border-border bg-surface p-6 text-muted">
          <CircleDot className="size-4" aria-hidden />{upcoming ? t("livetv.noSchedule") : t("livetv.noRecordings")}
        </p>
      ) : groups.map((g, i) => (
        <div key={g.day + i} className="flex flex-col gap-2">
          {g.day && <h2 className="text-xs font-semibold tracking-[0.08em] text-text/55 uppercase">{g.day}</h2>}
          <ul className="flex flex-col divide-y divide-border overflow-hidden rounded-card border border-border bg-surface">
            {g.items.map((r) => <RecordingRow key={r.id} rec={r} upcoming={upcoming} />)}
          </ul>
        </div>
      ))}
    </section>
  );
}

export function RecordingsPage() {
  return <RecordingList upcoming={false} />;
}

export function SchedulePage() {
  return <RecordingList upcoming />;
}
