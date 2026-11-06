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
    expect((fetch.mock.calls[0]![0] as Request).url).toMatch(/\/api\/v1\/auth\/logout$/);
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
