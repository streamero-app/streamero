import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Card, ErrorText, TextField } from "@/components/ui";
import { useConfirm } from "@/components/Dialog";
import { useDateTime } from "./SettingsLayout";

const EXPIRY = [24, 24 * 7, 24 * 30] as const;

export function InvitesSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const fmt = useDateTime();
  const confirmDialog = useConfirm();
  const invites = useQuery({ queryKey: ["invites"], queryFn: async () => unwrap(await api.GET("/invites")) });
  const [form, setForm] = useState({ note: "", expiresInHours: 24 * 7, maxUses: 1 });
  const [link, setLink] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/invites", { body: form })),
    onSuccess: (res) => {
      setLink(`${window.location.origin}/invite/${res.token}`);
      setCopied(false);
      setForm({ ...form, note: "" });
      void qc.invalidateQueries({ queryKey: ["invites"] });
    },
  });
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/invites/{inviteId}", { params: { path: { inviteId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["invites"] }),
  });
  const onCreate = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };
  const share = async () => {
    if (!link) return;
    try {
      if (navigator.share) await navigator.share({ url: link });
      else {
        await navigator.clipboard.writeText(link);
        setCopied(true);
      }
    } catch {
      /* cancelled or unavailable: the link is selectable */
    }
  };
  const now = Date.now();

  return (
    <div className="flex flex-col gap-6">
      <Card title={t("invites.create")}>
        <p className="mb-4 text-sm text-muted">{t("invites.intro")}</p>
        <form className="grid gap-4 sm:grid-cols-3" onSubmit={onCreate}>
          <TextField label={t("invites.note")} maxLength={100} value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} />
          <label className="flex flex-col gap-1 text-sm">
            {t("invites.expires")}
            <select value={form.expiresInHours} onChange={(e) => setForm({ ...form, expiresInHours: Number(e.target.value) })}
              className="rounded-md border border-border bg-bg px-3 py-2 text-base text-text">
              {EXPIRY.map((h) => <option key={h} value={h}>{t(`invites.expiry_${h}`)}</option>)}
            </select>
          </label>
          <TextField label={t("invites.maxUses")} type="number" min={1} max={100} value={form.maxUses}
            onChange={(e) => setForm({ ...form, maxUses: Math.max(1, Math.min(100, Number(e.target.value) || 1)) })} />
          <div className="sm:col-span-3">
            <Button type="submit" disabled={create.isPending}>{t("invites.create")}</Button>
          </div>
        </form>
        <ErrorText error={create.error} />
        {link && (
          <div className="mt-4 flex flex-col gap-2 rounded-md border border-success/40 bg-success/10 p-4" data-testid="new-invite">
            <p className="text-sm font-medium">{t("invites.copyNow")}</p>
            <code className="block overflow-x-auto rounded bg-bg px-3 py-2 font-mono text-sm break-all select-all">{link}</code>
            <div className="flex gap-2">
              <Button variant="secondary" onClick={() => void share()}>{copied ? t("apikeys.copied") : t("invites.share")}</Button>
              <Button variant="ghost" onClick={() => setLink(null)}>{t("apikeys.done")}</Button>
            </div>
          </div>
        )}
      </Card>

      <Card title={t("settings.invites")}>
        <ErrorText error={invites.error ?? remove.error} />
        {invites.data?.length === 0 && <p className="text-sm text-muted">{t("invites.none")}</p>}
        <ul className="divide-y divide-border">
          {invites.data?.map((i) => {
            const active = new Date(i.expiresAt).getTime() > now && i.uses < i.maxUses;
            return (
              <li key={i.id} className="flex flex-wrap items-center gap-3 py-3" data-testid="invite-row">
                <div className="min-w-0 flex-1">
                  <p className="font-medium">
                    {i.note || t("invites.untitled")} {active ? <Badge tone="success">{t("invites.active")}</Badge> : <Badge>{t("invites.inactive")}</Badge>}
                  </p>
                  <p className="text-xs text-muted">{t("invites.meta", { uses: i.uses, max: i.maxUses, expires: fmt(i.expiresAt), user: i.createdBy })}</p>
                </div>
                <Button variant="danger" disabled={remove.isPending} onClick={() => void confirmDialog({ message: t("invites.confirmRevoke", { name: i.note || t("invites.untitled") }), confirmLabel: t("invites.revoke"), danger: true })
                    .then((ok) => ok && remove.mutate(i.id))}>{t("invites.revoke")}</Button>
              </li>
            );
          })}
        </ul>
      </Card>
    </div>
  );
}
