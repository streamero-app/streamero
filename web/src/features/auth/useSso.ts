import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";

/** Sign-in methods the server offers (password, LDAP, OpenID Connect buttons). */
export function useAuthProviders() {
  return useQuery({
    queryKey: ["auth", "providers"],
    queryFn: async () => unwrap(await api.GET("/auth/providers")),
    staleTime: 60_000,
    retry: false,
  });
}

/** Error codes the server puts on a failed single sign-on redirect. */
export const SSO_ERRORS = ["invalid_state", "provider_error", "no_account", "not_allowed", "account_disabled", "identity_taken",
  "already_linked", "unknown_provider", "not_configured", "internal"] as const;

/** Reads (and removes from the address bar) an `sso_error` / `linked` query value. */
export function takeQueryParam(name: string): string | null {
  const url = new URL(window.location.href);
  const value = url.searchParams.get(name);
  if (value !== null) {
    url.searchParams.delete(name);
    window.history.replaceState(window.history.state, "", url.pathname + url.search + url.hash);
  }
  return value;
}

export function ssoErrorKey(code: string): string {
  return (SSO_ERRORS as readonly string[]).includes(code) ? `sso.error.${code}` : "sso.error.internal";
}
