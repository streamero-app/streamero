import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { detectDeviceProfile } from "./deviceProfile";

afterEach(() => vi.unstubAllGlobals());

describe("device profile", () => {
  it("maps supported MIME types to codec names", () => {
    const p = detectDeviceProfile((m) => /avc1|mp4a|opus|vp9/.test(m), false);
    expect(p.videoCodecs).toEqual(["h264", "vp9"]);
    expect(p.audioCodecs).toEqual(["aac", "opus"]);
    expect(p.containers).toContain("webm");
  });
});

describe("player", () => {
  beforeEach(() => setLanguage("en"));
  const item = { id: "0190a8b0-0000-7000-8000-00000000cccc", kind: "movie", title: "Film", addedAt: new Date().toISOString(), hasPoster: false };

  it("direct-plays the signed URL and saves progress on pause", async () => {
    const bodies: Record<string, unknown> = {};
    mockApi(({ method, path, body }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        bodies.playback = body;
        return { status: 200, body: { fileId: "f", method: "direct", url: "/stream/file/f/video.mp4?exp=1&sig=x", startMs: 0,
          durationMs: 100_000, audio: [{ codec: "aac" }], subtitles: [{ index: 0, url: "/stream/sub/f/0.vtt?exp=1&sig=x", external: true, language: "hr" }] } };
      }
      if (method === "PUT") {
        bodies.progress = body;
        return { status: 204 };
      }
      return { status: 200, body: { item, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false } };
    });
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    renderApp(`/watch/${item.id}`, { me: testUser() });
    const video = (await screen.findByTestId("video")) as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute("src") ?? video.src).toContain("/stream/file/f/video.mp4"));
    expect(screen.getByTestId("play-method")).toHaveTextContent("Direct play");
    expect((bodies.playback as { device: { containers: string[] } }).device.containers).toContain("mp4");
    expect(screen.getByLabelText("Subtitles")).toBeInTheDocument();
    Object.defineProperty(video, "currentTime", { value: 42, configurable: true });
    fireEvent.pause(video);
    await waitFor(() => expect(bodies.progress).toMatchObject({ positionMs: 42_000, durationMs: 100_000 }));
  });

  it("adds the stream offset for HLS that starts mid-way", async () => {
    let progress: unknown;
    mockApi(({ method, path, body }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        return { status: 200, body: { fileId: "f", method: "transcode", url: "/stream/hls/s/index.m3u8?exp=1&sig=x", startMs: 600_000,
          audio: [], subtitles: [] } };
      }
      if (method === "PUT") {
        progress = body;
        return { status: 204 };
      }
      return { status: 200, body: { item, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false } };
    });
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    vi.spyOn(HTMLMediaElement.prototype, "canPlayType").mockImplementation((t: string) => (t.includes("mpegurl") ? "maybe" : ""));
    renderApp(`/watch/${item.id}`, { me: testUser() });
    const video = (await screen.findByTestId("video")) as HTMLVideoElement;
    await waitFor(() => expect(screen.getByTestId("play-method")).toHaveTextContent("Transcoding"));
    Object.defineProperty(video, "currentTime", { value: 5, configurable: true });
    fireEvent.pause(video);
    await waitFor(() => expect(progress).toMatchObject({ positionMs: 605_000 }));
  });

  it("restarts the stream with a bitmap subtitle burned in and keeps it on seek", async () => {
    const requests: { subtitleIndex?: number; startMs?: number }[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        const b = body as { subtitleIndex?: number; startMs?: number };
        requests.push(b);
        const burned = b.subtitleIndex !== undefined && b.subtitleIndex >= 0 ? { burnedSubtitle: b.subtitleIndex, method: "transcode",
          url: "/stream/hls/s/index.m3u8?exp=1&sig=x", reason: "subtitle burn-in" } : { method: "direct", url: "/stream/file/f/video.mkv?exp=1&sig=x" };
        return { status: 200, body: { fileId: "f", startMs: b.startMs ?? 0, durationMs: 100_000, audio: [{ codec: "aac" }],
          subtitles: [{ index: 0, url: "/a.vtt", external: true, language: "en" }],
          bitmapSubtitles: [{ index: 1, codec: "hdmv_pgs_subtitle", language: "hr" }], ...burned } };
      }
      if (method === "PUT") return { status: 204 };
      return { status: 200, body: { item, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false } };
    });
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    vi.spyOn(HTMLMediaElement.prototype, "canPlayType").mockImplementation((t: string) => (t.includes("mpegurl") ? "maybe" : ""));
    renderApp(`/watch/${item.id}`, { me: testUser() });
    await screen.findByTestId("video");
    const select = (await screen.findByLabelText("Subtitles")) as HTMLSelectElement;
    expect(screen.getByRole("option", { name: "HR · PGS · burned in", hidden: true })).toBeInTheDocument();
    fireEvent.change(select, { target: { value: "b:1" } });
    await waitFor(() => expect(screen.getByTestId("play-method")).toHaveTextContent("Transcoding"));
    expect(requests.at(-1)).toMatchObject({ subtitleIndex: 1 });
    expect((screen.getByLabelText("Subtitles") as HTMLSelectElement).value).toBe("b:1");
    // Back to a text track: the picture is clean again.
    fireEvent.change(screen.getByLabelText("Subtitles"), { target: { value: "t:0" } });
    await waitFor(() => expect(requests.at(-1)).toMatchObject({ subtitleIndex: -1 }));
    await waitFor(() => expect((screen.getByLabelText("Subtitles") as HTMLSelectElement).value).toBe("t:0"));
  });

  it("selects the preferred subtitle and offers the next episode at the end", async () => {
    const ep = { ...item, id: "0190a8b0-0000-7000-8000-00000000eeee", kind: "episode", title: "Pilot", seasonNumber: 1, episodeNumber: 1 };
    const nextEp = { ...ep, id: "0190a8b0-0000-7000-8000-00000000ffff", title: "Second", episodeNumber: 2 };
    mockApi(({ method, path }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        return { status: 200, body: { fileId: "f", method: "direct", url: "/stream/file/f/video.mp4?exp=1&sig=x", startMs: 0, durationMs: 100_000,
          audio: [{ codec: "aac" }], subtitleDefault: 1, subtitles: [
            { index: 0, url: "/a.vtt", external: true, language: "en" }, { index: 1, url: "/b.vtt", external: true, language: "hr" }] } };
      }
      if (path === `/items/${ep.id}/next`) return { status: 200, body: nextEp };
      if (path === "/me/preferences") return { status: 200, body: { subtitleMode: "always", subtitleLanguage: "hr", autoplayNext: false } };
      if (method === "PUT") return { status: 204 };
      return { status: 200, body: { item: ep, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false } };
    });
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    const { router } = renderApp(`/watch/${ep.id}`, { me: testUser() });
    const video = (await screen.findByTestId("video")) as HTMLVideoElement;
    await waitFor(() => expect((screen.getByLabelText("Subtitles") as HTMLSelectElement).value).toBe("t:1"));
    fireEvent.ended(video);
    const card = await screen.findByTestId("up-next");
    expect(card).toHaveTextContent("Second");
    expect(card).toHaveTextContent("Next episode"); // autoplay off: no countdown
    fireEvent.click(screen.getByRole("button", { name: /Play now/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/watch/${nextEp.id}`));
  });

  it("offers to skip the intro and shows chapter and trickplay previews on the seek bar", async () => {
    const ep = { ...item, id: "0190a8b0-0000-7000-8000-00000000abab", kind: "episode", title: "Pilot", seasonNumber: 1, episodeNumber: 1 };
    mockApi(({ method, path }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        return { status: 200, body: { fileId: "f", method: "direct", url: "/stream/file/f/video.mp4?exp=1&sig=x", startMs: 0, durationMs: 1_000_000,
          audio: [{ codec: "aac" }], subtitles: [],
          chapters: [{ startMs: 0, title: "Cold open" }, { startMs: 60_000, title: "Opening" }],
          segments: [{ kind: "intro", startMs: 60_000, endMs: 120_000 }],
          trickplay: { intervalMs: 10_000, width: 320, height: 180, columns: 10, rows: 10, count: 100, sheetUrls: ["/api/v1/trickplay/f/0"] } } };
      }
      if (path === `/items/${ep.id}/next`) return { status: 404, body: { code: "not_found", message: "" } };
      if (method === "PUT") return { status: 204 };
      return { status: 200, body: { item: ep, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false } };
    });
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    renderApp(`/watch/${ep.id}`, { me: testUser() });
    const video = (await screen.findByTestId("video")) as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute("src") ?? video.src).toContain("/stream/file/f/video.mp4"));
    Object.defineProperty(video, "currentTime", { value: 65, configurable: true, writable: true });
    fireEvent.timeUpdate(video);
    const skip = await screen.findByTestId("skip-segment");
    expect(skip).toHaveTextContent("Skip intro");
    fireEvent.click(skip);
    expect(video.currentTime).toBe(120);

    const bar = screen.getByRole("slider", { name: "Seek" });
    vi.spyOn(bar, "getBoundingClientRect").mockReturnValue({ left: 0, width: 1000, top: 0, height: 10, right: 1000, bottom: 10, x: 0, y: 0, toJSON: () => ({}) });
    fireEvent.pointerMove(bar, { clientX: 70 }); // 70 s, inside "Opening"
    const preview = await screen.findByTestId("seek-preview");
    expect(preview).toHaveTextContent("Opening · 1:10");
    expect(preview.querySelector("div")?.getAttribute("style")).toContain("/api/v1/trickplay/f/0");
  });

  it("seeks a transcoded stream with whole milliseconds (the server rejects fractions)", async () => {
    const starts: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        starts.push((body as { startMs?: number }).startMs);
        return { status: 200, body: { fileId: "f", method: "transcode", url: "/stream/hls/s/index.m3u8?exp=1&sig=x", startMs: 0,
          durationMs: 5_400_123, audio: [], subtitles: [] } };
      }
      if (method === "PUT") return { status: 204 };
      return { status: 200, body: { item, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false } };
    });
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    renderApp(`/watch/${item.id}`, { me: testUser() });
    const bar = await screen.findByRole("slider", { name: "Seek" });
    await waitFor(() => expect(starts).toHaveLength(1));
    bar.getBoundingClientRect = () => ({ left: 0, width: 333, top: 0, height: 10, right: 333, bottom: 10, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect;
    fireEvent.pointerDown(bar, { clientX: 217 });
    fireEvent.pointerUp(bar, { clientX: 217 });
    await waitFor(() => expect(starts).toHaveLength(2));
    expect(Number.isInteger(starts[1])).toBe(true);
    expect(starts[1]).toBe(Math.round((217 / 333) * 5_400_123));
  });
});

