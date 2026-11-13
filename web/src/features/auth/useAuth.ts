import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api, deviceInfo, fetchMe, unwrap, type User } from "@/api/client";

/**
 * Whether the signed-in account may use administrator actions right now: an
 * administrator who is not on a kid profile (the server refuses admin calls there).
 */
export function canAdminister(me: User | null | undefined): boolean {
  return !!me?.isAdmin && !me.currentProfile?.isKid;
}

/** Query keys that belong to the account or the server, not to the profile in use. */
const ACCOUNT_KEYS = new Set(["auth", "system", "profiles"]);

/**
 * After switching profile: drops every cached row of the previous profile (home rows,
 * progress, preferences…) so none of it shows, then reloads the account.
 */
export async function resetProfileData(qc: QueryClient): Promise<void> {
  qc.removeQueries({ predicate: (q) => !ACCOUNT_KEYS.has(String(q.queryKey[0])) });
  await qc.invalidateQueries({ queryKey: meQueryKey });
}

export const meQueryKey = ["auth", "me"] as const;

export function useMe() {
  return useQuery({ queryKey: meQueryKey, queryFn: fetchMe, staleTime: 60_000, retry: false });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { username: string; password: string; totpCode?: string }) =>
      unwrap(await api.POST("/auth/login", { body: { ...v, useCookie: true, device: deviceInfo() } })),
    onSuccess: (res) => {
      qc.clear();
      qc.setQueryData<User | null>(meQueryKey, res.user);
    },
  });
}

export function useSetup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { setupCode: string; serverName: string; username: string; password: string }) =>
      unwrap(await api.POST("/setup", { body: { ...v, useCookie: true, device: deviceInfo() } })),
    onSuccess: async (res) => {
      qc.setQueryData<User | null>(meQueryKey, res.user);
      await qc.invalidateQueries({ queryKey: ["system", "info"] });
    },
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/logout")),
    onSettled: () => {
      qc.clear();
      qc.setQueryData<User | null>(meQueryKey, null);
    },
  });
}
