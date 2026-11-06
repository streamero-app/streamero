import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testInfo, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

describe("LoginPage", () => {
  it("renders in English and switches to Croatian", async () => {
    setLanguage("en");
    renderApp("/login");
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Language"), { target: { value: "hr" } });
    expect(screen.getByRole("heading", { name: "Prijava" })).toBeInTheDocument();
    setLanguage("en");
  });

  it("shows server version", async () => {
    setLanguage("en");
    renderApp("/login");
    expect(await screen.findByText(/Version 0\.1\.0/)).toBeInTheDocument();
  });

  it("sends the setup wizard when the server is not set up", async () => {
    setLanguage("en");
    const { router } = renderApp("/login", { info: testInfo({ setupCompleted: false }) });
    await waitFor(() => expect(router.state.location.pathname).toBe("/setup"));
    expect(await screen.findByRole("heading", { name: "Set up Streamero" })).toBeInTheDocument();
  });

  it("signs in with a cookie session and opens home", async () => {
    setLanguage("en");
    const fetch = mockApi(({ method, path }) =>
      method === "POST" && path === "/auth/login"
        ? { status: 200, body: { sessionId: "0190a8b0-0000-7000-8000-0000000000aa", user: testUser() } }
        : { status: 404, body: { code: "not_found", message: "" } },
    );
    const { router } = renderApp("/login");
    fireEvent.change(await screen.findByLabelText("Username"), { target: { value: "ana" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "secret-pass" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await screen.findByRole("heading", { name: "Welcome, ana" })).toBeInTheDocument();
    const req = fetch.mock.calls[0]![0] as Request;
    expect(await req.json()).toMatchObject({ username: "ana", password: "secret-pass", useCookie: true });
  });

  it("shows a translated error for wrong credentials", async () => {
    setLanguage("hr");
    mockApi(() => ({ status: 401, body: { code: "invalid_credentials", message: "invalid" } }));
    renderApp("/login");
    fireEvent.change(await screen.findByLabelText("Korisničko ime"), { target: { value: "ana" } });
    fireEvent.change(screen.getByLabelText("Lozinka"), { target: { value: "x" } });
    fireEvent.click(screen.getByRole("button", { name: "Prijavi se" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Pogrešno korisničko ime ili lozinka.");
    setLanguage("en");
  });
});
