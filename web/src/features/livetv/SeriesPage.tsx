import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Repeat } from "lucide-react";
import { Badge, Button, Card, ErrorText, Select, Spinner, TextField } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { canManageLiveTv, useChannels, useDeleteRule, useSaveRule, useSeriesRules, type SeriesRule, type SeriesRuleInput } from "./api";
import { useConfirm } from "@/components/Dialog";

const optNum = (v: string): number | undefined => (v.trim() === "" ? undefined : Number(v));

/** Create or change a series rule. */
export function SeriesRuleForm({ rule, onDone }: { rule?: SeriesRule; onDone: () => void }) {
  const { t } = useTranslation();
  const channels = useChannels();
  const save = useSaveRule();
  const [title, setTitle] = useState(rule?.title ?? "");
  const [channelId, setChannelId] = useState(rule?.channelId ?? "");
  const [newOnly, setNewOnly] = useState(rule?.newOnly ?? true);
  const [priority, setPriority] = useState(String(rule?.priority ?? 0));
  const [pre, setPre] = useState(rule?.prePaddingSeconds !== undefined ? String(Math.round(rule.prePaddingSeconds / 60)) : "");
  const [post, setPost] = useState(rule?.postPaddingSeconds !== undefined ? String(Math.round(rule.postPaddingSeconds / 60)) : "");
  const [keepLast, setKeepLast] = useState(String(rule?.keepLast ?? 0));
  const submit = () => {
    const preMin = optNum(pre);
    const postMin = optNum(post);
    const body: SeriesRuleInput = {
      title: title.trim(), newOnly, priority: Number(priority) || 0, keepLast: Number(keepLast) || 0,
      channelId: channelId || undefined,
      prePaddingSeconds: preMin === undefined ? undefined : preMin * 60,
      postPaddingSeconds: postMin === undefined ? undefined : postMin * 60,
    };
    save.mutate({ id: rule?.id, body }, { onSuccess: onDone });
  };
  return (
    <form className="flex flex-col gap-4" data-testid="series-form" onSubmit={(e) => { e.preventDefault(); if (title.trim()) submit(); }}>
      <TextField label={t("livetv.seriesTitle")} hint={t("livetv.seriesTitleHint")} value={title} required maxLength={300}
        onChange={(e) => setTitle(e.target.value)} />
      <label className="flex flex-col gap-1.5 text-sm font-medium text-text/90">{t("livetv.seriesChannel")}
        <Select className="h-10 w-full" value={channelId} onChange={(e) => setChannelId(e.target.value)}>
          <option value="">{t("livetv.anyChannel")}</option>
          {(channels.data ?? []).map((c) => <option key={c.id} value={c.id}>{[c.number, c.name].filter(Boolean).join(" · ")}</option>)}
        </Select>
      </label>
      <fieldset className="flex flex-col gap-2 text-sm">
        <legend className="mb-1 font-medium text-text/90">{t("livetv.seriesWhich")}</legend>
        <label className="flex items-center gap-2"><input type="radio" name="newOnly" checked={newOnly} onChange={() => setNewOnly(true)} className="accent-[var(--amb-a)]" />{t("livetv.newOnly")}</label>
        <label className="flex items-center gap-2"><input type="radio" name="newOnly" checked={!newOnly} onChange={() => setNewOnly(false)} className="accent-[var(--amb-a)]" />{t("livetv.allEpisodes")}</label>
      </fieldset>
      <div className="grid gap-4 sm:grid-cols-2">
        <TextField label={t("livetv.priority")} hint={t("livetv.priorityHint")} type="number" min={-100} max={100} value={priority} onChange={(e) => setPriority(e.target.value)} />
        <TextField label={t("livetv.keepLast")} hint={t("livetv.keepLastHint")} type="number" min={0} max={1000} value={keepLast} onChange={(e) => setKeepLast(e.target.value)} />
        <TextField label={t("livetv.prePadding")} hint={t("livetv.paddingDefaultHint")} type="number" min={0} max={60} value={pre} onChange={(e) => setPre(e.target.value)} />
        <TextField label={t("livetv.postPadding")} hint={t("livetv.paddingDefaultHint")} type="number" min={0} max={120} value={post} onChange={(e) => setPost(e.target.value)} />
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={save.isPending || !title.trim()}>{rule ? t("common.save") : t("livetv.createRule")}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
        <ErrorText error={save.error} />
      </div>
    </form>
  );
}

function RuleRow({ rule, manage }: { rule: SeriesRule; manage: boolean }) {
  const confirmDialog = useConfirm();
  const { t } = useTranslation();
  const del = useDeleteRule();
  const [editing, setEditing] = useState(false);
  if (editing) return <li className="px-4 py-4 sm:px-5"><SeriesRuleForm rule={rule} onDone={() => setEditing(false)} /></li>;
  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-4 sm:px-5" data-testid="series-rule">
      <Repeat className="size-4 shrink-0 text-text/50" aria-hidden />
      <div className="min-w-0 flex-1">
        <p className="font-medium">{rule.title}</p>
        <p className="text-[13px] text-muted">
          {[rule.channelName ?? t("livetv.anyChannel"), rule.newOnly ? t("livetv.newOnly") : t("livetv.allEpisodes"),
            rule.priority !== 0 ? t("livetv.priorityShort", { n: rule.priority }) : "",
            rule.keepLast > 0 ? t("livetv.keepLastShort", { count: rule.keepLast }) : ""].filter(Boolean).join(" · ")}
        </p>
      </div>
      <Badge tone={rule.upcoming > 0 ? "accent" : "muted"}>{t("livetv.upcoming", { count: rule.upcoming })}</Badge>
      {manage && (
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => setEditing(true)}>{t("common.edit")}</Button>
          <Button variant="danger" disabled={del.isPending} onClick={() => { void confirmDialog({ message: t("livetv.deleteRuleConfirm", { title: rule.title }), danger: true }).then((ok) => { if (ok) del.mutate(rule.id); }); }}>
            {t("common.delete")}
          </Button>
        </div>
      )}
      <ErrorText error={del.error} />
    </li>
  );
}

/** Series rules: record every (new) airing of a title. */
export function SeriesPage() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const manage = canManageLiveTv(me);
  const rules = useSeriesRules();
  const [adding, setAdding] = useState(false);
  const list = rules.data ?? [];
  return (
    <section className="flex max-w-4xl flex-col gap-4" data-testid="series-page">
      <div className="flex flex-wrap items-center gap-3">
        <p className="max-w-prose flex-1 text-sm text-muted">{t("livetv.seriesIntro")}</p>
        {manage && !adding && <Button onClick={() => setAdding(true)}><Plus className="size-4" aria-hidden />{t("livetv.addRule")}</Button>}
      </div>
      {adding && <Card title={t("livetv.addRule")}><SeriesRuleForm onDone={() => setAdding(false)} /></Card>}
      <ErrorText error={rules.error} />
      {rules.isPending ? <Spinner /> : list.length === 0 ? (
        <p className="rounded-card border border-border bg-surface p-6 text-muted">{t("livetv.noRules")}</p>
      ) : (
        <ul className="flex flex-col divide-y divide-border overflow-hidden rounded-card border border-border bg-surface">
          {list.map((r) => <RuleRow key={r.id} rule={r} manage={manage} />)}
        </ul>
      )}
    </section>
  );
}
