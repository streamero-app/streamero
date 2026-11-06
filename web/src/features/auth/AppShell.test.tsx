import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testInfo, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

describe("signed-in area", () => {
  beforeEach(() => setLanguage("en"));

  it("redirects anonymous visitors to sign-in", async () => {
    const { router } = renderApp("/settings/devices");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
  });

  it("shows administration tabs only to admins", async () => {
    mockApi(() => ({ status: 200, body: [] }));
    renderApp("/settings/account", { me: testUser({ isAdmin: true }) });
    expect(await screen.findByRole("link", { name: "Users" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Server" })).toBeInTheDocument();
  });

  it("keeps non-admins out of admin pages", async () => {
    mockApi(() => ({ status: 200, body: [] }));
    const { router } = renderApp("/settings/users", { me: testUser() });
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/account"));
    expect(screen.queryByRole("link", { name: "Users" })).not.toBeInTheDocument();
  });

  it("signs out and returns to sign-in", async () => {
    const fetch = mockApi(() => ({ status: 204 }));
    const { router } = renderApp("/", { me: testUser() });
    fireEvent.click(await screen.findByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(fetch.mock.calls.some(([r]) => /\/api\/v1\/auth\/logout$/.test((r as Request).url))).toBe(true);
  });

  it("setup checks that both passwords match before calling the server", async () => {
    const fetch = mockApi(() => ({ status: 500 }));
    renderApp("/setup", { info: testInfo({ setupCompleted: false }) });
    fireEvent.change(await screen.findByLabelText("Setup code"), { target: { value: "ABCD-EFGH" } });
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "password-1" } });
    fireEvent.change(screen.getByLabelText("Confirm password"), { target: { value: "password-2" } });
    fireEvent.click(screen.getByRole("button", { name: "Finish setup" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("The passwords do not match.");
    expect(fetch).not.toHaveBeenCalled();
  });
});

describe("API keys", () => {
  beforeEach(() => setLanguage("en"));

  it("shows a new key once and lists keys without secrets", async () => {
    const key = { id: "0190a8b0-0000-7000-8000-0000000000k1", name: "Jellyseerr", prefix: "sk_abcdef", createdBy: "admin", createdAt: new Date().toISOString() };
    let created = false;
    mockApi(({ method, path }) => {
      if (method === "POST" && path === "/apikeys") {
        created = true;
        return { status: 201, body: { key: "sk_abcdef_SECRET", apiKey: key } };
      }
      if (path === "/apikeys") return { status: 200, body: created ? [key] : [] };
      return { status: 200, body: [] };
    });
    renderApp("/settings/apikeys", { me: testUser({ isAdmin: true }) });
    expect(await screen.findByText("No API keys yet.")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Jellyseerr" } });
    fireEvent.click(screen.getByRole("button", { name: "Create API key" }));
    expect(await screen.findByText("sk_abcdef_SECRET")).toBeInTheDocument();
    expect(await screen.findByTestId("api-key-row")).toHaveTextContent("Jellyseerr");
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText("sk_abcdef_SECRET")).not.toBeInTheDocument();
  });
});

describe("user permissions", () => {
  beforeEach(() => setLanguage("en"));

  it("saves edited permissions for a user", async () => {
    const leo = testUser({ id: "0190a8b0-0000-7000-8000-0000000000dd", username: "leo" });
    const fetch = mockApi(({ method, path }) => {
      if (method === "PATCH") return { status: 200, body: leo };
      if (path === "/users") return { status: 200, body: [leo] };
      return { status: 200, body: [] };
    });
    renderApp("/settings/users", { me: testUser({ isAdmin: true, username: "admin" }) });
    fireEvent.click(await screen.findByRole("button", { name: "Permissions" }));
    fireEvent.click(screen.getByLabelText("Download"));
    fireEvent.change(screen.getByLabelText("Max. signed-in devices"), { target: { value: "3" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(fetch.mock.calls.some(([r]) => (r as Request).method === "PATCH")).toBe(true));
    const patch = fetch.mock.calls.find(([r]) => (r as Request).method === "PATCH")![0] as Request;
    expect(await patch.json()).toMatchObject({ permissions: { download: false, maxSessions: 3 } });
  });
});

describe("profiles", () => {
  beforeEach(() => setLanguage("en"));
  const kid = { id: "0190a8b0-0000-7000-8000-0000000000f1", name: "Lea", color: "#3dd68c", hasPin: false, isKid: true };
  const mama = { id: "0190a8b0-0000-7000-8000-0000000000f2", name: "Mama", color: "#e5484d", hasPin: true, isKid: false };

  it("asks for the PIN of a protected profile and switches", async () => {
    const fetch = mockApi(({ method, path }) => {
      if (path === "/profiles" && method === "GET") return { status: 200, body: [kid, mama] };
      if (path.endsWith("/select")) return { status: 204 };
      return { status: 200, body: [] };
    });
    renderApp("/", { me: testUser() });
    fireEvent.click(await screen.findByTestId("profile-switcher"));
    fireEvent.click(screen.getByRole("menuitem", { name: /Mama/ }));
    fireEvent.change(screen.getByLabelText("PIN"), { target: { value: "1234" } });
    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    await waitFor(() => expect(fetch.mock.calls.some(([r]) => (r as Request).url.endsWith(`/profiles/${mama.id}/select`))).toBe(true));
    const req = fetch.mock.calls.map(([r]) => r as Request).find((r) => r.url.endsWith("/select"))!;
    expect(await req.json()).toEqual({ pin: "1234" });
  });

  it("hides settings in a kids profile and keeps it out of the settings pages", async () => {
    mockApi(({ path }) => (path === "/profiles" ? { status: 200, body: [kid] } : { status: 200, body: [] }));
    const { router } = renderApp("/settings/account", { me: testUser({ currentProfile: kid }) });
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(screen.queryByRole("link", { name: "Settings" })).not.toBeInTheDocument();
  });
});
