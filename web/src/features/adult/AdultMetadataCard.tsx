import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Card, ErrorText, SuccessText, TextField } from "@/components/ui";

/** ThePornDB and StashDB keys for adult libraries (D-053); shown only when one exists. */
export function AdultMetadataCard() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const libraries = useQuery({ queryKey: ["libraries", "all"], queryFn: async () => unwrap(await api.GET("/libraries")) });
  const hasAdult = Array.isArray(libraries.data) && libraries.data.some((l) => l.kind === "adult");
  const settings = useQuery({ queryKey: ["settings", "adult"], enabled: hasAdult,
    queryFn: async () => unwrap(await api.GET("/adult/settings")) });
  const [tpdb, setTpdb] = useState("");
  const [stash, setStash] = useState("");
  const save = useMutation({
    mutationFn: async (body: { tpdbApiKey?: string; stashdbApiKey?: string; clearTpdb?: boolean; clearStashdb?: boolean }) =>
      unwrap(await api.PUT("/adult/settings", { body })),
    onSuccess: (data) => {
      qc.setQueryData(["settings", "adult"], data);
      setTpdb("");
      setStash("");
    },
  });
  if (!hasAdult) return null;
  const s = settings.data;
  const status = (on?: boolean) => on ? <Badge tone="success">{t("adult.connected")}</Badge> : <Badge>{t("adult.notConnected")}</Badge>;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ tpdbApiKey: tpdb.trim() || undefined, stashdbApiKey: stash.trim() || undefined });
  };
  return (
    <Card title={t("adult.settingsTitle")}>
      <p className="mb-4 max-w-prose text-sm text-muted">{t("adult.settingsIntro")}</p>
      <form className="flex max-w-md flex-col gap-4" onSubmit={submit} data-testid="adult-metadata">
        <div className="flex flex-col gap-1.5">
          <span className="flex items-center gap-2 text-sm">ThePornDB {status(s?.tpdbConfigured)}</span>
          <TextField label={s?.tpdbConfigured ? t("adult.replaceKey") : t("adult.tpdbKey")} type="password" autoComplete="off" spellCheck={false}
            value={tpdb} onChange={(e) => setTpdb(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1.5">
          <span className="flex items-center gap-2 text-sm">StashDB {status(s?.stashdbConfigured)}</span>
          <TextField label={s?.stashdbConfigured ? t("adult.replaceKey") : t("adult.stashKey")} type="password" autoComplete="off" spellCheck={false}
            value={stash} onChange={(e) => setStash(e.target.value)} />
        </div>
        <ErrorText error={settings.error ?? save.error} />
        {save.isSuccess && <SuccessText>{t("adult.saved")}</SuccessText>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={save.isPending || (!tpdb.trim() && !stash.trim())}>{save.isPending ? t("adult.checking") : t("common.save")}</Button>
          {s?.tpdbConfigured && <Button type="button" variant="danger" disabled={save.isPending} onClick={() => save.mutate({ clearTpdb: true })}>{t("adult.removeTpdb")}</Button>}
          {s?.stashdbConfigured && <Button type="button" variant="danger" disabled={save.isPending} onClick={() => save.mutate({ clearStashdb: true })}>{t("adult.removeStash")}</Button>}
        </div>
      </form>
    </Card>
  );
}
