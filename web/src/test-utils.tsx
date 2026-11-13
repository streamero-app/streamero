import { render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { afterEach, expect, vi } from "vitest";
import type { SystemInfo, User } from "@/api/client";
import { routeTree } from "@/app/router";

// Pages load lazily: a test that ends mid-navigation (a redirect still fetching its
// chunk) would let the router update React after the DOM is gone. Let it settle
// first (registered after Testing Library's cleanup, so it runs before it).
const routers = new Set<{ state: { status: string } }>();
afterEach(async () => {
  for (const r of routers) await waitFor(() => expect(r.state.status).toBe("idle")).catch(() => undefined);
  routers.clear();
});

export const testInfo = (over: Partial<SystemInfo> = {}): SystemInfo => ({
  id: "0190a8b0-0000-7000-8000-000000000000", name: "Test", product: "Streamero", version: "0.1.0",
  apiVersion: "v1", setupCompleted: true, startedAt: new Date().toISOString(), ...over,
});

export const testUser = (over: Partial<User> = {}): User => ({
  id: "0190a8b0-0000-7000-8000-000000000001", username: "ana", isAdmin: false, disabled: false, totpEnabled: false,
  permissions: { allLibraries: true, download: true, videoTranscoding: true, audioTranscoding: true, remux: true, liveTv: true,
    manageLiveTv: false, syncPlay: true, deleteMedia: false, manageCollections: false, editSubtitles: false, remoteControl: true, maxSessions: 0 },
  createdAt: new Date().toISOString(), ...over,
});

type Handler = (req: { method: string; path: string; body: unknown; query: URLSearchParams }) => { status: number; body?: unknown };

/** Replaces fetch with a tiny router over API paths; returns the mock for assertions. */
export function mockApi(handler: Handler) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = input instanceof Request ? input : new Request(String(input), init);
    const url = new URL(req.url, "http://localhost");
    const text = await req.clone().text();
    const res = handler({ method: req.method, path: url.pathname.replace(/^\/api\/v1/, ""), body: text ? JSON.parse(text) : undefined,
      query: url.searchParams });
    return new Response(res.body === undefined ? null : JSON.stringify(res.body), {
      status: res.status, headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

/** Renders the real app router at `path` with the given server info and signed-in user. */
export function renderApp(path: string, { info = testInfo(), me = null as User | null } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(["system", "info"], info);
  qc.setQueryData(["auth", "me"], me);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) });
  routers.add(router);
  const utils = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...utils, router, qc };
}
