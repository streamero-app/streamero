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

describe("LoginPage two-factor", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("asks for the code after a correct password and sends it", async () => {
    setLanguage("en");
    const fetch = mockApi(({ body }) => {
      const b = body as { totpCode?: string };
      if (!b.totpCode) return { status: 401, body: { code: "totp_required", message: "" } };
      return { status: 200, body: { sessionId: "0190a8b0-0000-7000-8000-0000000000bb", user: testUser({ totpEnabled: true }) } };
    });
    const { router } = renderApp("/login");
    fireEvent.change(await screen.findByLabelText("Username"), { target: { value: "ana" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "secret-pass" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    const codeField = await screen.findByLabelText("Code from the app");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(); // not an error, just the next step
    fireEvent.change(codeField, { target: { value: "123456" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await (fetch.mock.calls[1]![0] as Request).json()).toMatchObject({ totpCode: "123456" });
  });
});

describe("Quick Connect sign-in", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("shows the code and signs in once approved", async () => {
    setLanguage("en");
    let polls = 0;
    mockApi(({ path }) => {
      if (path === "/quickconnect/initiate") return { status: 200, body: { code: "042917", secret: "s3cret", expiresAt: new Date(Date.now() + 300_000).toISOString() } };
      if (path === "/quickconnect/poll") {
        polls++;
        return polls < 2 ? { status: 202 } : { status: 200, body: { sessionId: "0190a8b0-0000-7000-8000-0000000000cc", user: testUser() } };
      }
      return { status: 404, body: { code: "not_found", message: "" } };
    });
    const { router } = renderApp("/login");
    fireEvent.click(await screen.findByRole("button", { name: "Sign in with Quick Connect" }));
    expect(await screen.findByLabelText("Quick Connect code")).toHaveTextContent("042917");
    await waitFor(() => expect(router.state.location.pathname).toBe("/"), { timeout: 8000 });
  }, 10_000);
});

describe("InvitePage", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("creates the account from a valid invite and opens home", async () => {
    setLanguage("en");
    const fetch = mockApi(({ method }) => method === "GET"
      ? { status: 200, body: { serverName: "Kino", expiresAt: new Date(Date.now() + 86_400_000).toISOString() } }
      : { status: 200, body: { sessionId: "0190a8b0-0000-7000-8000-0000000000ee", user: testUser({ username: "teta" }) } });
    const { router } = renderApp("/invite/tok123");
    expect(await screen.findByRole("heading", { name: "Join Kino" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "teta" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "teta-pass-1" } });
    fireEvent.change(screen.getByLabelText("Confirm password"), { target: { value: "teta-pass-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Create account" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    const post = fetch.mock.calls.map(([r]) => r as Request).find((r) => r.method === "POST")!;
    expect(post.url).toMatch(/\/api\/v1\/invite\/tok123$/);
  });

  it("explains an invalid invite", async () => {
    setLanguage("en");
    mockApi(() => ({ status: 404, body: { code: "not_found", message: "" } }));
    renderApp("/invite/expired");
    expect(await screen.findByRole("heading", { name: "Invite not valid" })).toBeInTheDocument();
  });
});
