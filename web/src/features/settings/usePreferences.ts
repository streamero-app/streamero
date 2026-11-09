import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";

export type Prefs = components["schemas"]["PlaybackPreferences"];

/** Home page rows in their default order. */
export const HOME_SECTIONS = ["continue", "nextUp", "latest", "favorites", "collections", "top", "genres", "libraries"] as const;
export type HomeSection = (typeof HOME_SECTIONS)[number];

/** The current profile's preferences, editable as a draft and saved as a whole. */
export function usePreferences() {
  const qc = useQueryClient();
  const current = useQuery({ queryKey: ["preferences"], queryFn: async () => unwrap(await api.GET("/me/preferences")) });
  const [draft, setDraft] = useState<Prefs>({ subtitleMode: "default", autoplayNext: true });
  useEffect(() => { if (current.data) setDraft(current.data); }, [current.data]);
  const save = useMutation({
    mutationFn: async (p: Prefs) => unwrap(await api.PUT("/me/preferences", { body: p })),
    onSuccess: (d) => qc.setQueryData(["preferences"], d),
  });
  const set = <K extends keyof Prefs>(k: K, v: Prefs[K]) => setDraft((d) => ({ ...d, [k]: v }));
  return { draft, set, setDraft, save, current, submit: () => { save.reset(); save.mutate(draft); } };
}
