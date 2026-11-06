import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { vi } from "vitest";
import type { SystemInfo, User } from "@/api/client";
import { routeTree } from "@/app/router";

export const testInfo = (over: Partial<SystemInfo> = {}): SystemInfo => ({
  id: "0190a8b0-0000-7000-8000-000000000000", name: "Test", product: "Streamero", version: "0.1.0",
  apiVersion: "v1", setupCompleted: true, startedAt: new Date().toISOString(), ...over,
});

export const testUser = (over: Partial<User> = {}): User => ({
  id: "0190a8b0-0000-7000-8000-000000000001", username: "ana", isAdmin: false, disabled: false,
  createdAt: new Date().toISOString(), ...over,
});

type Handler = (req: { method: string; path: string; body: unknown }) => { status: number; body?: unknown };

/** Replaces fetch with a tiny router over API paths; returns the mock for assertions. */
export function mockApi(handler: Handler) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = input instanceof Request ? input : new Request(String(input), init);
    const url = new URL(req.url, "http://localhost");
    const text = await req.clone().text();
    const res = handler({ method: req.method, path: url.pathname.replace(/^\/api\/v1/, ""), body: text ? JSON.parse(text) : undefined });
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
  const utils = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...utils, router, qc };
}
