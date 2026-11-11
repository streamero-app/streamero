import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
});

const providers = (over: object = {}) => ({
  passwordLogin: true, ldap: false,
  oidc: [{ id: "authentik", label: "Authentik", authorizeUrl: "https://media.example.com/api/v1/auth/oidc/authentik/authorize" }],
  ...over,
});

describe("Login page single sign-on", () => {
  it("shows a provider button that navigates to the server's authorize URL", async () => {
    setLanguage("en");
    mockApi(({ path }) => (path === "/auth/providers" ? { status: 200, body: providers() } : { status: 404, body: { code: "not_found" } }));
    renderApp("/login");
    const link = await screen.findByRole("link", { name: "Continue with Authentik" });
    expect(link).toHaveAttribute("href", "https://media.example.com/api/v1/auth/oidc/authentik/authorize");
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
  });

  it("hides the password form when password sign-in is off", async () => {
    setLanguage("en");
    mockApi(({ path }) => (path === "/auth/providers" ? { status: 200, body: providers({ passwordLogin: false }) } : { status: 404 }));
    renderApp("/login");
    await screen.findByRole("link", { name: "Continue with Authentik" });
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in with Quick Connect" })).toBeInTheDocument();
  });

  it("explains a failed sign-in in Croatian and cleans the address bar", async () => {
    setLanguage("hr");
    window.history.replaceState(null, "", "/login?sso_error=no_account");
    mockApi(({ path }) => (path === "/auth/providers" ? { status: 200, body: providers() } : { status: 404 }));
    renderApp("/login");
    expect(await screen.findByRole("alert")).toHaveTextContent("Ova prijava nije povezana ni s jednim Streamero računom.");
    expect(window.location.search).toBe("");
    setLanguage("en");
  });
});

describe("Sign-in settings (admin)", () => {
  it("lists providers with their redirect URI and adds one", async () => {
    setLanguage("en");
    const fetch = mockApi(({ method, path }) => {
      if (path === "/settings/auth") return { status: 200, body: { publicUrl: "https://media.example.com", disablePasswordLogin: false, recoveryActive: false } };
      if (path === "/settings/auth/oidc" && method === "GET") {
        return { status: 200, body: [{ id: "keycloak", label: "Keycloak", issuer: "https://kc.example.com/realms/home", clientId: "streamero",
          clientSecretSet: true, scopes: ["openid"], usernameClaim: "preferred_username", emailClaim: "email", nameClaim: "name",
          groupsClaim: "groups", adminGroup: "", allowedGroup: "", autoCreate: false, linkByEmail: true, trustEmail: false, enabled: true,
          redirectUri: "https://media.example.com/api/v1/auth/oidc/keycloak/callback" }] };
      }
      if (path === "/settings/auth/oidc" && method === "POST") return { status: 200, body: {} };
      if (path === "/settings/auth/oidc-test") return { status: 200, body: { issuer: "x", authorizationEndpoint: "a", tokenEndpoint: "t", signingKeys: 2, pkceS256: true } };
      if (path === "/settings/auth/ldap") return { status: 200, body: { enabled: false, url: "", startTls: true, skipVerify: false, caCert: "",
        bindDn: "", bindPasswordSet: false, baseDn: "", userFilter: "(uid={username})", usernameAttribute: "uid", emailAttribute: "mail",
        nameAttribute: "cn", groupAttribute: "memberOf", groupFilter: "", adminGroup: "", allowedGroup: "" } };
      return { status: 404, body: { code: "not_found" } };
    });
    renderApp("/settings/sso", { me: testUser({ isAdmin: true }) });
    const row = await screen.findByTestId("oidc-keycloak");
    expect(within(row).getByText("https://media.example.com/api/v1/auth/oidc/keycloak/callback")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Add provider" }));
    const form = screen.getByRole("form", { name: "Provider" });
    fireEvent.change(within(form).getByLabelText("Provider type"), { target: { value: "google" } });
    fireEvent.change(within(form).getByLabelText("Client ID"), { target: { value: "abc.apps.googleusercontent.com" } });
    fireEvent.change(within(form).getByLabelText("Client secret"), { target: { value: "g-secret" } });
    fireEvent.click(within(form).getByRole("button", { name: "Test connection" }));
    expect(await within(form).findByTestId("oidc-test-ok")).toHaveTextContent("2 signing keys");
    fireEvent.click(within(form).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(fetch.mock.calls.some(([r]) => (r as Request).method === "POST" && (r as Request).url.endsWith("/settings/auth/oidc"))).toBe(true));
    const post = fetch.mock.calls.map(([r]) => r as Request).find((r) => r.method === "POST" && r.url.endsWith("/settings/auth/oidc"))!;
    expect(await post.json()).toMatchObject({ id: "google", label: "Google", issuer: "https://accounts.google.com",
      clientId: "abc.apps.googleusercontent.com", clientSecret: "g-secret", usernameClaim: "email" });
  });
});

describe("My account linked sign-in", () => {
  it("lists linked accounts, hides the password form for SSO-only users and unlinks", async () => {
    setLanguage("en");
    vi.stubGlobal("confirm", () => true);
    const fetch = mockApi(({ method, path }) => {
      if (path === "/auth/providers") return { status: 200, body: providers() };
      if (path === "/auth/identities") return { status: 200, body: { hasPassword: false, identities: [
        { id: "0190a8b0-0000-7000-8000-0000000000f1", kind: "oidc", providerId: "authentik", providerLabel: "Authentik",
          label: "ana@example.com", createdAt: new Date().toISOString() }] } };
      if (method === "DELETE") return { status: 409, body: { code: "last_sign_in_method", message: "" } };
      return { status: 404, body: { code: "not_found" } };
    });
    renderApp("/settings/account", { me: testUser() });
    expect(await screen.findByText("ana@example.com")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Change password" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Link Authentik" })).not.toBeInTheDocument(); // already linked
    fireEvent.click(screen.getByRole("button", { name: "Unlink" }));
    expect(await screen.findByText("This is the only way this account can sign in, so it cannot be unlinked.")).toBeInTheDocument();
    expect(fetch.mock.calls.some(([r]) => (r as Request).method === "DELETE")).toBe(true);
  });
});
