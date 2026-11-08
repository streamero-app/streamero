import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Bell, Plus, Send, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, Card, ErrorText, Select, SuccessText, TextField } from "@/components/ui";

type Target = components["schemas"]["NotificationTarget"];
type EventKind = components["schemas"]["NotificationEvent"];

const EVENTS: EventKind[] = ["item.added", "playback.started", "login.succeeded", "login.failed", "scan.failed"];
const KINDS: Target["kind"][] = ["discord", "ntfy", "gotify", "generic"];

function newTarget(): Target {
  const id = typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `${Date.now()}`;
  return { id, name: "", kind: "discord", url: "", events: ["item.added"], enabled: true };
}

function TargetEditor({ target, onChange, onRemove }: { target: Target; onChange: (t: Target) => void; onRemove: () => void }) {
  const { t } = useTranslation();
  const test = useMutation({
    mutationFn: async () => unwrap(await api.POST("/settings/notifications/test", { body: target })),
  });
  const set = (patch: Partial<Target>) => onChange({ ...target, ...patch });
  return (
    <li className="flex flex-col gap-4 rounded-md border border-border bg-white/[0.02] p-4" data-testid="notification-target">
      <div className="grid gap-4 sm:grid-cols-[1fr_auto]">
        <TextField label={t("notifications.name")} value={target.name} maxLength={80} required
          onChange={(e) => set({ name: e.target.value })} placeholder="Discord #filmovi" />
        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium text-text/90">{t("notifications.kind")}</span>
          <Select value={target.kind} onChange={(e) => set({ kind: e.target.value as Target["kind"] })} className="h-10">
            {KINDS.map((k) => <option key={k} value={k}>{t(`notifications.kind_${k}`)}</option>)}
          </Select>
        </label>
      </div>
      <TextField label={t("notifications.url")} type="url" value={target.url} required onChange={(e) => set({ url: e.target.value })}
        placeholder={t(`notifications.urlHint_${target.kind}`)} hint={t(`notifications.help_${target.kind}`)} />
      {target.kind !== "discord" && (
        <TextField label={target.kind === "gotify" ? t("notifications.appToken") : t("notifications.token")} type="password"
          autoComplete="off" value={target.token ?? ""} onChange={(e) => set({ token: e.target.value })}
          placeholder={target.hasToken ? t("notifications.tokenStored") : ""} />
      )}
      <fieldset className="flex flex-col gap-2 text-sm">
        <legend className="mb-1 font-medium text-text/90">{t("notifications.events")}</legend>
        <div className="flex flex-wrap gap-x-5 gap-y-2">
          {EVENTS.map((ev) => (
            <label key={ev} className="flex items-center gap-2">
              <input type="checkbox" className="accent-[var(--color-accent)]" checked={target.events.includes(ev)}
                onChange={(e) => set({ events: e.target.checked ? [...target.events, ev] : target.events.filter((x) => x !== ev) })} />
              {t(`notifications.event_${ev}`)}
            </label>
          ))}
        </div>
      </fieldset>
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" className="accent-[var(--color-accent)]" checked={target.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
          {t("notifications.enabled")}
        </label>
        <div className="ml-auto flex gap-2">
          <Button type="button" variant="secondary" disabled={!target.url || test.isPending} onClick={() => test.mutate()}>
            <Send className="size-4" aria-hidden />{t("notifications.test")}
          </Button>
          <Button type="button" variant="danger" onClick={onRemove}><Trash2 className="size-4" aria-hidden />{t("libraries.remove")}</Button>
        </div>
      </div>
      {test.isSuccess && <SuccessText>{t("notifications.testSent")}</SuccessText>}
      <ErrorText error={test.error} />
    </li>
  );
}

/** Webhook, Discord, ntfy and Gotify targets for server events. */
export function NotificationsSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const current = useQuery({
    queryKey: ["notifications"],
    queryFn: async () => unwrap(await api.GET("/settings/notifications")),
  });
  const [targets, setTargets] = useState<Target[]>([]);
  useEffect(() => { if (current.data) setTargets(current.data.targets); }, [current.data]);
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/settings/notifications", { body: { targets } })),
    onSuccess: (data) => {
      qc.setQueryData(["notifications"], data);
      setTargets(data.targets);
    },
  });
  return (
    <Card title={<span className="flex items-center gap-2"><Bell className="size-5 text-accent" aria-hidden />{t("notifications.title")}</span>}>
      <p className="mb-5 text-sm text-muted">{t("notifications.intro")}</p>
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); save.reset(); save.mutate(); }}>
        {targets.length === 0 && current.data && <p className="text-sm text-muted">{t("notifications.none")}</p>}
        <ul className="flex flex-col gap-4">
          {targets.map((tg, i) => (
            <TargetEditor key={tg.id} target={tg}
              onChange={(n) => setTargets(targets.map((x, j) => (j === i ? n : x)))}
              onRemove={() => setTargets(targets.filter((_, j) => j !== i))} />
          ))}
        </ul>
        <div className="flex flex-wrap gap-3">
          <Button type="button" variant="secondary" onClick={() => setTargets([...targets, newTarget()])} disabled={targets.length >= 20}>
            <Plus className="size-4" aria-hidden />{t("notifications.add")}
          </Button>
          <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
        </div>
        {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
        <ErrorText error={save.error} />
      </form>
    </Card>
  );
}
