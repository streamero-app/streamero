import { useEffect } from "react";
import { useQueryErrorResetBoundary } from "@tanstack/react-query";
import { Link, useRouter, type ErrorComponentProps } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { RotateCcw } from "lucide-react";
import { Button, buttonVariants, pressable } from "./ui";

/** A chunk that no longer exists after a new release was deployed. */
export function isStaleChunk(error: unknown): boolean {
  const msg = error instanceof Error ? error.message : String(error ?? "");
  return /dynamically imported module|Importing a module script failed|error loading dynamically imported|Failed to fetch dynamically/i.test(msg);
}

/**
 * Shown instead of a page that failed to render or load: the rest of the app keeps
 * working, and "Try again" re-runs the page (and its failed queries).
 */
export function RouteError({ error, reset }: ErrorComponentProps) {
  const { t } = useTranslation();
  const router = useRouter();
  const queries = useQueryErrorResetBoundary();
  const stale = isStaleChunk(error);
  useEffect(() => { queries.reset(); }, [queries]);
  const retry = () => {
    if (stale) {
      window.location.reload();
      return;
    }
    reset();
    void router.invalidate();
  };
  return (
    <div role="alert" data-testid="route-error"
      className="flex min-h-[70vh] flex-col items-start justify-center gap-5 px-5 pt-24 sm:px-10 lg:px-16">
      <p className="font-label text-text/55">{t("errors.pageTitle")}</p>
      <h1 className="max-w-[22ch] font-display text-4xl text-balance sm:text-5xl">
        {stale ? t("errors.newVersion") : t("errors.pageFailed")}
      </h1>
      <div className="flex flex-wrap gap-3">
        <Button onClick={retry}><RotateCcw className="size-4" aria-hidden />{stale ? t("errors.reload") : t("errors.tryAgain")}</Button>
        <Link to="/" className={`${pressable} ${buttonVariants.secondary} h-10 rounded-[6px] px-4 text-sm`}>{t("notFound.back")}</Link>
      </div>
    </div>
  );
}
