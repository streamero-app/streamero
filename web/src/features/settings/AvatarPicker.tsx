import { useRef } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Camera, Trash2 } from "lucide-react";
import { ApiError } from "@/api/client";
import { ErrorText } from "@/components/ui";
import { Avatar, avatarUrl, bumpAvatar } from "@/features/profiles/ProfileSwitcher";
import { meQueryKey } from "@/features/auth/useAuth";

/**
 * A picture that can be changed in place: click (or Enter) to choose a file, the
 * server crops it to a square. `profileId` targets a profile, otherwise the account.
 */
export function AvatarPicker({ ownerId, profileId, name, color, hasAvatar, size = 96 }: {
  ownerId: string; profileId?: string; name: string; color: string; hasAvatar?: boolean; size?: number;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const path = profileId ? `/api/v1/profiles/${profileId}/avatar` : "/api/v1/auth/me/avatar";
  const done = () => {
    bumpAvatar(ownerId);
    void qc.invalidateQueries({ queryKey: meQueryKey });
    void qc.invalidateQueries({ queryKey: ["profiles"] });
  };
  const upload = useMutation({
    mutationFn: async (file: File) => {
      const res = await fetch(path, { method: "PUT", body: file, credentials: "include", headers: { "Content-Type": "application/octet-stream" } });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { code?: string; message?: string };
        throw new ApiError(res.status, body.code ?? "generic", body.message ?? "");
      }
    },
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: async () => { await fetch(path, { method: "DELETE", credentials: "include" }); },
    onSuccess: done,
  });
  return (
    <div className="flex flex-col items-start gap-2">
      <div className="group relative">
        <button type="button" onClick={() => input.current?.click()} aria-label={t("account.changePicture")}
          className="tv-focus relative block rounded-md">
          <Avatar name={name} color={color} size={size} image={hasAvatar ? avatarUrl(ownerId) : null} />
          <span className="absolute inset-0 grid place-items-center rounded-md bg-black/55 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
            <Camera className="size-6 text-white" aria-hidden />
          </span>
        </button>
        {hasAvatar && (
          <button type="button" onClick={() => remove.mutate()} aria-label={t("account.removePicture")}
            className="absolute -right-2 -bottom-2 grid size-8 place-items-center rounded-full bg-surface-3 text-text/80 ring-2 ring-surface hover:text-danger">
            <Trash2 className="size-4" aria-hidden />
          </button>
        )}
      </div>
      <input ref={input} type="file" accept="image/jpeg,image/png,image/gif" className="sr-only" tabIndex={-1} aria-hidden
        onChange={(e) => { const f = e.target.files?.[0]; if (f) upload.mutate(f); e.target.value = ""; }} data-testid="avatar-input" />
      <ErrorText error={upload.error} />
    </div>
  );
}
