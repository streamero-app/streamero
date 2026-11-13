import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type User } from "@/api/client";
import type { components } from "@/api/schema.gen";

export type Channel = components["schemas"]["LiveTvChannel"];
export type Program = components["schemas"]["LiveTvProgram"];
export type Guide = components["schemas"]["LiveTvGuide"];
export type Category = components["schemas"]["LiveTvCategory"];
export type Recording = components["schemas"]["LiveTvRecording"];
export type RecordingStatus = components["schemas"]["LiveTvRecordingStatus"];
export type SeriesRule = components["schemas"]["LiveTvSeriesRule"];
export type SeriesRuleInput = components["schemas"]["LiveTvSeriesRuleInput"];
export type Playback = components["schemas"]["LiveTvPlayback"];

export const CATEGORIES: Category[] = ["movies", "sports", "news", "kids"];

/** Watching Live TV: the liveTv permission (administrators may do everything); never in a kids profile. */
export function canWatchLiveTv(me?: User | null): boolean {
  return !!me && !me.currentProfile?.isKid && (me.isAdmin || me.permissions.liveTv);
}

/** Recording, series rules, tuners and guides: the manageLiveTv permission. */
export function canManageLiveTv(me?: User | null): boolean {
  return !!me && (me.isAdmin || me.permissions.manageLiveTv);
}

export const liveKeys = {
  all: ["livetv"] as const,
  channels: (category?: Category, favorites?: boolean) => ["livetv", "channels", category ?? "", !!favorites] as const,
  guide: (start: string, hours: number, category?: Category, favorites?: boolean) =>
    ["livetv", "guide", start, hours, category ?? "", !!favorites] as const,
  recordings: (statuses: readonly RecordingStatus[]) => ["livetv", "recordings", statuses.join(",")] as const,
  rules: ["livetv", "series-rules"] as const,
  onNow: ["livetv", "on-now"] as const,
};

export function useChannels(category?: Category, favorites?: boolean) {
  return useQuery({
    queryKey: liveKeys.channels(category, favorites),
    queryFn: async () => unwrap(await api.GET("/livetv/channels", { params: { query: { category, favorites: favorites || undefined } } })),
    refetchInterval: 60_000,
  });
}

export function useGuide(start: string, hours: number, category?: Category, favorites?: boolean) {
  return useQuery({
    queryKey: liveKeys.guide(start, hours, category, favorites),
    queryFn: async () =>
      unwrap(await api.GET("/livetv/guide", { params: { query: { start, hours, category, favorites: favorites || undefined } } })),
    placeholderData: (prev) => prev,
  });
}

export function useRecordings(statuses: readonly RecordingStatus[]) {
  return useQuery({
    queryKey: liveKeys.recordings(statuses),
    queryFn: async () =>
      unwrap(await api.GET("/livetv/recordings", {
        params: { query: { status: [...statuses] } },
        querySerializer: { array: { style: "form", explode: false } },
      })),
    refetchInterval: 30_000,
  });
}

export function useSeriesRules() {
  return useQuery({ queryKey: liveKeys.rules, queryFn: async () => unwrap(await api.GET("/livetv/series-rules")) });
}

/** Everything that shows recordings or their badges is refreshed after a change. */
function useInvalidateLive() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: liveKeys.all });
}

export function useFavorite() {
  const done = useInvalidateLive();
  return useMutation({
    mutationFn: async ({ channelId, on }: { channelId: string; on: boolean }) => {
      const params = { path: { channelId } };
      return on ? unwrap(await api.PUT("/livetv/channels/{channelId}/favorite", { params }))
        : unwrap(await api.DELETE("/livetv/channels/{channelId}/favorite", { params }));
    },
    onSuccess: done,
  });
}

export function useRecord() {
  const done = useInvalidateLive();
  return useMutation({
    mutationFn: async (body: components["schemas"]["LiveTvRecordingCreate"]) => unwrap(await api.POST("/livetv/recordings", { body })),
    onSuccess: done,
  });
}

export function useUpdateRecording() {
  const done = useInvalidateLive();
  return useMutation({
    mutationFn: async ({ id, body }: { id: string; body: components["schemas"]["LiveTvRecordingUpdate"] }) =>
      unwrap(await api.PATCH("/livetv/recordings/{recordingId}", { params: { path: { recordingId: id } }, body })),
    onSuccess: done,
  });
}

export function useDeleteRecording() {
  const done = useInvalidateLive();
  return useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/livetv/recordings/{recordingId}", { params: { path: { recordingId: id } } })),
    onSuccess: done,
  });
}

export function useSaveRule() {
  const done = useInvalidateLive();
  return useMutation({
    mutationFn: async ({ id, body }: { id?: string; body: SeriesRuleInput }) =>
      id ? unwrap(await api.PUT("/livetv/series-rules/{ruleId}", { params: { path: { ruleId: id } }, body }))
        : unwrap(await api.POST("/livetv/series-rules", { body })),
    onSuccess: done,
  });
}

export function useDeleteRule() {
  const done = useInvalidateLive();
  return useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/livetv/series-rules/{ruleId}", { params: { path: { ruleId: id } } })),
    onSuccess: done,
  });
}
