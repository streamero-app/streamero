import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const movie = { id: "0190a8b0-0000-7000-8000-00000000bbbb", kind: "movie", title: "Big Test Movie", year: 2021, addedAt: new Date().toISOString(), hasPoster: false };
const detail = {
  item: movie, libraryId: "0190a8b0-0000-7000-8000-00000000aaaa", providerIds: { tmdb: "1" }, children: [], genres: ["Drama"], cast: [], crew: [], hasBackdrop: false, overview: "x",
  files: [{ id: "0190a8b0-0000-7000-8000-0000000000f1", path: "/media/movies/Big/a.mkv", size: 2048, container: "mkv", width: 1920, height: 1080, videoCodec: "h264", durationMs: 60000, audio: [{ codec: "aac", language: "eng", channels: 2 }], subtitles: [] }],
};
const perms = testUser().permissions;

describe("download original", () => {
  beforeEach(() => setLanguage("en"));

  it("asks for a signed link and saves the file", async () => {
    const posts: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === `/items/${movie.id}` && method === "GET") return { status: 200, body: detail };
      if (path === `/items/${movie.id}/original` && method === "POST") {
        posts.push(body);
        return { status: 200, body: { url: "/stream/orig/f1/a.mkv?exp=1&sig=x", fileName: "a.mkv", sizeBytes: 2048 } };
      }
      return { status: 200, body: [] };
    });
    const clicks: string[] = [];
    const orig = HTMLAnchorElement.prototype.click;
    HTMLAnchorElement.prototype.click = function (this: HTMLAnchorElement) { clicks.push(`${this.getAttribute("href")} ${this.download}`); };
    try {
      renderApp(`/item/${movie.id}`, { me: testUser() });
      fireEvent.click(await screen.findByTestId("download-original"));
      await waitFor(() => expect(clicks).toEqual(["/stream/orig/f1/a.mkv?exp=1&sig=x a.mkv"]));
      expect(posts).toEqual([{ fileId: "0190a8b0-0000-7000-8000-0000000000f1" }]);
      expect(screen.getByTestId("download-original")).toHaveTextContent("Download original");
    } finally {
      HTMLAnchorElement.prototype.click = orig;
    }
  });

  it("is hidden without the download permission and shown in Croatian with it", async () => {
    mockApi(({ path }) => (path === `/items/${movie.id}` ? { status: 200, body: detail } : { status: 200, body: [] }));
    const { unmount } = renderApp(`/item/${movie.id}`, { me: testUser({ permissions: { ...perms, download: false } }) });
    expect(await screen.findByTestId("media-file")).toBeInTheDocument();
    expect(screen.queryByTestId("download-original")).not.toBeInTheDocument();
    unmount();

    setLanguage("hr");
    renderApp(`/item/${movie.id}`, { me: testUser({ permissions: { ...perms, download: false }, isAdmin: true }) });
    expect(await screen.findByTestId("download-original")).toHaveTextContent("Preuzmi original");
  });

  it("lets an admin cap download transcodes and see the cache", async () => {
    const puts: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/settings/downloads" && method === "PUT") { puts.push(body); return { status: 200, body }; }
      if (path === "/admin/downloads") {
        return { status: 200, body: { settings: { maxTranscodes: 1, cacheLimitGb: 50, keepDays: 7 }, running: 0, queued: 0, cacheBytes: 3 * 1024 ** 3,
          transcodes: [{ id: "t1", itemId: movie.id, title: "Big Test Movie", quality: "720p", state: "ready", progress: 1, sizeBytes: 3 * 1024 ** 3,
            jobs: 1, users: ["ana"], createdAt: new Date().toISOString(), lastAccessAt: new Date().toISOString() }] } };
      }
      if (path === "/settings/encoding") return { status: 200, body: { hardwareAcceleration: "none", preset: "veryfast", quality: 22, audioBitrateKbps: 192, segmentSeconds: 6, maxSessions: 0, toneMapping: true } };
      if (path === "/settings/resume") return { status: 200, body: { minResumePercent: 5, maxResumePercent: 90, minResumeDurationSeconds: 300 } };
      return { status: 200, body: [] };
    });
    renderApp("/settings/transcoding", { me: testUser({ isAdmin: true }) });
    const card = await screen.findByTestId("downloads-admin");
    expect(await screen.findByTestId("download-transcode")).toHaveTextContent("Big Test Movie");
    expect(card).toHaveTextContent("In use: 3.0 GB");
    fireEvent.change(screen.getByLabelText("Conversions at once"), { target: { value: "3" } });
    fireEvent.submit(card);
    await waitFor(() => expect(puts).toEqual([{ maxTranscodes: 3, cacheLimitGb: 50, keepDays: 7 }]));
  });
});
