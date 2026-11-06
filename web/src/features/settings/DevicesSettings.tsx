import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Card, ErrorText, TextField } from "@/components/ui";
import { useLogout, useMe } from "@/features/auth/useAuth";
import { useDateTime } from "./SettingsLayout";

export function DevicesSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const fmt = useDateTime();
  const { data: me } = useMe();
  const logout = useLogout();
  const sessions = useQuery({ queryKey: ["sessions"], queryFn: async () => unwrap(await api.GET("/sessions")) });
  const [editing, setEditing] = useState<{ id: string; name: string } | null>(null);
  const rename = useMutation({
    mutationFn: async (v: { id: string; name: string }) =>
      unwrap(await api.PATCH("/sessions/{sessionId}", { params: { path: { sessionId: v.id } }, body: { deviceName: v.name } })),
    onSuccess: () => {
      setEditing(null);
      void qc.invalidateQueries({ queryKey: ["sessions"] });
    },
  });
  const revoke = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/sessions/{sessionId}", { params: { path: { sessionId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sessions"] }),
  });

  return (
    <Card title={t("settings.devices")}>
      <p className="mb-4 text-sm text-muted">{me?.isAdmin ? t("devices.introAdmin") : t("devices.intro")}</p>
      <ErrorText error={sessions.error ?? revoke.error ?? rename.error} />
      <ul className="divide-y divide-border">
        {sessions.data?.map((s) => (
          <li key={s.id} className="flex flex-wrap items-center gap-3 py-3" data-testid="session-row">
            <div className="min-w-0 flex-1">
              <p className="font-medium">
                {s.deviceName || t("devices.unknown")} {s.current && <Badge tone="success">{t("devices.current")}</Badge>}
              </p>
              <p className="text-xs text-muted">
                {[me?.isAdmin ? s.username : null, s.client, t("devices.lastSeen", { when: fmt(s.lastSeenAt) })].filter(Boolean).join(" · ")}
              </p>
            </div>
            <Button variant="secondary" onClick={() => setEditing({ id: s.id, name: s.deviceName })}>{t("devices.rename")}</Button>
            <Button variant="danger" disabled={revoke.isPending || logout.isPending}
              onClick={() => s.current
                ? logout.mutate(undefined, { onSettled: () => void navigate({ to: "/login" }) })
                : revoke.mutate(s.id)}>
              {t("devices.signOut")}
            </Button>
            {editing?.id === s.id && (
              <form className="flex w-full flex-wrap items-end gap-2" onSubmit={(e) => {
                e.preventDefault();
                rename.mutate(editing);
              }}>
                <div className="min-w-56 flex-1">
                  <TextField label={t("devices.name")} required maxLength={100} autoFocus
                    value={editing.name} onChange={(e) => setEditing({ ...editing, name: e.target.value })} />
                </div>
                <Button type="submit" disabled={rename.isPending}>{t("common.save")}</Button>
                <Button type="button" variant="ghost" onClick={() => setEditing(null)}>{t("common.cancel")}</Button>
              </form>
            )}
          </li>
        ))}
      </ul>
    </Card>
  );
}
