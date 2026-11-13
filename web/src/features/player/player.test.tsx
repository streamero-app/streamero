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

  it("falls back to an H.264/AAC stream when the browser can't decode the file", async () => {
    const devices: { videoCodecs: string[] }[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        devices.push((body as { device: { videoCodecs: string[] } }).device);
        return { status: 200, body: { fileId: "f", method: "direct", url: `/stream/file/f/video${devices.length}.mp4?exp=1&sig=x`, startMs: 0,
          durationMs: 100_000, audio: [{ codec: "aac" }], subtitles: [] } };
      }
      if (method === "PUT") return { status: 204 };
      return { status: 200, body: { item, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false } };
    });
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    renderApp(`/watch/${item.id}`, { me: testUser() });
    const video = (await screen.findByTestId("video")) as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute("src") ?? video.src).toContain("video1.mp4"));
    fireEvent.error(video);
    await waitFor(() => expect(devices).toHaveLength(2));
    expect(devices[1]?.videoCodecs).toEqual(["h264"]);
    await waitFor(() => expect(video.getAttribute("src") ?? video.src).toContain("video2.mp4"));
    fireEvent.error(video);
    await new Promise((r) => setTimeout(r, 50));
    expect(devices).toHaveLength(2);
    // The safe stream failed too: say so (no silent black screen) and let the viewer retry it.
    expect(await screen.findByTestId("playback-error")).toHaveTextContent("can't be played in this browser");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(devices).toHaveLength(3));
    expect(devices[2]?.videoCodecs).toEqual(["h264"]);
    await waitFor(() => expect(screen.queryByTestId("playback-error")).not.toBeInTheDocument());
  });

  it("adds the stream offset for HLS that starts mid-way", async () => {
    let progress: unknown;
    mockApi(({ method, path, body }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        return { status: 200, body: { fileId: "f", method: "transcode", url: "/stream/hls/s/index.m3u8?exp=1&sig=x", startMs: 600_000,
          audio: [], subtitles: [{ index: 0, url: "/stream/sub/f/0.vtt?exp=1&sig=x", external: true, language: "hr" }] } };
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
    // The stream's 0 is 10:00 into the file: the subtitle cues are asked for shifted to match.
    expect(video.querySelector("track")?.getAttribute("src")).toBe("/stream/sub/f/0.vtt?exp=1&sig=x&offsetMs=600000");
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

  const detailOf = (it: typeof item) => ({ item: it, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false });

  it("keeps each episode's own position when moving on to the next one", async () => {
    const ep = { ...item, id: "0190a8b0-0000-7000-8000-00000000e001", kind: "episode", title: "Pilot", seasonNumber: 1, episodeNumber: 1 };
    const nextEp = { ...ep, id: "0190a8b0-0000-7000-8000-00000000e002", title: "Second", episodeNumber: 2 };
    const puts: { path: string; body: unknown }[] = [];
    let starts = 0;
    mockApi(({ method, path, body }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        starts++;
        return { status: 200, body: { fileId: "f", method: "direct", url: `/stream/file/f/ep${starts}.mp4?exp=1&sig=x`, startMs: 0, durationMs: 100_000,
          audio: [{ codec: "aac" }], subtitles: [] } };
      }
      if (path === `/items/${ep.id}/next`) return { status: 200, body: nextEp };
      if (path === `/items/${nextEp.id}/next`) return { status: 404, body: { code: "not_found", message: "" } };
      if (path === "/me/preferences") return { status: 200, body: { autoplayNext: false } };
      if (method === "PUT") {
        puts.push({ path, body });
        return { status: 204 };
      }
      return { status: 200, body: detailOf(path.includes(nextEp.id) ? nextEp : ep) };
    });
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
    const { router } = renderApp(`/watch/${ep.id}`, { me: testUser() });
    const video = (await screen.findByTestId("video")) as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute("src")).toContain("ep1.mp4"));
    Object.defineProperty(video, "currentTime", { value: 95, configurable: true, writable: true });
    fireEvent.ended(video);
    fireEvent.click(await screen.findByRole("button", { name: /Play now/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/watch/${nextEp.id}`));
    await waitFor(() => expect(starts).toBe(2));
    await new Promise((r) => setTimeout(r, 50));
    expect(puts.some((p) => p.path === `/items/${ep.id}/progress` && (p.body as { positionMs: number }).positionMs === 95_000)).toBe(true);
    // The next episode never gets the previous one's end position (it would resume near the end).
    expect(puts.filter((p) => p.path === `/items/${nextEp.id}/progress`)).toEqual([]);
  });

  it("does not steal focus for Play next: Space keeps playing and pausing", async () => {
    const ep = { ...item, id: "0190a8b0-0000-7000-8000-00000000e003", kind: "episode", title: "Pilot", seasonNumber: 1, episodeNumber: 1 };
    const nextEp = { ...ep, id: "0190a8b0-0000-7000-8000-00000000e004", title: "Second", episodeNumber: 2 };
    mockApi(({ method, path }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        return { status: 200, body: { fileId: "f", method: "direct", url: "/stream/file/f/video.mp4?exp=1&sig=x", startMs: 0, durationMs: 100_000,
          audio: [{ codec: "aac" }], subtitles: [] } };
      }
      if (path === `/items/${ep.id}/next`) return { status: 200, body: nextEp };
      if (path === "/me/preferences") return { status: 200, body: { autoplayNext: false } };
      if (method === "PUT") return { status: 204 };
      return { status: 200, body: detailOf(ep) };
    });
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    const { router } = renderApp(`/watch/${ep.id}`, { me: testUser() });
    const video = (await screen.findByTestId("video")) as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute("src")).toContain("video.mp4"));
    Object.defineProperty(video, "currentTime", { value: 90, configurable: true, writable: true });
    fireEvent.timeUpdate(video); // 10 s left: the up-next card appears
    const next = await screen.findByRole("button", { name: /Play now/ });
    expect(next).not.toHaveFocus();
    play.mockClear();
    fireEvent.keyDown(document.body, { key: " " });
    expect(play).toHaveBeenCalledTimes(1);
    expect(router.state.location.pathname).toBe(`/watch/${ep.id}`);
  });

  it("keeps a paused viewer paused when the audio track restarts the stream", async () => {
    let n = 0;
    mockApi(({ method, path }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        n++;
        return { status: 200, body: { fileId: "f", method: "transcode", url: `/stream/hls/s${n}/index.m3u8?exp=1&sig=x`, startMs: 0, durationMs: 100_000,
          audio: [{ codec: "aac", language: "en" }, { codec: "ac3", language: "hr" }], subtitles: [] } };
      }
      if (method === "PUT") return { status: 204 };
      return { status: 200, body: detailOf(item) };
    });
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    vi.spyOn(HTMLMediaElement.prototype, "canPlayType").mockImplementation((t: string) => (t.includes("mpegurl") ? "maybe" : ""));
    renderApp(`/watch/${item.id}`, { me: testUser() });
    const video = (await screen.findByTestId("video")) as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute("src")).toContain("/s1/"));
    // Safari-style native HLS: playback starts once the stream can play.
    expect(play).not.toHaveBeenCalled();
    fireEvent.canPlay(video);
    expect(play).toHaveBeenCalledTimes(1);

    // Playing → switch audio → plays again.
    Object.defineProperty(video, "paused", { value: false, configurable: true });
    fireEvent.change(screen.getByLabelText("Audio"), { target: { value: "1" } });
    await waitFor(() => expect(video.getAttribute("src")).toContain("/s2/"));
    fireEvent.canPlay(video);
    expect(play).toHaveBeenCalledTimes(2);

    // Paused → switch audio → stays paused.
    Object.defineProperty(video, "paused", { value: true, configurable: true });
    fireEvent.change(screen.getByLabelText("Audio"), { target: { value: "0" } });
    await waitFor(() => expect(video.getAttribute("src")).toContain("/s3/"));
    fireEvent.canPlay(video);
    expect(play).toHaveBeenCalledTimes(2);
  });

  it("shows a big play button when the browser refuses to start on its own", async () => {
    mockApi(({ method, path }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        return { status: 200, body: { fileId: "f", method: "direct", url: "/stream/file/f/video.mp4?exp=1&sig=x", startMs: 0, durationMs: 100_000,
          audio: [{ codec: "aac" }], subtitles: [] } };
      }
      if (method === "PUT") return { status: 204 };
      return { status: 200, body: detailOf(item) };
    });
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockRejectedValueOnce(new DOMException("gesture needed", "NotAllowedError"));
    renderApp(`/watch/${item.id}`, { me: testUser() });
    const video = (await screen.findByTestId("video")) as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute("src")).toContain("video.mp4"));
    fireEvent.canPlay(video);
    const big = await screen.findByTestId("tap-to-play");
    play.mockResolvedValue(undefined);
    fireEvent.click(big);
    expect(play).toHaveBeenCalledTimes(2);
    fireEvent.play(video);
    await waitFor(() => expect(screen.queryByTestId("tap-to-play")).not.toBeInTheDocument());
  });

  it("keeps the chosen speed when a new stream loads", async () => {
    mockApi(({ method, path }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        return { status: 200, body: { fileId: "f", method: "direct", url: "/stream/file/f/video.mp4?exp=1&sig=x", startMs: 0, durationMs: 100_000,
          audio: [{ codec: "aac" }], subtitles: [] } };
      }
      if (method === "PUT") return { status: 204 };
      return { status: 200, body: detailOf(item) };
    });
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    renderApp(`/watch/${item.id}`, { me: testUser() });
    const video = (await screen.findByTestId("video")) as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute("src")).toContain("video.mp4"));
    fireEvent.click(screen.getByRole("button", { name: "Playback speed" }));
    fireEvent.click(screen.getByRole("menuitemradio", { name: "1.5×", hidden: true }));
    expect(video.playbackRate).toBe(1.5);
    expect(video.defaultPlaybackRate).toBe(1.5);
    video.playbackRate = 1; // what a new source does
    fireEvent.loadedMetadata(video);
    expect(video.playbackRate).toBe(1.5);
  });
});

