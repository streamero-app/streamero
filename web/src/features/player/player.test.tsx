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
});
