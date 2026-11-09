import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Lock } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, ErrorText, Spinner, TextField } from "@/components/ui";
import { Mark } from "@/components/Logo";
import { useMe } from "@/features/auth/useAuth";

export type Profile = components["schemas"]["Profile"];
export const MAIN_COLOR = "#8a5a2b";
const PICKED = "streamero.profilePicked";

export function profilePicked(): boolean {
  try {
    return sessionStorage.getItem(PICKED) === "1";
  } catch {
    return true;
  }
}
export function markProfilePicked(v = true) {
  try {
    if (v) sessionStorage.setItem(PICKED, "1");
    else sessionStorage.removeItem(PICKED);
  } catch {
    /* private mode: the gate simply shows again */
  }
}

const bust: Record<string, number> = {};

/** The picture URL of a user or profile; bumped after an upload so the new one shows at once. */
export function avatarUrl(id: string): string {
  return `/api/v1/avatars/${id}${bust[id] ? `?v=${bust[id]}` : ""}`;
}

/** Call after uploading or removing a picture. */
export function bumpAvatar(id: string) {
  bust[id] = Date.now();
}

export function Avatar({ name, color, size = 28, className = "", fluid = false, image }: {
  name: string; color: string; size?: number; className?: string; fluid?: boolean; image?: string | null;
}) {
  const [broken, setBroken] = useState(false);
  const dims = fluid ? {} : { width: size, height: size, fontSize: size * 0.42 };
  return (
    <span aria-hidden className={`relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md font-bold text-white ${className}`}
      style={{ background: `linear-gradient(160deg, ${color}, color-mix(in srgb, ${color} 55%, black))`, ...dims }}>
      {(name ?? "?").slice(0, 1).toUpperCase()}
      {image && !broken && <img src={image} alt="" draggable={false} onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover" />}
    </span>
  );
}

export function useProfiles() {
  return useQuery({ queryKey: ["profiles"], queryFn: async () => unwrap(await api.GET("/profiles")) });
}

/** "Who's watching?" — full-screen profile picker shown after sign-in. */
export function ProfileGatePage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data: me, isPending } = useMe();
  const profiles = useProfiles();
  const [target, setTarget] = useState<{ id: string | null; needs: "pin" | "password"; name: string } | null>(null);
  const [secret, setSecret] = useState("");

  const select = useMutation({
    mutationFn: async (v: { id: string | null; secret?: string }) =>
      v.id === null
        ? unwrap(await api.POST("/profiles/main/select", { body: { password: v.secret ?? "" } }))
        : unwrap(await api.POST("/profiles/{profileId}/select", { params: { path: { profileId: v.id } }, body: { pin: v.secret } })),
    onSuccess: async () => {
      markProfilePicked();
      setTarget(null);
      setSecret("");
      await qc.invalidateQueries();
      void navigate({ to: "/" });
    },
  });

  if (isPending) return <div className="flex h-full items-center justify-center"><Spinner /></div>;
  if (!me) return <Navigate to="/login" />;
  const current = me.currentProfile;
  const inKid = !!current?.isKid;

  const choose = (p: Profile | null) => {
    select.reset();
    const active = p === null ? !current : current?.id === p.id;
    if (active) {
      markProfilePicked();
      void navigate({ to: "/" });
    } else if (p === null) {
      if (inKid) setTarget({ id: null, needs: "password", name: me.username });
      else select.mutate({ id: null });
    } else if (p.hasPin) {
      setTarget({ id: p.id, needs: "pin", name: p.name });
    } else {
      select.mutate({ id: p.id });
    }
  };
  const onSecret = (e: FormEvent) => {
    e.preventDefault();
    if (target) select.mutate({ id: target.id, secret });
  };
  const all: (Profile | null)[] = [null, ...(profiles.data ?? [])];

  return (
    <div className="flex min-h-full flex-col items-center justify-center gap-12 bg-bg px-6 py-16" data-testid="profile-gate">
      <Mark size={40} className="absolute top-6 left-6 sm:left-10" />
      <h1 className="enter text-center font-display text-4xl text-balance sm:text-6xl">{t("profiles.whoIsWatching")}</h1>
      <ul className="enter-2 flex flex-wrap justify-center gap-6 sm:gap-10">
        {all.map((p) => {
          const name = p?.name ?? me.displayName ?? me.username;
          return (
            <li key={p?.id ?? "main"}>
              <button type="button" onClick={() => choose(p)} disabled={select.isPending}
                className="group flex w-28 flex-col items-center gap-3 rounded-[6px] outline-none sm:w-40">
                <span className="relative block rounded-[6px] outline-3 outline-offset-4 outline-transparent transition-[outline-color,transform] duration-200 ease-out group-hover:outline-text group-focus-visible:-translate-y-1 group-focus-visible:outline-text">
                  <Avatar name={name} color={p?.color ?? MAIN_COLOR} fluid className="size-28 text-5xl sm:size-40 sm:text-7xl"
                    image={p ? (p.hasAvatar ? avatarUrl(p.id) : null) : me.hasAvatar ? avatarUrl(me.id) : null} />
                  {p?.hasPin && <Lock className="absolute right-2 bottom-2 size-5 text-white/80" role="img" aria-label={t("profiles.pinLocked")} />}
                </span>
                <span className="text-base text-muted transition-colors group-hover:text-text group-focus-visible:text-text sm:text-lg">
                  {name}{p?.isKid ? ` · ${t("profiles.kid")}` : ""}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
      {target && (
        <form className="enter flex w-full max-w-xs flex-col gap-3" onSubmit={onSecret}>
          <TextField label={target.needs === "pin" ? t("profiles.pin") : t("profiles.passwordToLeave")}
            type="password" inputMode={target.needs === "pin" ? "numeric" : undefined} autoFocus required
            value={secret} onChange={(e) => setSecret(e.target.value)} />
          <Button type="submit" disabled={select.isPending}>{t("profiles.open")}</Button>
        </form>
      )}
      <ErrorText error={select.error} />
    </div>
  );
}
