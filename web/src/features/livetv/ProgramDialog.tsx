import { useEffect, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { CircleDot, Play, Repeat, X } from "lucide-react";
import { Badge, Button, ErrorText } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { canManageLiveTv, useDeleteRecording, useRecord, useSaveRule, type Program, type RecordingStatus } from "./api";
import { dayOf, episodeTag, isAiring, timeOf } from "./time";

/** Tone of a recording status badge. */
export function statusTone(s?: RecordingStatus): "muted" | "accent" | "danger" | "success" {
  switch (s) {
    case "recording":
      return "danger";
    case "conflict":
    case "failed":
      return "danger";
    case "completed":
      return "success";
    case "scheduled":
      return "accent";
    default:
      return "muted";
  }
}

export function StatusBadge({ status }: { status?: RecordingStatus }) {
  const { t } = useTranslation();
  if (!status) return null;
  return (
    <Badge tone={statusTone(status)}>
      {status === "recording" && <span className="size-1.5 animate-pulse rounded-full bg-danger" aria-hidden />}
      {t(`livetv.status.${status}`)}
    </Badge>
  );
}

/** Programme details with Watch, Record and Record series. */
export function ProgramDialog({ program, onClose }: { program: Program; onClose: () => void }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.resolvedLanguage;
  const navigate = useNavigate();
  const { data: me } = useMe();
  const manage = canManageLiveTv(me);
  const record = useRecord();
  const series = useSaveRule();
  const cancel = useDeleteRecording();
  const [newOnly, setNewOnly] = useState(true);
  const closeRef = useRef<HTMLButtonElement>(null);
  const now = Date.now();
  const airing = isAiring(program, now);
  const ended = new Date(program.end).getTime() <= now;
  // The answer of a just-made request wins over the (possibly stale) programme data.
  const recordingId = record.data?.id ?? (cancel.isSuccess ? undefined : program.recordingId);
  const status = record.data?.status ?? (cancel.isSuccess ? undefined : program.recordingStatus);
  const ruleId = series.data?.id ?? program.seriesRuleId;

  useEffect(() => {
    closeRef.current?.focus();
    const esc = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("keydown", esc);
    return () => document.removeEventListener("keydown", esc);
  }, [onClose]);

  const ep = episodeTag(program);
  const flags = [
    program.isLive && t("livetv.flag.live"),
    program.isNew && t("livetv.flag.new"),
    program.isPremiere && t("livetv.flag.premiere"),
    program.isRepeat && t("livetv.flag.repeat"),
  ].filter(Boolean) as string[];

  return (
    <div className="fixed inset-0 z-50 grid place-items-end bg-black/70 backdrop-blur-sm sm:place-items-center sm:p-4" onClick={onClose}>
      <div role="dialog" aria-modal aria-labelledby="program-title" data-testid="program-dialog" onClick={(e) => e.stopPropagation()}
        className="enter relative flex max-h-[90vh] w-full flex-col overflow-hidden rounded-t-[12px] border border-border bg-surface shadow-2xl sm:max-w-xl sm:rounded-card">
        {program.imageUrl && (
          <div className="relative aspect-[21/9] w-full overflow-hidden bg-surface-2">
            <img src={program.imageUrl} alt="" className="size-full object-cover" />
            <div className="absolute inset-0 bg-gradient-to-t from-surface via-surface/30 to-transparent" />
          </div>
        )}
        <button ref={closeRef} type="button" aria-label={t("common.close")} onClick={onClose}
          className="absolute top-3 right-3 grid size-9 place-items-center rounded-[6px] bg-black/50 text-text backdrop-blur hover:bg-black/70">
          <X className="size-5" aria-hidden />
        </button>
        <div className="flex flex-col gap-4 overflow-y-auto p-5 sm:p-6">
          <div className="flex flex-col gap-1.5">
            <p className="text-xs font-semibold tracking-[0.08em] text-text/55 uppercase">
              {[program.channelNumber, program.channelName].filter(Boolean).join(" · ")}
            </p>
            <h2 id="program-title" className="font-head text-2xl leading-tight">{program.title}</h2>
            {(program.subtitle || ep) && (
              <p className="text-[15px] text-text/80">{[ep, program.subtitle].filter(Boolean).join(" · ")}</p>
            )}
            <p className="text-sm text-muted tabular" data-testid="program-time">
              {dayOf(program.start, lang)} · {timeOf(program.start, lang)}–{timeOf(program.end, lang)}
              {program.year ? ` · ${program.year}` : ""}
            </p>
          </div>
          {(flags.length > 0 || status || program.categories.length > 0) && (
            <div className="flex flex-wrap gap-1.5">
              <StatusBadge status={status} />
              {flags.map((f) => <Badge key={f} tone="accent">{f}</Badge>)}
              {program.categories.slice(0, 4).map((c) => <Badge key={c}>{c}</Badge>)}
            </div>
          )}
          {program.description && <p className="max-w-prose text-[15px] leading-relaxed text-text/80">{program.description}</p>}

          <div className="flex flex-wrap items-center gap-3 pt-1">
            {airing && (
              <Button onClick={() => void navigate({ to: "/live/watch/$channelId", params: { channelId: program.channelId } })}>
                <Play className="size-4 fill-current" aria-hidden />{t("livetv.watch")}
              </Button>
            )}
            {manage && !ended && !recordingId && (
              <Button variant="secondary" disabled={record.isPending}
                onClick={() => record.mutate({ programId: program.id, channelId: program.channelId })}>
                <CircleDot className="size-4 text-danger" aria-hidden />{t("livetv.record")}
              </Button>
            )}
            {manage && recordingId && (status === "scheduled" || status === "conflict" || status === "recording") && (
              <Button variant="danger" disabled={cancel.isPending} onClick={() => cancel.mutate(recordingId)}>
                {status === "recording" ? t("livetv.stopRecording") : t("livetv.cancelRecording")}
              </Button>
            )}
          </div>
          {manage && !ended && (
            <div className="flex flex-wrap items-center gap-3 border-t border-border pt-4">
              {ruleId ? (
                <p className="flex items-center gap-2 text-sm text-text/80"><Repeat className="size-4" aria-hidden />{t("livetv.seriesActive")}</p>
              ) : (
                <>
                  <Button variant="secondary" disabled={series.isPending}
                    onClick={() => series.mutate({ body: { programId: program.id, channelId: program.channelId, newOnly } })}>
                    <Repeat className="size-4" aria-hidden />{t("livetv.recordSeries")}
                  </Button>
                  <label className="flex items-center gap-2 text-sm text-text/80">
                    <input type="checkbox" checked={newOnly} onChange={(e) => setNewOnly(e.target.checked)} className="size-4 accent-[var(--amb-a)]" />
                    {t("livetv.newOnly")}
                  </label>
                </>
              )}
            </div>
          )}
          {record.data?.status === "conflict" && <p role="alert" className="text-sm text-danger">{t("livetv.conflictNotice")}</p>}
          <ErrorText error={record.error ?? series.error ?? cancel.error} />
        </div>
      </div>
    </div>
  );
}
