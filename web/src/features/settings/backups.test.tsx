import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const NAME = "streamero-20261110T030000Z-0.9.0.tar.gz";

function overview(extra: Record<string, unknown> = {}) {
  return {
    dir: "/config/backups",
    schemaVersion: 23,
    scanRunning: false,
    running: false,
    settings: { enabled: true, keepLast: 7, includeImages: false },
    backups: [
      { name: NAME, sizeBytes: 5 * 1024 ** 2, createdAt: "2026-11-10T03:00:00Z", kind: "scheduled", serverVersion: "0.9.0",
        schemaVersion: 23, includesImages: false, scanRunning: false, restorable: true, skippedTables: [] },
      { name: "uploaded-20261109T120000Z-1.2.0.tar.gz", sizeBytes: 1024, createdAt: "2026-11-09T12:00:00Z", kind: "uploaded",
        serverVersion: "1.2.0", schemaVersion: 40, includesImages: false, scanRunning: false, restorable: false, problem: "newer_schema" },
    ],
    ...extra,
  };
}

describe("backups page", () => {
  beforeEach(() => setLanguage("en"));

  it("lists backups and warns when a scan runs during Back up now", async () => {
    const posts: string[] = [];
    mockApi(({ method, path }) => {
      if (path === "/backups" && method === "POST") { posts.push(path); return { status: 202, body: { scanRunning: true } }; }
      if (path === "/backups") return { status: 200, body: overview() };
      return { status: 200, body: [] };
    });
    renderApp("/settings/backups", { me: testUser({ isAdmin: true }) });
    const rows = await screen.findAllByTestId("backup-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Scheduled");
    expect(rows[1]).toHaveTextContent("Newer version");
    expect(screen.getAllByRole("link", { name: "Download" })[0]).toHaveAttribute("href", `/api/v1/backups/${NAME}/download`);
    // A backup from a newer schema cannot be restored.
    expect(screen.getAllByRole("button", { name: "Restore" })[1]).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "Back up now" }));
    await waitFor(() => expect(posts).toEqual(["/backups"]));
    expect(await screen.findByTestId("scan-warning")).toHaveTextContent("A library scan is running");
  });

  it("restores only after the backup name is typed", async () => {
    const restores: { path: string; body: unknown }[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "POST" && path.endsWith("/restore")) { restores.push({ path, body }); return { status: 202 }; }
      if (path === "/backups") return { status: 200, body: overview() };
      return { status: 200, body: [] };
    });
    renderApp("/settings/backups", { me: testUser({ isAdmin: true }) });
    fireEvent.click((await screen.findAllByRole("button", { name: "Restore" }))[0]!);
    const confirm = screen.getByRole("button", { name: "Restore and restart" });
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByLabelText(`Type ${NAME} to confirm`), { target: { value: "streamero" } });
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByLabelText(`Type ${NAME} to confirm`), { target: { value: NAME } });
    fireEvent.click(confirm);
    await waitFor(() => expect(restores).toEqual([{ path: `/backups/${NAME}/restore`, body: { confirm: NAME } }]));
  });

  it("saves the schedule and shows the last restore", async () => {
    const puts: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/backups/settings" && method === "PUT") { puts.push(body); return { status: 200, body }; }
      if (path === "/backups") {
        return { status: 200, body: overview({ lastRestore: { backup: NAME, state: "failed", startedAt: "2026-11-10T04:00:00Z",
          finishedAt: "2026-11-10T04:01:00Z", error: "disk full", safetyBackup: "streamero-pre-restore-x.tar.gz" } }) };
      }
      return { status: 200, body: [] };
    });
    renderApp("/settings/backups", { me: testUser({ isAdmin: true }) });
    const status = await screen.findByTestId("restore-status");
    expect(status).toHaveTextContent(`Restoring ${NAME} failed`);
    expect(status).toHaveTextContent("disk full");
    fireEvent.change(screen.getByLabelText("Keep"), { target: { value: "3" } });
    fireEvent.click(screen.getByLabelText("Include artwork"));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(puts).toEqual([{ enabled: true, keepLast: 3, includeImages: true }]));
  });

  it("is admin only", async () => {
    mockApi(() => ({ status: 200, body: [] }));
    const { router } = renderApp("/settings/backups", { me: testUser({ isAdmin: false }) });
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/account"));
  });
});
