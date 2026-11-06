import createClient from "openapi-fetch";
import type { paths, components } from "./schema.gen";

export type SystemInfo = components["schemas"]["SystemInfo"];

/** Typed API client generated from api/openapi.yaml (see scripts/gen-api.sh). */
export const api = createClient<paths>({ baseUrl: "/api/v1" });

export class ServerStartingError extends Error {
  constructor(public retryAfter: number) {
    super("server is starting");
  }
}

export async function fetchSystemInfo(): Promise<SystemInfo> {
  const { data, response } = await api.GET("/system/info");
  if (response.status === 503) {
    throw new ServerStartingError(Number(response.headers.get("Retry-After") ?? 5));
  }
  if (!data) throw new Error(`system info: HTTP ${response.status}`);
  return data;
}
