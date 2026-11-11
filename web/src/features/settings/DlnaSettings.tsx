import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Badge, Button, Card, ErrorText, Select, Spinner, SuccessText } from "@/components/ui";
import { SettingRow, Toggle } from "./SettingRow";

type Settings = components["schemas"]["DlnaSettings"];
type Overview = components["schemas"]["DlnaOverview"];

const KEY = ["settings", "dlna"];

/** DLNA media server for smart TVs: on/off, the name TVs show, and who browsing acts as. */
export function DlnaSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: KEY, queryFn: async () => unwrap(await api.GET("/settings/dlna")) });
  const [draft, setDraft] = useState<Settings>({ enabled: false, friendlyName: "", language: "en" });
  useEffect(() => { if (q.data) setDraft(q.data.settings); }, [q.data]);
  const save = useMutation({
    mutationFn: async (body: Settings) => unwrap(await api.PUT("/settings/dlna", { body })),
    onSuccess: (d: Overview) => qc.setQueryData(KEY, d),
  });
  if (q.isPending) return <Spinner />;
  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => { save.reset(); setDraft({ ...draft, [k]: v }); };
  const users = q.data?.users ?? [];
  const user = users.find((u) => u.id === draft.userId);
  const status = q.data?.status;
  const needUser = draft.enabled && !draft.userId;

  return (
    <form data-testid="dlna-settings" onSubmit={(e) => { e.preventDefault(); if (!needUser) save.mutate(draft); }}>
      <Card title={t("dlna.title")} actions={status && (
        !status.available ? <Badge>{t("dlna.status_stopped")}</Badge>
          : status.running ? <Badge tone="success">{t("dlna.status_running", { addr: status.httpAddr })}</Badge>
            : <Badge>{t("dlna.status_stopped")}</Badge>
      )}>
        <p className="mb-4 max-w-prose text-sm text-muted">{t("dlna.intro")}</p>
        {status && !status.available && <p className="mb-4 text-sm text-muted">{t("dlna.status_unavailable")}</p>}
        <SettingRow label={t("dlna.enabled")} hint={t("dlna.enabledHint")}>
          {(id) => <Toggle id={id} checked={draft.enabled} onChange={(v) => set("enabled", v)} label={t("dlna.enabled")} />}
        </SettingRow>
        <SettingRow label={t("dlna.user")} hint={t("dlna.userHint")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={draft.userId ?? ""}
              onChange={(e) => { save.reset(); setDraft({ ...draft, userId: e.target.value || undefined, profileId: undefined }); }}>
              <option value="">{t("dlna.chooseUser")}</option>
              {users.map((u) => <option key={u.id} value={u.id}>{u.username}</option>)}
            </Select>
          )}
        </SettingRow>
        {user && user.profiles.length > 0 && (
          <SettingRow label={t("dlna.profile")}>
            {(id) => (
              <Select id={id} className="h-10 w-full" value={draft.profileId ?? ""} onChange={(e) => set("profileId", e.target.value || undefined)}>
                <option value="">{t("dlna.mainProfile")}</option>
                {user.profiles.map((p) => (
                  <option key={p.id} value={p.id}>{p.isKid ? t("dlna.kidProfile", { name: p.name }) : p.name}</option>
                ))}
              </Select>
            )}
          </SettingRow>
        )}
        <SettingRow label={t("dlna.friendlyName")} hint={t("dlna.friendlyNameHint")}>
          {(id) => (
            <input id={id} maxLength={64} value={draft.friendlyName} onChange={(e) => set("friendlyName", e.target.value)}
              className="h-10 w-full rounded-[6px] border border-border bg-black/30 px-3 text-[15px] text-text hover:border-border-strong focus:border-accent/70" />
          )}
        </SettingRow>
        <SettingRow label={t("dlna.language")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={draft.language} onChange={(e) => set("language", e.target.value as Settings["language"])}>
              <option value="en">{t("dlna.lang_en")}</option>
              <option value="hr">{t("dlna.lang_hr")}</option>
            </Select>
          )}
        </SettingRow>
        {status?.available && (
          <p className="mt-2 text-[13px] leading-snug text-text/55">
            {status.interfaces && status.interfaces.length > 0 ? `${t("dlna.interfaces", { list: status.interfaces.join(", ") })} · ` : ""}
            {t("dlna.noDiscovery")}
          </p>
        )}
        {status?.error && <p className="mt-2 text-[13px] text-danger">{status.error}</p>}
        <div className="mt-5 flex flex-wrap items-center gap-4">
          <Button type="submit" disabled={save.isPending || needUser}>{t("common.save")}</Button>
          {needUser && <span className="text-sm text-muted">{t("dlna.needUser")}</span>}
          {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
          <ErrorText error={save.error ?? q.error} />
        </div>
      </Card>
    </form>
  );
}
