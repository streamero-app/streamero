import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { ApiError } from "@/api/client";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { isSessionLost, shouldRetry } from "./queryClient";

afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
});
beforeEach(() => setLanguage("en"));

describe("session handling", () => {
  it("tells a lost session from a wrong password", () => {
    expect(isSessionLost(new ApiError(401, "unauthenticated", ""))).toBe(true);
    expect(isSessionLost(new ApiError(401, "invalid_credentials", ""))).toBe(false);
    expect(isSessionLost(new ApiError(403, "forbidden", ""))).toBe(false);
  });

  it("does not retry client errors", () => {
    expect(shouldRetry(0, new ApiError(404, "not_found", ""))).toBe(false);
    expect(shouldRetry(0, new ApiError(503, "unavailable", ""))).toBe(true);
    expect(shouldRetry(2, new Error("network"))).toBe(false);
  });

  it("sends a visitor whose session expired to sign-in, then back to the page", async () => {
    let signedIn = true;
    mockApi(({ method, path }) => {
      if (path === "/auth/login" && method === "POST") {
        signedIn = true;
        return { status: 200, body: { user: testUser(), token: "" } };
      }
      if (!signedIn) return { status: 401, body: { code: "unauthenticated", message: "" } };
      if (path === "/auth/providers") return { status: 200, body: { passwordLogin: true, oidc: [] } };
      return { status: 200, body: [] };
    });
    signedIn = false; // the cookie expired while the tab was open
    const { router, qc } = renderApp("/my-list", { me: testUser() });
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(qc.getQueryData(["auth", "me"])).toBeNull();

    fireEvent.change(await screen.findByLabelText("Username"), { target: { value: "ana" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "secret-pass" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/my-list"));
  });

  it("a wrong password does not count as a lost session", async () => {
    mockApi(({ path }) => (path === "/auth/login"
      ? { status: 401, body: { code: "invalid_credentials", message: "" } }
      : { status: 200, body: { passwordLogin: true, oidc: [] } }));
    const { router } = renderApp("/login");
    fireEvent.change(await screen.findByLabelText("Username"), { target: { value: "ana" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "nope" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
  });

  it("starts a new sign-in with an empty cache", async () => {
    mockApi(({ path }) => (path === "/auth/login"
      ? { status: 200, body: { user: testUser({ username: "leo" }), token: "" } }
      : { status: 200, body: path === "/auth/providers" ? { passwordLogin: true, oidc: [] } : [] }));
    const { qc } = renderApp("/login");
    qc.setQueryData(["favorites"], [{ id: "left-over" }]);
    fireEvent.change(await screen.findByLabelText("Username"), { target: { value: "leo" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "leo-pass-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(qc.getQueryData(["favorites"])).not.toEqual([{ id: "left-over" }]));
  });
});

describe("page errors", () => {
  it("shows a recoverable error screen instead of a blank page", async () => {
    mockApi(({ path }) => {
      if (path === "/home/latest") return { status: 200, body: "not-a-list" };
      return { status: 200, body: [] };
    });
    // A row that crashes while rendering: the home page breaks, the app does not.
    const spy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    renderApp("/", { me: testUser() });
    expect(await screen.findByTestId("route-error")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
    // The header (outside the failed page) still works.
    expect(screen.getByTestId("account-menu")).toBeInTheDocument();
    spy.mockRestore();
  });
});
