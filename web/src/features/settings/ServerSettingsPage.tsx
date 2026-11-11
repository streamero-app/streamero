import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Button, Card, ErrorText, SuccessText, TextField } from "@/components/ui";
import { useSystemInfo } from "@/features/server/useSystemInfo";
import { DlnaSettings } from "./DlnaSettings";

export function ServerSettingsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: info } = useSystemInfo();
  const settings = useQuery({ queryKey: ["settings", "server"], queryFn: async () => unwrap(await api.GET("/settings/server")) });
  const [name, setName] = useState("");
  useEffect(() => {
    if (settings.data) setName(settings.data.serverName);
  }, [settings.data]);
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/settings/server", { body: { serverName: name } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["system", "info"] }),
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };

  return (
    <div className="flex flex-col gap-6">
      <Card title={t("settings.server")}>
        <form className="flex max-w-sm flex-col gap-4" onSubmit={onSubmit}>
          <TextField label={t("setup.serverName")} required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
          <ErrorText error={settings.error ?? save.error} />
          {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
          <Button type="submit" disabled={save.isPending || !settings.data} className="self-start">{t("common.save")}</Button>
        </form>
      </Card>
      <DlnaSettings />
      <Card title={t("server.about")}>
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
          <dt className="text-muted">{t("server.versionLabel")}</dt>
          <dd>{info?.version}{info?.commit ? ` (${info.commit})` : ""}</dd>
          <dt className="text-muted">{t("server.id")}</dt>
          <dd className="font-mono text-xs break-all">{info?.id}</dd>
          <dt className="text-muted">API</dt>
          <dd>{info?.apiVersion}</dd>
        </dl>
      </Card>
    </div>
  );
}
