import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const ana = "0190a8b0-0000-7000-8000-00000000000a";
const marko = "0190a8b0-0000-7000-8000-00000000000b";
const runId = "0190a8b0-0000-7000-8000-0000000000f1";

const finished = {
  id: runId, status: "done", dryRun: true, sourceUrl: "http://old:8096", createdAt: new Date().toISOString(),
  progress: { usersDone: 2, usersTotal: 2 },
  report: {
    users: [{ sourceUserName: "Ana", userId: ana, username: "ana", items: 12, matched: 11, unmatched: 1, played: 8, resume: 2, favorites: 3, playlists: 1 }],
    skippedUsers: ["Guest"], matchedByProvider: 9, matchedByPath: 1, matchedByTitle: 1, unmatched: 1,
    unmatchedSample: [{ user: "Ana", name: "Lost Film", kind: "movie", path: "/data/movies/Lost Film.mkv" }],
    playlistsCreated: 1, playlistsUpdated: 0, playlistItemsMissing: 0, keptNewer: 2,
  },
};

describe("import from another server", () => {
  beforeEach(() => setLanguage("en"));

  it("suggests users by name, lets the admin skip one and starts a dry run", async () => {
    const started: Record<string, unknown>[] = [];
    let runs: unknown[] = [];
    mockApi(({ method, path, body: raw }) => {
      const body = raw as Record<string, unknown>;
      if (path === "/users") return { status: 200, body: [{ id: ana, username: "ana", isAdmin: true }, { id: marko, username: "marko", isAdmin: false }] };
      if (path === "/import/source") {
        return body.apiKey === "good"
          ? { status: 200, body: { serverName: "Old box", version: "10.10.7", users: [
            { id: "u1", name: "Ana", isAdmin: true, suggestedUserId: ana }, { id: "u2", name: "Marko", isAdmin: false, suggestedUserId: marko }] } }
          : { status: 502, body: { code: "import_key_rejected", message: "rejected" } };
      }
      if (path === "/import/runs" && method === "POST") {
        started.push(body);
        runs = [finished];
        return { status: 202, body: { ...finished, status: "queued", report: undefined } };
      }
      if (path === "/import/runs") return { status: 200, body: runs };
      return { status: 200, body: [] };
    });
    renderApp("/settings/import", { me: testUser({ isAdmin: true }) });

    fireEvent.change(await screen.findByLabelText("Server address"), { target: { value: "http://old:8096" } });
    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "bad" } });
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText(/rejected the API key/)).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "good" } });
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByTestId("import-source")).toHaveTextContent("Old box (10.10.7)");
    expect(screen.getByLabelText("Ana")).toHaveValue(ana);
    fireEvent.change(screen.getByLabelText("Marko"), { target: { value: "" } });

    fireEvent.click(screen.getByRole("button", { name: "Add folder mapping" }));
    fireEvent.change(screen.getByLabelText("Folder on the old server"), { target: { value: "/data" } });
    fireEvent.change(screen.getByLabelText("Same folder in Streamero"), { target: { value: "/media" } });
    fireEvent.click(screen.getByRole("button", { name: "Dry run" }));

    await waitFor(() => expect(started).toHaveLength(1));
    expect(started[0]).toEqual({
      url: "http://old:8096", apiKey: "good", dryRun: true, playlists: true,
      users: [{ sourceUserId: "u1", sourceUserName: "Ana", userId: ana }, { sourceUserId: "u2", sourceUserName: "Marko" }],
      pathMappings: [{ from: "/data", to: "/media" }],
    });

    const report = await screen.findByTestId("import-report");
    expect(report).toHaveTextContent("nothing was saved");
    expect(report).toHaveTextContent("Matched 9 by id, 1 by file path and 1 by title. Not found: 1.");
    expect(within(report).getByTestId("import-user-row")).toHaveTextContent("Ana → ana");
    expect(report).toHaveTextContent("Skipped: Guest");
    expect(report).toHaveTextContent("newer Streamero progress was kept: 2");
    expect(within(report).getByTestId("import-unmatched")).toHaveTextContent("Lost Film");
  });

  it("shows why the last import failed", async () => {
    mockApi(({ path }) => (path === "/import/runs"
      ? { status: 200, body: [{ ...finished, dryRun: false, status: "failed", error: "the server is unreachable", report: undefined }] }
      : { status: 200, body: [] }));
    renderApp("/settings/import", { me: testUser({ isAdmin: true }) });
    const run = await screen.findByTestId("import-run");
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(within(run).getByRole("alert")).toHaveTextContent("The import stopped: the server is unreachable");
  });

  it("is not available to other users", async () => {
    mockApi(() => ({ status: 200, body: [] }));
    const { router } = renderApp("/settings/import", { me: testUser({ isAdmin: false }) });
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/account"));
    expect(screen.queryByText("Import from another media server")).not.toBeInTheDocument();
  });
});
