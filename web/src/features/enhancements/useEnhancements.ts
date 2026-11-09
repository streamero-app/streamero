import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";

export type Enhancements = components["schemas"]["Enhancements"];

const OFF: Enhancements = { qualityTags: false, randomButton: false, pauseScreen: false, arrLinks: false, discoverInSearch: false, removeFromContinue: false, shortcutsHelp: false, autoPip: false };

/** Built-in enhancements switched on by the administrator (Settings → Enhancements). */
export function useEnhancements(): Enhancements {
  const q = useQuery({ queryKey: ["enhancements"], queryFn: async () => unwrap(await api.GET("/enhancements")), staleTime: 5 * 60_000 });
  return q.data && typeof q.data === "object" && "qualityTags" in q.data ? q.data : OFF;
}

/** Opens a random unwatched title (from one library, or from the first that has one). */
export function useSurprise() {
  const navigate = useNavigate();
  return async (libraryIds: string[]) => {
    for (const id of [...libraryIds].sort(() => Math.random() - 0.5)) {
      const page = unwrap(await api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId: id }, query: { sort: "random", unplayed: true, limit: 1 } } }));
      const pick = page.items?.[0];
      if (pick) {
        void navigate({ to: "/item/$itemId", params: { itemId: pick.id } });
        return true;
      }
    }
    return false;
  };
}

/** Short quality labels for a title's best file: 4K / HD and the HDR format. */
export function qualityTags(height?: number, hdr?: string): string[] {
  const tags: string[] = [];
  if (height && height >= 2000) tags.push("4K");
  else if (height && height >= 700) tags.push("HD");
  if (hdr) tags.push(/dolby|dovi|dv/i.test(hdr) ? "DV" : hdr.toUpperCase().includes("HLG") ? "HLG" : "HDR");
  return tags;
}
