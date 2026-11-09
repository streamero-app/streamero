import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const dashboard = {
  server: { name: "Kino", version: "0.6.0", os: "linux/amd64", startedAt: new Date().toISOString() },
  storage: [{ kind: "media", path: "/media", totalBytes: 1000 * 1024 ** 3, freeBytes: 400 * 1024 ** 3 }],
  nowPlaying: [{ key: "sess-1", user: "ana", device: "iPhone", itemId: "0190a8b0-0000-7000-8000-00000000bbbb", title: "Dune", method: "transcode",
    reason: "video hevc", positionMs: 60_000, durationMs: 120_000, updatedAt: new Date().toISOString() }],
  activeTranscodes: 1,
  activity: [{ at: new Date().toISOString(), user: "ana", action: "login.succeeded", detail: { ip: "10.0.0.5" } }],
};

describe("admin pages", () => {
  beforeEach(() => setLanguage("en"));

  it("shows who is watching, storage and activity", async () => {
    mockApi(({ path }) => (path === "/dashboard" ? { status: 200, body: dashboard } : { status: 200, body: [] }));
    renderApp("/settings/dashboard", { me: testUser({ isAdmin: true }) });
    const row = await screen.findByTestId("now-playing");
    expect(row).toHaveTextContent("Dune");
    expect(row).toHaveTextContent("Transcoding");
    expect(row).toHaveTextContent("iPhone");
    expect(screen.getByText("400 GB free of 1000 GB")).toBeInTheDocument();
    expect(screen.getByTestId("activity")).toHaveTextContent("Signed in");
  });

  it("runs a task now", async () => {
    const fetch = mockApi(({ path }) => (path === "/tasks"
      ? { status: 200, body: [{ key: "scan_all", running: false, interval: "6h" }, { key: "refresh_metadata", running: false }, { key: "cleanup_images", running: true }] }
      : { status: 202 }));
    renderApp("/settings/tasks", { me: testUser({ isAdmin: true }) });
    const rows = await screen.findAllByTestId("task-row");
    expect(rows).toHaveLength(3);
    fireEvent.click(screen.getAllByRole("button", { name: "Run now" })[1]!);
    await waitFor(() => expect(fetch.mock.calls.some(([r]) => (r as Request).url.endsWith("/tasks/refresh_metadata/run"))).toBe(true));
    expect(screen.getAllByRole("button", { name: "Run now" })[2]).toBeDisabled();
  });

  it("adds a Discord target and saves it", async () => {
    const bodies: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/settings/notifications" && method === "PUT") {
        bodies.push(body);
        return { status: 200, body };
      }
      if (path === "/settings/notifications") return { status: 200, body: { targets: [] } };
      return { status: 200, body: [] };
    });
    renderApp("/settings/notifications", { me: testUser({ isAdmin: true }) });
    await screen.findByText("No targets yet.");
    fireEvent.click(screen.getByRole("button", { name: "Add target" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Filmovi" } });
    fireEvent.change(screen.getByLabelText("URL"), { target: { value: "https://discord.com/api/webhooks/1/abc" } });
    fireEvent.click(screen.getByLabelText("Failed sign-ins"));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const saved = (bodies[0] as { targets: { name: string; kind: string; events: string[] }[] }).targets[0]!;
    expect(saved.name).toBe("Filmovi");
    expect(saved.kind).toBe("discord");
    expect(saved.events).toEqual(["item.added", "login.failed"]);
    expect(await screen.findByText("Saved.")).toBeInTheDocument();
  });

  it("saves transcoding with hardware acceleration", async () => {
    const puts: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/settings/encoding" && method === "PUT") { puts.push(body); return { status: 200, body }; }
      if (path === "/settings/encoding") return { status: 200, body: { hardwareAcceleration: "none", preset: "veryfast", quality: 22, audioBitrateKbps: 192, segmentSeconds: 6, maxSessions: 0, toneMapping: true } };
      if (path === "/settings/resume") return { status: 200, body: { minResumePercent: 5, maxResumePercent: 90, minResumeDurationSeconds: 300 } };
      return { status: 200, body: [] };
    });
    renderApp("/settings/transcoding", { me: testUser({ isAdmin: true }) });
    fireEvent.change(await screen.findByLabelText("Hardware acceleration"), { target: { value: "vaapi" } });
    expect(screen.getByLabelText("VAAPI device")).toHaveValue("/dev/dri/renderD128");
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]!);
    await waitFor(() => expect(puts[0]).toMatchObject({ hardwareAcceleration: "vaapi", quality: 22 }));
  });

  it("shows and filters server logs", async () => {
    const queries: string[] = [];
    mockApi(({ path, query }) => {
      if (path === "/admin/logs") {
        queries.push(query.toString());
        return { status: 200, body: [{ time: new Date().toISOString(), level: "error", message: "ffmpeg failed", source: "serve", attrs: "code=1" }] };
      }
      return { status: 200, body: [] };
    });
    renderApp("/settings/logs", { me: testUser({ isAdmin: true }) });
    expect(await screen.findByTestId("log-row")).toHaveTextContent("ffmpeg failed");
    fireEvent.change(screen.getByLabelText("Level"), { target: { value: "error" } });
    await waitFor(() => expect(queries.some((q) => q.includes("level=error"))).toBe(true));
  });

  it("sends a message to a playing session", async () => {
    const posts: { url: string; body: unknown }[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "POST") { posts.push({ url: path, body }); return { status: 204 }; }
      return path === "/dashboard" ? { status: 200, body: dashboard } : { status: 200, body: [] };
    });
    renderApp("/settings/dashboard", { me: testUser({ isAdmin: true }) });
    fireEvent.click(await screen.findByRole("button", { name: "Message" }));
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "Dinner is ready" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(posts).toContainEqual({ url: "/sessions/sess-1/command", body: { kind: "message", text: "Dinner is ready" } }));
  });
});
