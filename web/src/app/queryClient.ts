import { MutationCache, QueryCache, QueryClient, type Query } from "@tanstack/react-query";
import { ApiError } from "@/api/client";

/** Query keys that hold the session itself or public server facts. */
const PUBLIC_KEYS = new Set(["auth", "system", "invite"]);

/**
 * Whether an error means the session is gone (expired, signed out elsewhere, revoked).
 * Wrong passwords, codes and PINs are 401 too, but with their own codes.
 */
export function isSessionLost(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401 && (error.code === "unauthenticated" || error.code === "http_401");
}

/**
 * The session ended while the app was open: forget every private row and mark the
 * visitor signed out, so the app shell sends them to sign-in (and back afterwards).
 */
export function dropSession(qc: QueryClient) {
  qc.removeQueries({ predicate: (q: Query) => !PUBLIC_KEYS.has(String(q.queryKey[0])) });
  qc.setQueryData(["auth", "me"], null);
}

/** Client errors never get better by asking again; network and server errors may. */
export function shouldRetry(count: number, error: unknown): boolean {
  if (error instanceof ApiError && error.status >= 400 && error.status < 500 && error.status !== 408 && error.status !== 429) return false;
  return count < 2;
}

export function createQueryClient(): QueryClient {
  const onError = (error: unknown) => {
    if (isSessionLost(error) && qc.getQueryData(["auth", "me"])) dropSession(qc);
  };
  const qc: QueryClient = new QueryClient({
    queryCache: new QueryCache({ onError }),
    mutationCache: new MutationCache({ onError }),
    defaultOptions: {
      queries: {
        // Rows stay fresh for a short while: moving between pages does not refetch everything.
        staleTime: 30_000,
        gcTime: 10 * 60_000,
        retry: shouldRetry,
      },
      mutations: { retry: false },
    },
  });
  return qc;
}
