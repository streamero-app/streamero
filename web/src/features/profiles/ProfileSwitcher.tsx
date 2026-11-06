import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, ErrorText, TextField } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";

export type Profile = components["schemas"]["Profile"];

export function Avatar({ name, color, size = 28 }: { name: string; color: string; size?: number }) {
  return (
    <span aria-hidden className="inline-flex shrink-0 items-center justify-center rounded-md font-semibold text-white"
      style={{ background: color, width: size, height: size, fontSize: size * 0.45 }}>
      {name.slice(0, 1).toUpperCase()}
    </span>
  );
}

const MAIN_COLOR = "#6e6e80";

/** Header control: shows the active profile and switches between profiles. */
export function ProfileSwitcher() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data: me } = useMe();
  const profiles = useQuery({ queryKey: ["profiles"], queryFn: async () => unwrap(await api.GET("/profiles")) });
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState<{ id: string | null; needs: "pin" | "password" } | null>(null);
  const [secret, setSecret] = useState("");

  const select = useMutation({
    mutationFn: async (v: { id: string | null; secret?: string }) =>
      v.id === null
        ? unwrap(await api.POST("/profiles/main/select", { body: { password: v.secret ?? "" } }))
        : unwrap(await api.POST("/profiles/{profileId}/select", { params: { path: { profileId: v.id } }, body: { pin: v.secret } })),
    onSuccess: async () => {
      setTarget(null);
      setSecret("");
      setOpen(false);
      await qc.invalidateQueries();
      void navigate({ to: "/" });
    },
  });

  if (!me || (profiles.data?.length ?? 0) === 0) return null;
  const current = me.currentProfile;
  const inKid = !!current?.isKid;

  const choose = (p: Profile | null) => {
    select.reset();
    if (p === null) {
      if (inKid) setTarget({ id: null, needs: "password" });
      else select.mutate({ id: null });
    } else if (p.hasPin) {
      setTarget({ id: p.id, needs: "pin" });
    } else {
      select.mutate({ id: p.id });
    }
  };
  const onSecret = (e: FormEvent) => {
    e.preventDefault();
    if (target) select.mutate({ id: target.id, secret });
  };

  return (
    <div className="relative">
      <button type="button" className="flex items-center gap-2 rounded-md px-2 py-1 hover:bg-surface-2"
        aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)} data-testid="profile-switcher">
        <Avatar name={current?.name ?? me.username} color={current?.color ?? MAIN_COLOR} />
        <span className="hidden text-sm sm:inline">{current?.name ?? me.username}</span>
      </button>
      {open && (
        <div role="menu" className="absolute right-0 z-20 mt-2 w-64 rounded-card border border-border bg-surface p-2 shadow-2xl">
          <p className="px-2 py-1 text-xs text-muted">{t("profiles.switch")}</p>
          {[null, ...(profiles.data ?? [])].map((p) => {
            const active = p === null ? !current : current?.id === p.id;
            return (
              <button key={p?.id ?? "main"} role="menuitem" type="button" disabled={active || select.isPending}
                onClick={() => choose(p)}
                className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-sm hover:bg-surface-2 disabled:opacity-60">
                <Avatar name={p?.name ?? me.username} color={p?.color ?? MAIN_COLOR} />
                <span className="flex-1">{p?.name ?? me.username}</span>
                {p?.isKid && <span className="text-xs text-muted">{t("profiles.kid")}</span>}
                {p?.hasPin && <span role="img" aria-label={t("profiles.pinLocked")}>🔒</span>}
              </button>
            );
          })}
          {target && (
            <form className="mt-2 flex flex-col gap-2 border-t border-border px-2 pt-3" onSubmit={onSecret}>
              <TextField label={target.needs === "pin" ? t("profiles.pin") : t("profiles.passwordToLeave")}
                type="password" inputMode={target.needs === "pin" ? "numeric" : undefined} autoFocus required
                value={secret} onChange={(e) => setSecret(e.target.value)} />
              <Button type="submit" disabled={select.isPending}>{t("profiles.open")}</Button>
            </form>
          )}
          <div className="px-2"><ErrorText error={select.error} /></div>
        </div>
      )}
    </div>
  );
}
