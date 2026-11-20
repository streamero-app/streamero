import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Users } from "lucide-react";
import { ApiError } from "@/api/client";
import { buttonVariants, pressable } from "@/components/ui";
import { useSyncPlay } from "./SyncPlay";

const bigButton = "tv-focus h-13 rounded-[6px] px-7 text-base font-semibold";

/** A refused watch-together action, in words. */
export function TogetherError({ error }: { error: unknown }) {
  const { t } = useTranslation();
  if (!error) return null;
  const code = error instanceof ApiError ? error.code : typeof error === "string" ? error : "generic";
  return <p role="alert" className="text-sm text-danger">{t(`together.errors.${code}`, { defaultValue: t("errors.generic") })}</p>;
}

/**
 * "Watch together" on a title page: starts a group with the title and opens the player.
 * Its own small module, so the title page does not load the whole watch-together section.
 */
export function WatchTogetherButton({ itemId }: { itemId: string }) {
  const { t } = useTranslation();
  const sync = useSyncPlay();
  const navigate = useNavigate();
  const [error, setError] = useState<unknown>(null);
  // One group per press: a second click while the first is starting does nothing.
  const [busy, setBusy] = useState(false);
  if (!sync.allowed) return null;
  return (
    <>
      <button type="button" data-testid="watch-together" disabled={busy} className={`${pressable} ${buttonVariants.glass} ${bigButton}`}
        onClick={() => {
          if (busy) return;
          setBusy(true);
          setError(null);
          sync.create([itemId])
            .then(() => navigate({ to: "/watch/$itemId", params: { itemId }, search: { together: 1 } }), setError)
            .finally(() => setBusy(false));
        }}>
        <Users className="size-5" aria-hidden />{t("together.title")}
      </button>
      {error ? <TogetherError error={error} /> : null}
    </>
  );
}
