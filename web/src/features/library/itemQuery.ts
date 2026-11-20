import type { QueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";

/** A title's full detail; the one definition shared by the pages and the hover prefetch. */
export function itemDetailQuery(id: string) {
  return {
    queryKey: ["item", id] as const,
    queryFn: async () => unwrap(await api.GET("/items/{itemId}", { params: { path: { itemId: id } } })),
  };
}

/**
 * Starts loading a title while the pointer rests on its card (or focus lands on it),
 * so opening it usually needs no wait. Fresh data is not fetched again.
 */
export function prefetchItem(qc: QueryClient, id: string) {
  void qc.prefetchQuery({ ...itemDetailQuery(id), staleTime: 30_000 });
}
