import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ServerStartingError } from "@/api/client";
import { useSystemInfo } from "./useSystemInfo";

/** Blocks the UI until the server reports it is ready. */
export function ServerGate({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const { data, error, isPending, failureReason, refetch } = useSystemInfo();

  if (data) return <>{children}</>;
  const starting = failureReason instanceof ServerStartingError;
  return (
    <div role="status" className="flex h-full flex-col items-center justify-center gap-4 text-muted">
      <p>{isPending ? (starting ? t("server.starting") : t("server.checking")) : t("server.unreachable")}</p>
      {error && !isPending && (
        <button className="rounded-md bg-surface-2 px-4 py-2 text-text" onClick={() => void refetch()}>
          {t("server.retry")}
        </button>
      )}
    </div>
  );
}
