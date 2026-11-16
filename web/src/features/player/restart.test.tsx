import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

// A stand-in for hls.js that records every instance, so a test can make an old or the
// current stream fail the way hls.js reports it (fatal ERROR events).
const fake = vi.hoisted(() => {
  type Handler = (event: string, data: { fatal: boolean; type: string }) => void;
  class FakeHls {
    static Events = { MANIFEST_PARSED: "hlsManifestParsed", ERROR: "hlsError" };
    static ErrorTypes = { NETWORK_ERROR: "networkError", MEDIA_ERROR: "mediaError" };
    url = "";
    handlers: Record<string, Handler[]> = {};
    stopLoad = vi.fn();
    startLoad = vi.fn();
    recoverMediaError = vi.fn();
    destroy = vi.fn();
    constructor() { fake.instances.push(this); }
    on(event: string, fn: Handler) { (this.handlers[event] ??= []).push(fn); }
    loadSource(url: string) { this.url = url; }
    attachMedia() {}
    fail(type: "networkError" | "mediaError") { for (const fn of this.handlers.hlsError ?? []) fn("hlsError", { fatal: true, type }); }
  }
  return { FakeHls, instances: [] as InstanceType<typeof FakeHls>[] };
});
vi.mock("hls.js", () => ({ default: fake.FakeHls }));

afterEach(() => vi.unstubAllGlobals());

describe("player restarts", () => {
  beforeEach(() => {
    setLanguage("en");
    fake.instances.length = 0;
  });
  const item = { id: "0190a8b0-0000-7000-8000-00000000dddd", kind: "movie", title: "Film", addedAt: new Date().toISOString(), hasPoster: false };
  const detail = { item, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false };

  /** Every playback request gets its own HLS session (s1, s2, …), like the server. */
  function serve() {
    const requests: { audioIndex?: number; device: { videoCodecs: string[] } }[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "POST" && path.endsWith("/playback")) {
        requests.push(body as (typeof requests)[number]);
        return { status: 200, body: { fileId: "f", method: "remux", url: `/stream/hls/s${requests.length}/index.m3u8?exp=1&sig=x`, startMs: 0,
          durationMs: 100_000, audio: [{ codec: "aac", language: "en" }, { codec: "ac3", language: "hr" }], audioIndex: 0,
          subtitles: [{ index: 2, url: "/stream/sub/f/2.vtt?exp=1&sig=x", external: false, language: "en" }] } };
      }
      if (method === "PUT") return { status: 204 };
      return { status: 200, body: detail };
    });
    return requests;
  }
  const session = (n: number) => fake.instances.find((h) => h.url.startsWith(`/stream/hls/s${n}/`));

  it("ignores the replaced stream's errors while switching audio (no fallback, no error)", async () => {
    const requests = serve();
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    renderApp(`/watch/${item.id}`, { me: testUser() });
    const video = (await screen.findByTestId("video")) as HTMLVideoElement;
    await waitFor(() => expect(session(1)).toBeDefined());
    const old = session(1)!;

    fireEvent.change(screen.getByLabelText("Audio"), { target: { value: "1" } });
    // The server stopped s1 the moment s2 was asked for: its playlist and segments now 404.
    expect(old.stopLoad).toHaveBeenCalled();
    act(() => {
      old.fail("networkError");
      old.fail("networkError");
      fireEvent.error(video);
    });
    await waitFor(() => expect(session(2)).toBeDefined());
    act(() => old.fail("networkError"));
    // …but the new stream's own error still counts (one reload, below).
    act(() => session(2)!.fail("networkError"));
    expect(session(2)!.startLoad).toHaveBeenCalledTimes(1);

    await new Promise((r) => setTimeout(r, 50));
    expect(old.startLoad).not.toHaveBeenCalled();
    expect(requests).toHaveLength(2); // the switch itself, no safe-mode request
    expect(requests[1]).toMatchObject({ audioIndex: 1 });
    expect(requests[1]!.device.videoCodecs).not.toEqual(["h264"]);
    expect(screen.queryByTestId("playback-error")).not.toBeInTheDocument();
    expect(screen.queryByText(/being converted/)).not.toBeInTheDocument();
  });

  it("still falls back once, then shows the error, when the current stream fails", async () => {
    const requests = serve();
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    renderApp(`/watch/${item.id}`, { me: testUser() });
    await waitFor(() => expect(session(1)).toBeDefined());
    const first = session(1)!;
    act(() => {
      first.fail("networkError"); // one reload…
      first.fail("networkError"); // …then the stream every browser plays
      first.fail("networkError"); // the same failure again: no second fallback
    });
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(first.startLoad).toHaveBeenCalledTimes(1);
    expect(requests[1]!.device.videoCodecs).toEqual(["h264"]);
    await waitFor(() => expect(session(2)).toBeDefined());
    expect(screen.queryByTestId("playback-error")).not.toBeInTheDocument();
    act(() => {
      session(2)!.fail("networkError");
      session(2)!.fail("networkError");
    });
    expect(await screen.findByTestId("playback-error")).toHaveTextContent("can't be played in this browser");
    expect(requests).toHaveLength(2);
  });
});
