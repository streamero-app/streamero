import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Card, ErrorText, SuccessText, TextField } from "@/components/ui";

const LANGUAGES = ["en-US", "hr-HR", "en-GB", "de-DE", "sr-RS", "bs-BA", "sl-SI", "it-IT", "fr-FR", "es-ES"];
const COUNTRIES = ["US", "HR", "GB", "DE", "AT", "RS", "BA", "SI", "IT", "FR", "ES"];

export function MetadataSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: ["settings", "metadata"], queryFn: async () => unwrap(await api.GET("/settings/metadata")) });
  const [form, setForm] = useState({ key: "", language: "en-US", country: "US" });
  useEffect(() => {
    if (settings.data) setForm((f) => ({ ...f, language: settings.data.language, country: settings.data.country }));
  }, [settings.data]);
  const save = useMutation({
    mutationFn: async (remove: boolean) =>
      unwrap(await api.PUT("/settings/metadata", {
        body: { language: form.language, country: form.country, tmdbApiKey: form.key.trim() || undefined, removeTmdbKey: remove || undefined },
      })),
    onSuccess: (data) => {
      qc.setQueryData(["settings", "metadata"], data);
      setForm((f) => ({ ...f, key: "" }));
    },
  });
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate(false);
  };
  const configured = settings.data?.tmdbConfigured;

  return (
    <Card title={<span className="flex items-center gap-2">{t("settings.metadata")} {configured
      ? <Badge tone="success">{t("metadata.connected")}</Badge> : <Badge>{t("metadata.notConnected")}</Badge>}</span>}>
      <p className="mb-4 text-sm text-muted">{t("metadata.intro")}</p>
      <form className="flex max-w-md flex-col gap-4" onSubmit={onSubmit}>
        <TextField label={configured ? t("metadata.replaceKey") : t("metadata.key")} type="password" autoComplete="off"
          spellCheck={false} value={form.key} onChange={(e) => setForm({ ...form, key: e.target.value })}
          hint={<>{t("metadata.keyHint")} <a className="text-accent" href="https://www.themoviedb.org/settings/api" target="_blank" rel="noreferrer">themoviedb.org/settings/api</a></>} />
        <label className="flex flex-col gap-1 text-sm">
          {t("metadata.language")}
          <select value={form.language} onChange={(e) => setForm({ ...form, language: e.target.value })}
            className="rounded-md border border-border bg-bg px-3 py-2 text-base text-text">
            {LANGUAGES.map((l) => <option key={l} value={l}>{l}</option>)}
          </select>
        </label>
        <label className="flex flex-col gap-1 text-sm">
          {t("metadata.country")}
          <select value={form.country} onChange={(e) => setForm({ ...form, country: e.target.value })}
            className="rounded-md border border-border bg-bg px-3 py-2 text-base text-text">
            {COUNTRIES.map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
        </label>
        <ErrorText error={settings.error ?? save.error} />
        {save.isSuccess && <SuccessText>{t("metadata.saved")}</SuccessText>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={save.isPending}>{save.isPending ? t("metadata.checking") : t("common.save")}</Button>
          {configured && (
            <Button type="button" variant="danger" disabled={save.isPending}
              onClick={() => window.confirm(t("metadata.confirmRemove")) && save.mutate(true)}>{t("metadata.removeKey")}</Button>
          )}
        </div>
      </form>
    </Card>
  );
}
