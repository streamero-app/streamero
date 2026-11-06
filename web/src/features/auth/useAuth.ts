import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, deviceInfo, fetchMe, unwrap, type User } from "@/api/client";

export const meQueryKey = ["auth", "me"] as const;

export function useMe() {
  return useQuery({ queryKey: meQueryKey, queryFn: fetchMe, staleTime: 60_000, retry: false });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { username: string; password: string }) =>
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
