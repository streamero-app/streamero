import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Button, Card, ErrorText, TextField } from "@/components/ui";
import { useDateTime } from "./SettingsLayout";
import { useConfirm } from "@/components/Dialog";

export function ApiKeysSettings() {
  const confirmDialog = useConfirm();
  const { t } = useTranslation();
  const qc = useQueryClient();
  const fmt = useDateTime();
  const keys = useQuery({ queryKey: ["apikeys"], queryFn: async () => unwrap(await api.GET("/apikeys")) });
  const [name, setName] = useState("");
  const [secret, setSecret] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/apikeys", { body: { name } })),
    onSuccess: (res) => {
      setSecret(res.key);
      setCopied(false);
      setName("");
      void qc.invalidateQueries({ queryKey: ["apikeys"] });
    },
  });
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/apikeys/{apiKeyId}", { params: { path: { apiKeyId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["apikeys"] }),
  });

  const onCreate = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };
  const copy = async () => {
    if (!secret) return;
    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
    } catch {
      /* clipboard unavailable: the key is selectable */
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <Card title={t("apikeys.create")}>
        <p className="mb-4 text-sm text-muted">{t("apikeys.intro")}</p>
        <form className="flex flex-wrap items-end gap-3" onSubmit={onCreate}>
          <div className="min-w-56 flex-1">
            <TextField label={t("apikeys.name")} required maxLength={100} placeholder="Jellyseerr"
              value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <Button type="submit" disabled={create.isPending}>{t("apikeys.create")}</Button>
        </form>
        <ErrorText error={create.error} />
        {secret && (
          <div className="mt-4 flex flex-col gap-2 rounded-md border border-success/40 bg-success/10 p-4" data-testid="new-api-key">
            <p className="text-sm font-medium">{t("apikeys.copyNow")}</p>
            <code className="block overflow-x-auto rounded bg-bg px-3 py-2 font-mono text-sm break-all select-all">{secret}</code>
            <div className="flex gap-2">
              <Button variant="secondary" onClick={() => void copy()}>{copied ? t("apikeys.copied") : t("apikeys.copy")}</Button>
              <Button variant="ghost" onClick={() => setSecret(null)}>{t("apikeys.done")}</Button>
            </div>
          </div>
        )}
      </Card>

      <Card title={t("settings.apiKeys")}>
        <ErrorText error={keys.error ?? remove.error} />
        {keys.data?.length === 0 && <p className="text-sm text-muted">{t("apikeys.none")}</p>}
        <ul className="divide-y divide-border">
          {keys.data?.map((k) => (
            <li key={k.id} className="flex flex-wrap items-center gap-3 py-3" data-testid="api-key-row">
              <div className="min-w-0 flex-1">
                <p className="font-medium">{k.name} <code className="ml-1 text-xs text-muted">{k.prefix}…</code></p>
                <p className="text-xs text-muted">
                  {t("apikeys.meta", { user: k.createdBy, created: fmt(k.createdAt), used: k.lastUsedAt ? fmt(k.lastUsedAt) : t("apikeys.never") })}
                </p>
              </div>
              <Button variant="danger" disabled={remove.isPending}
                onClick={() => void confirmDialog({ message: t("apikeys.confirmRevoke", { name: k.name }), danger: true }).then((ok) => ok && remove.mutate(k.id))}>
                {t("apikeys.revoke")}
              </Button>
            </li>
          ))}
        </ul>
      </Card>
    </div>
  );
}
