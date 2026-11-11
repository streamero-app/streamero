import { useCallback, useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";

export type ProgressInput = { page?: number; locator?: string; progression: number; finished?: boolean };

/**
 * Saves the reading position at most every 1.5 s while reading, and once more when
 * the reader closes or the tab is hidden, so every client resumes at the same place.
 */
export function useReadingProgress(itemId: string) {
  const qc = useQueryClient();
  const pending = useRef<ProgressInput | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const flush = useCallback(() => {
    window.clearTimeout(timer.current);
    timer.current = undefined;
    const body = pending.current;
    pending.current = null;
    if (!body) return;
    void api.PUT("/items/{itemId}/reading-progress", { params: { path: { itemId } }, body, keepalive: true })
      .then(() => { void qc.invalidateQueries({ queryKey: ["item", itemId] }); });
  }, [itemId, qc]);
  const save = useCallback((p: ProgressInput) => {
    pending.current = { ...p, progression: Math.min(1, Math.max(0, p.progression)) };
    if (timer.current === undefined) timer.current = window.setTimeout(flush, 1500);
  }, [flush]);
  useEffect(() => {
    const onHide = () => { if (document.visibilityState === "hidden") flush(); };
    document.addEventListener("visibilitychange", onHide);
    window.addEventListener("pagehide", flush);
    return () => {
      document.removeEventListener("visibilitychange", onHide);
      window.removeEventListener("pagehide", flush);
      flush();
    };
  }, [flush]);
  return save;
}
