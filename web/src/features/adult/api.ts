import { useQuery } from "@tanstack/react-query";
import { api, ApiError, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";

/** Adult section (D-053): types and data hooks. Every query key starts with "adult". */
export type AdultStatus = components["schemas"]["AdultStatus"];
export type AdultScene = components["schemas"]["AdultScene"];
export type AdultSceneDetail = components["schemas"]["AdultSceneDetail"];
export type AdultStudio = components["schemas"]["AdultStudio"];
export type AdultPerformer = components["schemas"]["AdultPerformer"];

export type SceneQuery = {
  studioId?: string;
  performerId?: string;
  kind?: "scene" | "movie";
  q?: string;
  tag?: string;
  status?: "all" | "unwatched" | "inProgress" | "watched";
  sort?: "date" | "added" | "title" | "played";
};

export const statusKey = ["adult", "status"] as const;

export function useAdultStatus(enabled = true) {
  return useQuery({
    queryKey: statusKey,
    queryFn: async () => unwrap(await api.GET("/adult/status")),
    retry: false,
    enabled,
  });
}

/** The PIN has not been entered in this session. */
export function isLocked(err: unknown): boolean {
  return err instanceof ApiError && err.code === "adult_locked";
}

/** Artwork of a scene, studio or performer (served with the session; never cached by the browser). */
export function adultImage(owner: "scene" | "studio" | "performer", id: string, type: "poster" | "backdrop" | "image"): string {
  return `/api/v1/adult/images/${owner}/${id}/${type}`;
}

export const PAGE = 48;

export async function fetchScenes(q: SceneQuery, offset: number) {
  const query: Record<string, string | number> = { offset, limit: PAGE };
  for (const [k, v] of Object.entries(q)) if (v) query[k] = v;
  return unwrap(await api.GET("/adult/scenes", { params: { query } }));
}

/** "2024-01-15" → localised date ("" when absent). */
export function formatDate(date: string | undefined, lang: string): string {
  if (!date) return "";
  const d = new Date(date + "T00:00:00");
  return Number.isNaN(d.getTime()) ? date : d.toLocaleDateString(lang, { day: "numeric", month: "short", year: "numeric" });
}

/** Progress of a scene in percent (0 when not started or watched). */
export function sceneProgress(s: Pick<AdultScene, "positionMs" | "durationMs" | "played">): number {
  if (s.played || !s.positionMs || !s.durationMs) return 0;
  return Math.min(100, (s.positionMs / s.durationMs) * 100);
}
