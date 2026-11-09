import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { CheckCircle2, XCircle } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Button, Card, ErrorText, Spinner, SuccessText } from "@/components/ui";
import { SettingRow, Toggle } from "./SettingRow";

const field = "h-10 w-full rounded-[6px] border border-border bg-black/30 px-3 text-[15px] text-text placeholder:text-faint hover:border-border-strong focus:border-accent/70";
type App = "seerr" | "radarr" | "sonarr" | "bazarr";

/** Connections for Discover: Seerr for search and requests, Radarr/Sonarr for the release calendar. */
export function RequestsSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const cur = useQuery({ queryKey: ["settings", "requests"], queryFn: async () => unwrap(await api.GET("/settings/requests")) });
  const [form, setForm] = useState({ seerrUrl: "", seerrApiKey: "", radarrUrl: "", radarrApiKey: "", sonarrUrl: "", sonarrApiKey: "", bazarrUrl: "", bazarrApiKey: "", usersCanRequest: true });
  const [remove, setRemove] = useState<App[]>([]);
  useEffect(() => {
    if (cur.data) setForm((f) => ({ ...f, seerrUrl: cur.data.seerrUrl ?? "", radarrUrl: cur.data.radarrUrl ?? "", sonarrUrl: cur.data.sonarrUrl ?? "", bazarrUrl: cur.data.bazarrUrl ?? "", usersCanRequest: cur.data.usersCanRequest }));
  }, [cur.data]);
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/settings/requests", { body: { ...form, removeKeys: remove } })),
    onSuccess: (d) => {
      qc.setQueryData(["settings", "requests"], d);
      setForm((f) => ({ ...f, seerrApiKey: "", radarrApiKey: "", sonarrApiKey: "", bazarrApiKey: "" }));
      setRemove([]);
      void qc.invalidateQueries({ queryKey: ["discover"] });
    },
  });
  const test = useMutation({ mutationFn: async () => unwrap(await api.POST("/settings/requests/test")) });
  if (cur.isPending) return <Spinner />;
  const d = cur.data;
  const app = (id: App, name: string, placeholder: string, configured?: boolean) => (
    <>
      <SettingRow label={t("requests.url", { app: name })}>
        {(htmlId) => <input id={htmlId} className={field} placeholder={placeholder} value={form[`${id}Url`]} onChange={(e) => setForm({ ...form, [`${id}Url`]: e.target.value })} />}
      </SettingRow>
      <SettingRow label={t("requests.key", { app: name })} hint={configured ? t("requests.keyStored") : t("requests.keyWhere", { app: name })}>
        {(htmlId) => (
          <div className="flex gap-2">
            <input id={htmlId} type="password" autoComplete="off" className={field} placeholder={configured ? "••••••••" : ""}
              value={form[`${id}ApiKey`]} onChange={(e) => setForm({ ...form, [`${id}ApiKey`]: e.target.value })} />
            {configured && (
              <Button type="button" variant={remove.includes(id) ? "danger" : "ghost"} onClick={() => setRemove(remove.includes(id) ? remove.filter((x) => x !== id) : [...remove, id])}>
                {remove.includes(id) ? t("requests.willRemove") : t("requests.remove")}
              </Button>
            )}
          </div>
        )}
      </SettingRow>
    </>
  );
  return (
    <form className="flex flex-col gap-6" onSubmit={(e) => { e.preventDefault(); save.reset(); save.mutate(); }}>
      <Card title="Seerr / Jellyseerr">
        <p className="mb-4 text-sm text-muted">{t("requests.seerrIntro")}</p>
        {app("seerr", "Seerr", "http://seerr:5055", d?.seerrConfigured)}
        <SettingRow label={t("requests.usersCanRequest")} hint={t("requests.usersCanRequestHint")}>
          {(id) => <Toggle id={id} checked={form.usersCanRequest} onChange={(v) => setForm({ ...form, usersCanRequest: v })} />}
        </SettingRow>
      </Card>
      <Card title="Radarr / Sonarr">
        <p className="mb-4 text-sm text-muted">{t("requests.arrIntro")}</p>
        {app("radarr", "Radarr", "http://radarr:7878", d?.radarrConfigured)}
        {app("sonarr", "Sonarr", "http://sonarr:8989", d?.sonarrConfigured)}
      </Card>
      <Card title="Bazarr">
        <p className="mb-4 text-sm text-muted">{t("requests.bazarrIntro")}</p>
        {app("bazarr", "Bazarr", "http://bazarr:6767", d?.bazarrConfigured)}
      </Card>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
        <Button type="button" variant="secondary" disabled={test.isPending} onClick={() => test.mutate()}>
          {test.isPending && <Spinner className="size-4" />}{t("requests.test")}
        </Button>
        {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
        <ErrorText error={save.error ?? test.error ?? cur.error} />
      </div>
      {test.data && (
        <ul className="flex flex-col gap-2" data-testid="connection-checks">
          {test.data.length === 0 && <li className="text-sm text-muted">{t("requests.nothingToTest")}</li>}
          {test.data.map((c) => (
            <li key={c.app} className="flex items-center gap-2 text-sm">
              {c.ok ? <CheckCircle2 className="size-4 text-success" aria-hidden /> : <XCircle className="size-4 text-danger" aria-hidden />}
              <span className="font-semibold">{c.app}</span>
              <span className="text-text/60">{c.ok ? t("requests.connected", { version: c.version ?? "" }) : c.error}</span>
            </li>
          ))}
        </ul>
      )}
    </form>
  );
}
