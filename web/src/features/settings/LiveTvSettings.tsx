import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, Search, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Badge, Button, Card, ErrorText, Select, Spinner, SuccessText, TextField } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { SettingRow, Toggle } from "./SettingRow";
import { useDateTime } from "./SettingsLayout";

type Tuner = components["schemas"]["LiveTvTuner"];
type TunerInput = components["schemas"]["LiveTvTunerInput"];
type GuideSource = components["schemas"]["LiveTvGuideSource"];
type GuideInput = components["schemas"]["LiveTvGuideSourceInput"];
type TestResult = components["schemas"]["LiveTvTestResult"];
type ChannelAdmin = components["schemas"]["LiveTvChannelAdmin"];
type ChannelUpdate = components["schemas"]["LiveTvChannelUpdate"];
type LiveSettings = components["schemas"]["LiveTvSettings"];

const K = {
  tuners: ["livetv", "admin", "tuners"],
  guides: ["livetv", "admin", "guides"],
  mapping: ["livetv", "admin", "mapping"],
  settings: ["livetv", "admin", "settings"],
};

export const PLACEHOLDERS = ["{path}", "{dir}", "{filename}", "{title}", "{subtitle}", "{channel}", "{channelNumber}", "{start}", "{end}", "{season}", "{episode}", "{recordingId}"];

const optNum = (v: string): number | undefined => (v.trim() === "" ? undefined : Number(v));

function TestOutcome({ result }: { result?: TestResult }) {
  if (!result) return null;
  return (
    <p role="status" data-testid="test-result" className={`text-sm ${result.ok ? "text-success" : "text-danger"}`}>{result.message}</p>
  );
}

