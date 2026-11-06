import createClient from "openapi-fetch";
import type { paths, components } from "./schema.gen";

export type SystemInfo = components["schemas"]["SystemInfo"];
export type User = components["schemas"]["User"];
export type Session = components["schemas"]["Session"];
export type ServerSettings = components["schemas"]["ServerSettings"];
export type AuthResult = components["schemas"]["AuthResult"];
type ErrorBody = components["schemas"]["Error"];

/** Typed API client generated from api/openapi.yaml (see scripts/gen-api.sh). */
export const api = createClient<paths>({
  // Absolute same-origin URL: identical in browsers, and valid for Request() in tests.
  baseUrl: new URL("/api/v1", globalThis.location?.origin ?? "http://localhost").href,
  credentials: "same-origin",
  // Resolve fetch per call (not at import) so tests and polyfills can replace it.
  fetch: (req: Request) => globalThis.fetch(req),
});

export class ServerStartingError extends Error {
  constructor(public retryAfter: number) {
    super("server is starting");
  }
}

/** An error response from the API; `code` is stable and used for translations. */
export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

/** Returns data or throws ApiError for non-2xx responses. */
export function unwrap<T>(res: { data?: T; error?: unknown; response: Response }): T {
  if (res.response.ok) return res.data as T;
  const body = res.error as Partial<ErrorBody> | undefined;
  throw new ApiError(res.response.status, body?.code ?? "http_" + res.response.status, body?.message ?? res.response.statusText);
}

export async function fetchSystemInfo(): Promise<SystemInfo> {
  const { data, response } = await api.GET("/system/info");
  if (response.status === 503) {
    throw new ServerStartingError(Number(response.headers.get("Retry-After") ?? 5));
  }
  if (!data) throw new Error(`system info: HTTP ${response.status}`);
  return data;
}

/** The signed-in user, or null when there is no valid session. */
export async function fetchMe(): Promise<User | null> {
  const res = await api.GET("/auth/me");
  if (res.response.status === 401) return null;
  return unwrap(res);
}

/** Describes this browser for the device list, e.g. "Firefox on Linux". */
export function deviceInfo() {
  const ua = typeof navigator !== "undefined" ? navigator.userAgent : "";
  const browser = /Edg\//.test(ua) ? "Edge" : /Firefox\//.test(ua) ? "Firefox" : /Chrome\//.test(ua) ? "Chrome"
    : /Safari\//.test(ua) ? "Safari" : "Browser";
  const os = /iPhone|iPad/.test(ua) ? "iOS" : /Android/.test(ua) ? "Android" : /Mac OS X/.test(ua) ? "macOS"
    : /Windows/.test(ua) ? "Windows" : /Linux/.test(ua) ? "Linux" : "";
  return { name: os ? `${browser} · ${os}` : browser, client: "Streamero Web" };
}
