import { useQuery } from "@tanstack/react-query";
import { fetchSystemInfo, ServerStartingError } from "@/api/client";

export function useSystemInfo() {
  return useQuery({
    queryKey: ["system", "info"],
    queryFn: fetchSystemInfo,
    retry: (count, err) => err instanceof ServerStartingError || count < 2,
    retryDelay: (_, err) => (err instanceof ServerStartingError ? err.retryAfter * 1000 : 1000),
    staleTime: 60_000,
  });
}
