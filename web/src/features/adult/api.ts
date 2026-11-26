import { useQuery } from "@tanstack/react-query";
import { api, ApiError, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";

/** Adult section (D-053): types and data hooks. Every query key starts with "adult". */
export type AdultStatus = components["schemas"]["AdultStatus"];
export type AdultScene = components["schemas"]["AdultScene"];
export type AdultSceneDetail = components["schemas"]["AdultSceneDetail"];
export type AdultStudio = components["schemas"]["AdultStudio"];
export type AdultPerformer = components["schemas"]["AdultPerformer"];
export type AdultGallery = components["schemas"]["AdultGallery"];
export type AdultGalleryDetail = components["schemas"]["AdultGalleryDetail"];
export type AdultGalleryImage = components["schemas"]["AdultGalleryImage"];
export type AdultTag = components["schemas"]["AdultTag"];
export type AdultHome = components["schemas"]["AdultHome"];

export type SceneQuery = {
  studioId?: string;
  performerId?: string;
  kind?: "scene" | "movie";
  q?: string;
  tag?: string;
  status?: "all" | "unwatched" | "inProgress" | "watched";
  sort?: "date" | "oldest" | "added" | "title" | "played" | "duration";
  favoritePerformers?: boolean;
};

export type GalleryQuery = {
  studioId?: string;
  performerId?: string;
  sceneId?: string;
  q?: string;
  tag?: string;
  sort?: "date" | "oldest" | "added" | "title" | "images";
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

/** A gallery picture: the cached thumbnail, the picture itself or the original as a download (D-058). */
export function galleryImage(id: string, variant: "thumb" | "full" | "download"): string {
  return `/api/v1/adult/gallery-images/${id}/${variant}`;
}

export const PAGE = 48;

export async function fetchGalleries(q: GalleryQuery, offset: number) {
  const query: Record<string, string | number> = { offset, limit: PAGE };
  for (const [k, v] of Object.entries(q)) if (v) query[k] = v;
  return unwrap(await api.GET("/adult/galleries", { params: { query } }));
}

export async function fetchScenes(q: SceneQuery, offset: number) {
  const query: Record<string, string | number | boolean> = { offset, limit: PAGE };
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

/** Next index of a lightbox (wraps around); used by keys, swipes and the slideshow. */
export function stepIndex(i: number, delta: number, count: number): number {
  if (count <= 0) return 0;
  return (((i + delta) % count) + count) % count;
}

/** Fit a picture of w×h into a box (contain), for zoom limits. */
export function clampZoom(z: number): number {
  return Math.min(4, Math.max(1, Math.round(z * 100) / 100));
}