/** Add or change an M3U playlist or an HDHomeRun device, with "Test". */
function TunerForm({ tuner, preset, onDone }: { tuner?: Tuner; preset?: Partial<TunerInput>; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [kind, setKind] = useState<TunerInput["kind"]>(tuner?.kind ?? preset?.kind ?? "m3u");
  const [name, setName] = useState(tuner?.name ?? preset?.name ?? "");
  const [fromFile, setFromFile] = useState(!!tuner?.filePath);
  const [url, setUrl] = useState(tuner?.url ?? preset?.url ?? "");
  const [filePath, setFilePath] = useState(tuner?.filePath ?? "");
  const [count, setCount] = useState(tuner ? String(tuner.tunerCount) : preset?.tunerCount !== undefined ? String(preset.tunerCount) : "");
  const [userAgent, setUserAgent] = useState(tuner?.userAgent ?? "");
  const [enabled, setEnabled] = useState(tuner?.enabled ?? true);
  const body = (): TunerInput => ({
    kind, name: name.trim(), enabled,
    url: kind === "hdhomerun" || !fromFile ? url.trim() || undefined : undefined,
    filePath: kind === "m3u" && fromFile ? filePath.trim() || undefined : undefined,
    tunerCount: optNum(count),
    userAgent: kind === "m3u" ? userAgent.trim() || undefined : undefined,
  });
  const test = useMutation({ mutationFn: async (b: TunerInput) => unwrap(await api.POST("/livetv/tuners/test", { body: b })) });
  const save = useMutation({
    mutationFn: async (b: TunerInput) => tuner
      ? unwrap(await api.PUT("/livetv/tuners/{tunerId}", { params: { path: { tunerId: tuner.id } }, body: b }))
      : unwrap(await api.POST("/livetv/tuners", { body: b })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["livetv"] });
      onDone();
    },
  });
  const valid = name.trim() !== "" && (kind === "m3u" && fromFile ? filePath.trim() !== "" : url.trim() !== "");
  return (
    <form className="flex flex-col gap-4" data-testid="tuner-form" onSubmit={(e) => { e.preventDefault(); if (valid) save.mutate(body()); }}>
      <div className="grid gap-4 sm:grid-cols-2">
        <label className="flex flex-col gap-1.5 text-sm font-medium text-text/90">{t("livetvAdmin.kind")}
          <Select className="h-10 w-full" value={kind} onChange={(e) => { test.reset(); setKind(e.target.value as TunerInput["kind"]); }}>
            <option value="m3u">{t("livetvAdmin.kind_m3u")}</option>
            <option value="hdhomerun">{t("livetvAdmin.kind_hdhomerun")}</option>
          </Select>
        </label>
        <TextField label={t("livetvAdmin.name")} value={name} maxLength={100} required onChange={(e) => setName(e.target.value)} />
      </div>
      {kind === "m3u" && (
        <div className="flex gap-4 text-sm" role="radiogroup" aria-label={t("livetvAdmin.source")}>
          <label className="flex items-center gap-2"><input type="radio" checked={!fromFile} onChange={() => setFromFile(false)} className="accent-[var(--amb-a)]" />{t("livetvAdmin.fromUrl")}</label>
          <label className="flex items-center gap-2"><input type="radio" checked={fromFile} onChange={() => setFromFile(true)} className="accent-[var(--amb-a)]" />{t("livetvAdmin.fromFile")}</label>
        </div>
      )}
      {kind === "m3u" && fromFile ? (
        <TextField label={t("livetvAdmin.playlistFile")} hint={t("livetvAdmin.fileHint")} value={filePath} maxLength={1000}
          placeholder="/config/livetv/channels.m3u" onChange={(e) => setFilePath(e.target.value)} />
      ) : (
        <TextField label={kind === "m3u" ? t("livetvAdmin.playlistUrl") : t("livetvAdmin.deviceAddress")}
          hint={kind === "hdhomerun" ? t("livetvAdmin.deviceAddressHint") : undefined} value={url} maxLength={2000}
          placeholder={kind === "m3u" ? "https://example.com/playlist.m3u" : "http://192.168.1.50"} onChange={(e) => setUrl(e.target.value)} />
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        <TextField label={t("livetvAdmin.tunerCount")} type="number" min={0} max={64} value={count}
          hint={kind === "m3u" ? t("livetvAdmin.tunerCountM3u") : t("livetvAdmin.tunerCountHdhr")} onChange={(e) => setCount(e.target.value)} />
        {kind === "m3u" && (
          <TextField label={t("livetvAdmin.userAgent")} hint={t("livetvAdmin.userAgentHint")} value={userAgent} maxLength={300} onChange={(e) => setUserAgent(e.target.value)} />
        )}
      </div>
      <label className="flex items-center gap-3 text-sm"><Toggle checked={enabled} onChange={setEnabled} label={t("livetvAdmin.enabled")} />{t("livetvAdmin.enabled")}</label>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" variant="secondary" disabled={!valid || test.isPending} onClick={() => test.mutate(body())}>
          {test.isPending ? <Spinner className="size-4" /> : null}{t("livetvAdmin.test")}
        </Button>
        <Button type="submit" disabled={!valid || save.isPending}>{tuner ? t("common.save") : t("livetvAdmin.addTuner")}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
      </div>
      <TestOutcome result={test.data} />
      <ErrorText error={test.error ?? save.error} />
    </form>
  );
}

function TunersCard() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const when = useDateTime();
  const q = useQuery({ queryKey: K.tuners, queryFn: async () => unwrap(await api.GET("/livetv/tuners")) });
  const [editing, setEditing] = useState<Tuner | "new" | null>(null);
  const [preset, setPreset] = useState<Partial<TunerInput> | undefined>();
  const discover = useMutation({ mutationFn: async () => unwrap(await api.GET("/livetv/tuners/discover")) });
  const refresh = useMutation({
    mutationFn: async (id: string) => unwrap(await api.POST("/livetv/tuners/{tunerId}/refresh", { params: { path: { tunerId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["livetv"] }),
  });
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/livetv/tuners/{tunerId}", { params: { path: { tunerId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["livetv"] }),
  });
  const list = q.data ?? [];
  return (
    <Card title={t("livetvAdmin.tuners")} actions={!editing && (
      <div className="flex gap-2">
        <Button variant="secondary" disabled={discover.isPending} onClick={() => discover.mutate()}>
          {discover.isPending ? <Spinner className="size-4" /> : <Search className="size-4" aria-hidden />}{t("livetvAdmin.discover")}
        </Button>
        <Button onClick={() => { setPreset(undefined); setEditing("new"); }}><Plus className="size-4" aria-hidden />{t("livetvAdmin.addTuner")}</Button>
      </div>
    )}>
      <p className="mb-4 max-w-prose text-sm text-muted">{t("livetvAdmin.tunersIntro")}</p>
      {discover.data && (
        <div className="mb-4 rounded-[6px] border border-border bg-black/20 p-4 text-sm" data-testid="discovered">
          {discover.data.length === 0 ? <p className="text-muted">{t("livetvAdmin.discoverNone")}</p> : (
            <ul className="flex flex-col gap-2">
              {discover.data.map((d) => (
                <li key={d.deviceId} className="flex flex-wrap items-center gap-3">
                  <span className="font-medium">{d.name}</span>
                  <span className="text-muted">{[d.model, d.url, t("livetvAdmin.tunersN", { count: d.tunerCount })].filter(Boolean).join(" · ")}</span>
                  {d.added ? <Badge>{t("livetvAdmin.alreadyAdded")}</Badge> : (
                    <Button variant="secondary" className="ml-auto" onClick={() => {
                      setPreset({ kind: "hdhomerun", name: d.name, url: d.url, tunerCount: d.tunerCount });
                      setEditing("new");
                    }}>{t("livetvAdmin.add")}</Button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
      {editing && (
        <div className="mb-5 rounded-[6px] border border-border bg-black/20 p-4">
          <TunerForm key={editing === "new" ? "new" + (preset?.url ?? "") : editing.id} tuner={editing === "new" ? undefined : editing} preset={preset} onDone={() => setEditing(null)} />
        </div>
      )}
      {q.isPending ? <Spinner /> : list.length === 0 ? <p className="text-sm text-muted">{t("livetvAdmin.noTuners")}</p> : (
        <ul className="flex flex-col divide-y divide-border">
          {list.map((tu) => (
            <li key={tu.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 py-3" data-testid="tuner-row">
              <div className="min-w-0 flex-1">
                <p className="flex flex-wrap items-center gap-2 font-medium">
                  {tu.name}<Badge>{t(`livetvAdmin.kind_${tu.kind}`)}</Badge>{!tu.enabled && <Badge>{t("livetvAdmin.disabled")}</Badge>}
                </p>
                <p className="truncate text-[13px] text-muted">
                  {[tu.model, tu.url ?? tu.filePath, t("livetvAdmin.channelsN", { count: tu.channelCount }),
                    tu.tunerCount > 0 ? t("livetvAdmin.tunersN", { count: tu.tunerCount }) : t("livetvAdmin.unlimited"),
                    tu.lastRefreshAt ? t("livetvAdmin.refreshedAt", { when: when(tu.lastRefreshAt) }) : ""].filter(Boolean).join(" · ")}
                </p>
                {tu.error && <p className="text-[13px] text-danger">{tu.error}</p>}
              </div>
              <div className="flex gap-2">
                <Button variant="ghost" aria-label={t("livetvAdmin.refreshChannels")} title={t("livetvAdmin.refreshChannels")}
                  disabled={refresh.isPending} onClick={() => refresh.mutate(tu.id)}><RefreshCw className="size-4" aria-hidden /></Button>
                <Button variant="secondary" onClick={() => setEditing(tu)}>{t("common.edit")}</Button>
                <Button variant="ghost" aria-label={t("common.delete")} title={t("common.delete")}
                  onClick={() => { if (window.confirm(t("livetvAdmin.deleteTunerConfirm", { name: tu.name }))) remove.mutate(tu.id); }}>
                  <Trash2 className="size-4" aria-hidden />
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      <ErrorText error={q.error ?? discover.error ?? refresh.error ?? remove.error} />
    </Card>
  );
}

function GuideForm({ guide, onDone }: { guide?: GuideSource; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [name, setName] = useState(guide?.name ?? "");
  const [fromFile, setFromFile] = useState(!!guide?.filePath);
  const [url, setUrl] = useState(guide?.url ?? "");
  const [filePath, setFilePath] = useState(guide?.filePath ?? "");
  const [timezone, setTimezone] = useState(guide?.timezone ?? "");
  const [hours, setHours] = useState(String(guide?.refreshHours ?? 12));
  const [enabled, setEnabled] = useState(guide?.enabled ?? true);
  const body = (): GuideInput => ({
    name: name.trim(), enabled, refreshHours: Number(hours) || 12, timezone: timezone.trim() || undefined,
    url: fromFile ? undefined : url.trim() || undefined, filePath: fromFile ? filePath.trim() || undefined : undefined,
  });
  const test = useMutation({ mutationFn: async (b: GuideInput) => unwrap(await api.POST("/livetv/guides/test", { body: b })) });
  const save = useMutation({
    mutationFn: async (b: GuideInput) => guide
      ? unwrap(await api.PUT("/livetv/guides/{guideId}", { params: { path: { guideId: guide.id } }, body: b }))
      : unwrap(await api.POST("/livetv/guides", { body: b })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["livetv"] });
      onDone();
    },
  });
  const valid = name.trim() !== "" && (fromFile ? filePath.trim() !== "" : url.trim() !== "");
  return (
    <form className="flex flex-col gap-4" data-testid="guide-form" onSubmit={(e) => { e.preventDefault(); if (valid) save.mutate(body()); }}>
      <TextField label={t("livetvAdmin.name")} value={name} maxLength={100} required onChange={(e) => setName(e.target.value)} />
      <div className="flex gap-4 text-sm" role="radiogroup" aria-label={t("livetvAdmin.source")}>
        <label className="flex items-center gap-2"><input type="radio" checked={!fromFile} onChange={() => setFromFile(false)} className="accent-[var(--amb-a)]" />{t("livetvAdmin.fromUrl")}</label>
        <label className="flex items-center gap-2"><input type="radio" checked={fromFile} onChange={() => setFromFile(true)} className="accent-[var(--amb-a)]" />{t("livetvAdmin.fromFile")}</label>
      </div>
      {fromFile
        ? <TextField label={t("livetvAdmin.guideFile")} hint={t("livetvAdmin.fileHint")} value={filePath} maxLength={1000} placeholder="/config/livetv/guide.xml" onChange={(e) => setFilePath(e.target.value)} />
        : <TextField label={t("livetvAdmin.guideUrl")} hint={t("livetvAdmin.guideUrlHint")} value={url} maxLength={2000} placeholder="https://example.com/guide.xml.gz" onChange={(e) => setUrl(e.target.value)} />}
      <div className="grid gap-4 sm:grid-cols-2">
        <TextField label={t("livetvAdmin.timezone")} hint={t("livetvAdmin.timezoneHint")} value={timezone} maxLength={64} placeholder="Europe/Zagreb" onChange={(e) => setTimezone(e.target.value)} />
        <TextField label={t("livetvAdmin.refreshHours")} type="number" min={1} max={168} value={hours} onChange={(e) => setHours(e.target.value)} />
      </div>
      <label className="flex items-center gap-3 text-sm"><Toggle checked={enabled} onChange={setEnabled} label={t("livetvAdmin.enabled")} />{t("livetvAdmin.enabled")}</label>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" variant="secondary" disabled={!valid || test.isPending} onClick={() => test.mutate(body())}>
          {test.isPending ? <Spinner className="size-4" /> : null}{t("livetvAdmin.test")}
        </Button>
        <Button type="submit" disabled={!valid || save.isPending}>{guide ? t("common.save") : t("livetvAdmin.addGuide")}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t("common.cancel")}</Button>
      </div>
      <TestOutcome result={test.data} />
      <ErrorText error={test.error ?? save.error} />
    </form>
  );
}

function GuidesCard() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const when = useDateTime();
  const q = useQuery({ queryKey: K.guides, queryFn: async () => unwrap(await api.GET("/livetv/guides")), refetchInterval: 15_000 });
  const [editing, setEditing] = useState<GuideSource | "new" | null>(null);
  const refresh = useMutation({
    mutationFn: async (id: string) => unwrap(await api.POST("/livetv/guides/{guideId}/refresh", { params: { path: { guideId: id } } })),
  });
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/livetv/guides/{guideId}", { params: { path: { guideId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["livetv"] }),
  });
  const list = q.data ?? [];
  return (
    <Card title={t("livetvAdmin.guides")} actions={!editing && <Button onClick={() => setEditing("new")}><Plus className="size-4" aria-hidden />{t("livetvAdmin.addGuide")}</Button>}>
      <p className="mb-4 max-w-prose text-sm text-muted">{t("livetvAdmin.guidesIntro")}</p>
      {editing && (
        <div className="mb-5 rounded-[6px] border border-border bg-black/20 p-4">
          <GuideForm key={editing === "new" ? "new" : editing.id} guide={editing === "new" ? undefined : editing} onDone={() => setEditing(null)} />
        </div>
      )}
      {q.isPending ? <Spinner /> : list.length === 0 ? <p className="text-sm text-muted">{t("livetvAdmin.noGuides")}</p> : (
        <ul className="flex flex-col divide-y divide-border">
          {list.map((g) => (
            <li key={g.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 py-3" data-testid="guide-source-row">
              <div className="min-w-0 flex-1">
                <p className="flex items-center gap-2 font-medium">{g.name}{!g.enabled && <Badge>{t("livetvAdmin.disabled")}</Badge>}</p>
                <p className="truncate text-[13px] text-muted">
                  {[g.url ?? g.filePath, t("livetvAdmin.channelsN", { count: g.channelCount }), t("livetvAdmin.programsN", { count: g.programCount }),
                    t("livetvAdmin.everyHours", { count: g.refreshHours }),
                    g.lastRefreshAt ? t("livetvAdmin.refreshedAt", { when: when(g.lastRefreshAt) }) : t("livetvAdmin.notLoaded")].filter(Boolean).join(" · ")}
                </p>
                {g.lastResult && <p className="text-[13px] text-text/60">{g.lastResult}</p>}
                {g.error && <p className="text-[13px] text-danger">{g.error}</p>}
              </div>
              <div className="flex gap-2">
                <Button variant="ghost" aria-label={t("livetvAdmin.refreshNow")} title={t("livetvAdmin.refreshNow")}
                  disabled={refresh.isPending} onClick={() => refresh.mutate(g.id)}><RefreshCw className="size-4" aria-hidden /></Button>
                <Button variant="secondary" onClick={() => setEditing(g)}>{t("common.edit")}</Button>
                <Button variant="ghost" aria-label={t("common.delete")} title={t("common.delete")}
                  onClick={() => { if (window.confirm(t("livetvAdmin.deleteGuideConfirm", { name: g.name }))) remove.mutate(g.id); }}>
                  <Trash2 className="size-4" aria-hidden />
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      {refresh.isSuccess && <SuccessText>{t("livetvAdmin.refreshQueued")}</SuccessText>}
      <ErrorText error={q.error ?? refresh.error ?? remove.error} />
    </Card>
  );
}

function MappingRow({ row, guideOptions }: { row: ChannelAdmin; guideOptions: { value: string; label: string }[] }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const patch = useMutation({
    mutationFn: async (body: ChannelUpdate) =>
      unwrap(await api.PATCH("/livetv/channels/{channelId}", { params: { path: { channelId: row.channel.id } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["livetv"] }),
  });
  const mapped = row.guideSourceId && row.guideChannelId ? `${row.guideSourceId}|${row.guideChannelId}` : "";
  const mappedLabel = guideOptions.find((o) => o.value === mapped)?.label ?? row.guideChannelId;
  const ch = row.channel;
  return (
    <tr className="border-t border-border align-middle" data-testid="mapping-row">
      <td className="py-2.5 pr-3 text-xs text-text/55 tabular">{ch.number}</td>
      <td className="py-2.5 pr-3">
        <p className="font-medium">{ch.name}</p>
        <p className="text-xs text-muted">{[row.tunerName, row.videoCodec, row.audioCodec].filter(Boolean).join(" · ")}</p>
      </td>
      <td className="py-2.5 pr-3">
        <div className="flex flex-col gap-1.5">
          <Select aria-label={t("livetvAdmin.mappingFor", { name: ch.name })} value={row.mapping} disabled={patch.isPending}
            onChange={(e) => {
              const mapping = e.target.value as ChannelUpdate["mapping"];
              if (mapping === "manual") {
                const [guideSourceId, guideChannelId] = (mapped || guideOptions[0]?.value || "|").split("|");
                if (guideSourceId && guideChannelId) patch.mutate({ mapping, guideSourceId, guideChannelId });
              } else patch.mutate({ mapping });
            }}>
            <option value="auto">{t("livetvAdmin.mapping_auto")}</option>
            <option value="manual" disabled={guideOptions.length === 0}>{t("livetvAdmin.mapping_manual")}</option>
            <option value="none">{t("livetvAdmin.mapping_none")}</option>
          </Select>
          {row.mapping === "manual" ? (
            <Select aria-label={t("livetvAdmin.guideChannelFor", { name: ch.name })} value={mapped} disabled={patch.isPending}
              onChange={(e) => {
                const [guideSourceId, guideChannelId] = e.target.value.split("|");
                patch.mutate({ mapping: "manual", guideSourceId, guideChannelId });
              }}>
              {!mapped && <option value="">{t("livetvAdmin.chooseGuideChannel")}</option>}
              {guideOptions.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
            </Select>
          ) : row.mapping === "auto" ? (
            <span className="text-xs text-muted">{mapped ? t("livetvAdmin.autoMatched", { name: mappedLabel }) : t("livetvAdmin.autoUnmatched")}</span>
          ) : null}
        </div>
      </td>
      <td className="py-2.5 pr-3">
        <Select aria-label={t("livetvAdmin.categoryFor", { name: ch.name })} value={row.categoryOverride ?? "auto"} disabled={patch.isPending}
          onChange={(e) => patch.mutate({ category: e.target.value as ChannelUpdate["category"] })}>
          <option value="auto">{t("livetvAdmin.categoryAuto")}{ch.category ? ` (${t(`livetv.category.${ch.category}`)})` : ""}</option>
          {(["movies", "sports", "news", "kids"] as const).map((c) => <option key={c} value={c}>{t(`livetv.category.${c}`)}</option>)}
        </Select>
      </td>
      <td className="py-2.5">
        <Toggle checked={!row.hidden} onChange={(v) => patch.mutate({ hidden: !v })} label={t("livetvAdmin.visibleFor", { name: ch.name })} />
        <ErrorText error={patch.error} />
      </td>
    </tr>
  );
}

function MappingCard() {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: K.mapping, queryFn: async () => unwrap(await api.GET("/livetv/mapping")) });
  const [filter, setFilter] = useState("");
  const channels = (q.data?.channels ?? []).filter((r) => !filter || r.channel.name.toLowerCase().includes(filter.toLowerCase()) || r.channel.number.includes(filter));
  const guideOptions = (q.data?.guideChannels ?? []).map((g) => ({ value: `${g.sourceId}|${g.id}`, label: g.name === g.id ? g.name : `${g.name} (${g.id})` }));
  return (
    <Card title={t("livetvAdmin.mapping")}>
      <p className="mb-4 max-w-prose text-sm text-muted">{t("livetvAdmin.mappingIntro")}</p>
      {q.isPending ? <Spinner /> : (q.data?.channels.length ?? 0) === 0 ? <p className="text-sm text-muted">{t("livetvAdmin.noChannels")}</p> : (
        <>
          <input type="search" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t("livetvAdmin.filterChannels")} aria-label={t("livetvAdmin.filterChannels")}
            className="mb-3 h-9 w-full max-w-xs rounded-[6px] border border-border bg-black/30 px-3 text-sm text-text placeholder:text-faint focus:border-accent/70" />
          <div className="max-h-[60vh] overflow-auto">
            <table className="w-full text-left text-sm">
              <thead className="sticky top-0 bg-surface text-xs text-muted">
                <tr>
                  <th className="pb-2 pr-3 font-medium">#</th>
                  <th className="pb-2 pr-3 font-medium">{t("livetvAdmin.channel")}</th>
                  <th className="pb-2 pr-3 font-medium">{t("livetvAdmin.guideChannel")}</th>
                  <th className="pb-2 pr-3 font-medium">{t("livetvAdmin.category")}</th>
                  <th className="pb-2 font-medium">{t("livetvAdmin.visible")}</th>
                </tr>
              </thead>
              <tbody>{channels.map((r) => <MappingRow key={r.channel.id} row={r} guideOptions={guideOptions} />)}</tbody>
            </table>
          </div>
        </>
      )}
      <ErrorText error={q.error} />
    </Card>
  );
}

const numClass = "h-10 w-full rounded-[6px] border border-border bg-black/30 px-3 text-[15px] text-text tabular hover:border-border-strong focus:border-accent/70";

function DvrCard() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: me } = useMe();
  const admin = !!me?.isAdmin;
  const q = useQuery({ queryKey: K.settings, queryFn: async () => unwrap(await api.GET("/livetv/settings")) });
  const [draft, setDraft] = useState<LiveSettings | null>(null);
  const [argsText, setArgsText] = useState("");
  useEffect(() => {
    if (q.data) {
      setDraft(q.data);
      setArgsText(q.data.postProcess.args.join("\n"));
    }
  }, [q.data]);
  const save = useMutation({
    mutationFn: async (body: LiveSettings) => unwrap(await api.PUT("/livetv/settings", { body })),
    onSuccess: (d) => qc.setQueryData(K.settings, d),
  });
  if (q.isPending || !draft) return <Card title={t("livetvAdmin.dvr")}>{q.error ? <ErrorText error={q.error} /> : <Spinner />}</Card>;
  const set = (patch: Partial<LiveSettings>) => { save.reset(); setDraft({ ...draft, ...patch }); };
  const setRet = (patch: Partial<LiveSettings["retention"]>) => set({ retention: { ...draft.retention, ...patch } });
  const submit = () => {
    const { recordingsPath: _p, recordingsLibraryId: _l, ...rest } = draft;
    void _p; void _l;
    const body: LiveSettings = admin
      ? { ...rest, postProcess: { ...draft.postProcess, args: argsText.split("\n").map((a) => a.trim()).filter(Boolean) } }
      : rest;
    save.mutate(body);
  };
  return (
    <form data-testid="dvr-settings" onSubmit={(e) => { e.preventDefault(); submit(); }}>
      <Card title={t("livetvAdmin.dvr")}>
        <SettingRow label={t("livetvAdmin.recordingsPath")} hint={t("livetvAdmin.recordingsPathHint")}>
          {() => <code className="block truncate rounded-[6px] bg-black/30 px-3 py-2 text-sm">{draft.recordingsPath ?? "/recordings"}</code>}
        </SettingRow>
        <SettingRow label={t("livetvAdmin.prePadding")} hint={t("livetvAdmin.paddingHint")}>
          {(id) => <input id={id} type="number" min={0} max={60} className={numClass} value={Math.round(draft.prePaddingSeconds / 60)}
            onChange={(e) => set({ prePaddingSeconds: Number(e.target.value) * 60 })} />}
        </SettingRow>
        <SettingRow label={t("livetvAdmin.postPadding")}>
          {(id) => <input id={id} type="number" min={0} max={120} className={numClass} value={Math.round(draft.postPaddingSeconds / 60)}
            onChange={(e) => set({ postPaddingSeconds: Number(e.target.value) * 60 })} />}
        </SettingRow>
        <SettingRow label={t("livetvAdmin.timeshift")} hint={t("livetvAdmin.timeshiftHint")}>
          {(id) => <input id={id} type="number" min={1} max={240} className={numClass} value={draft.timeshiftMinutes}
            onChange={(e) => set({ timeshiftMinutes: Number(e.target.value) })} />}
        </SettingRow>
        <SettingRow label={t("livetvAdmin.retention")} hint={t("livetvAdmin.retentionHint")}>
          {(id) => <Toggle id={id} checked={draft.retention.enabled} onChange={(v) => setRet({ enabled: v })} label={t("livetvAdmin.retention")} />}
        </SettingRow>
        {draft.retention.enabled && (
          <>
            <SettingRow label={t("livetvAdmin.deleteAfterDays")} hint={t("livetvAdmin.zeroNever")}>
              {(id) => <input id={id} type="number" min={0} max={3650} className={numClass} value={draft.retention.deleteAfterDays}
                onChange={(e) => setRet({ deleteAfterDays: Number(e.target.value) })} />}
            </SettingRow>
            <SettingRow label={t("livetvAdmin.keepLast")} hint={t("livetvAdmin.keepLastHint")}>
              {(id) => <input id={id} type="number" min={0} max={1000} className={numClass} value={draft.retention.keepLast}
                onChange={(e) => setRet({ keepLast: Number(e.target.value) })} />}
            </SettingRow>
          </>
        )}
        <div className="border-t border-border pt-4">
          <h3 className="font-medium">{t("livetvAdmin.postProcess")}</h3>
          <p className="mt-0.5 mb-3 max-w-prose text-[13px] leading-snug text-text/55">{t("livetvAdmin.postProcessHint")}</p>
          {admin ? (
            <div className="flex flex-col gap-4" data-testid="post-process">
              <TextField label={t("livetvAdmin.command")} value={draft.postProcess.command} maxLength={1000} placeholder="/config/scripts/comskip.sh"
                onChange={(e) => set({ postProcess: { ...draft.postProcess, command: e.target.value } })} />
              <label className="flex flex-col gap-1.5 text-sm font-medium text-text/90">{t("livetvAdmin.args")}
                <textarea rows={4} value={argsText} onChange={(e) => { save.reset(); setArgsText(e.target.value); }} placeholder={"--input\n{path}"}
                  className="rounded-[6px] border border-border bg-black/30 px-3 py-2 font-mono text-sm text-text focus:border-accent/70" />
                <span className="text-xs font-normal text-muted">{t("livetvAdmin.argsHint")}</span>
              </label>
              <p className="flex flex-wrap gap-1.5" aria-label={t("livetvAdmin.placeholders")}>
                {PLACEHOLDERS.map((p) => <code key={p} className="rounded-[4px] bg-white/[0.07] px-1.5 py-0.5 text-xs">{p}</code>)}
              </p>
              <TextField label={t("livetvAdmin.timeoutMinutes")} type="number" min={1} max={1440} value={draft.postProcess.timeoutMinutes}
                onChange={(e) => set({ postProcess: { ...draft.postProcess, timeoutMinutes: Number(e.target.value) } })} />
            </div>
          ) : (
            <p className="text-sm text-muted" data-testid="post-process-readonly">
              {draft.postProcess.command ? t("livetvAdmin.postProcessSet", { command: draft.postProcess.command }) : t("livetvAdmin.postProcessOff")}
              {" "}{t("livetvAdmin.postProcessAdminOnly")}
            </p>
          )}
        </div>
        <div className="mt-5 flex flex-wrap items-center gap-4">
          <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
          {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
          <ErrorText error={save.error} />
        </div>
      </Card>
    </form>
  );
}

/** Settings → Live TV: tuners, guides, channel mapping and the DVR (admin or manageLiveTv). */
export function LiveTvSettings() {
  return (
    <div className="flex flex-col gap-6" data-testid="livetv-settings">
      <TunersCard />
      <GuidesCard />
      <MappingCard />
      <DvrCard />
    </div>
  );
}
