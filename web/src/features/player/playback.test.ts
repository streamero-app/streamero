import { afterEach, describe, expect, it, vi } from "vitest";
import { keyBelongsToControl, requestSequence, subtitleSrc, toggleFullscreen, tryPlay } from "./playback";

afterEach(() => vi.restoreAllMocks());

describe("subtitleSrc", () => {
  it("asks the server to shift cues for a transcode that starts mid-way", () => {
    expect(subtitleSrc("/stream/sub/f/0.vtt?exp=1&sig=x", "transcode", 600_000.4)).toBe("/stream/sub/f/0.vtt?exp=1&sig=x&offsetMs=600000");
    expect(subtitleSrc("/a.vtt", "remux", 5000)).toBe("/a.vtt?offsetMs=5000");
  });
  it("keeps the file's own times for direct play and streams from 0", () => {
    expect(subtitleSrc("/a.vtt?x=1", "direct", 600_000)).toBe("/a.vtt?x=1");
    expect(subtitleSrc("/a.vtt?x=1", "transcode", 0)).toBe("/a.vtt?x=1");
  });
});

describe("requestSequence", () => {
  it("only the newest request counts", () => {
    const s = requestSequence();
    const a = s.next();
    const b = s.next();
    expect(s.isLatest(a)).toBe(false);
    expect(s.isLatest(b)).toBe(true);
    s.bump();
    expect(s.isLatest(b)).toBe(false);
  });
});

describe("keyBelongsToControl", () => {
  const button = () => {
    const b = document.createElement("button");
    document.body.append(b);
    return b;
  };
  it("gives Space to the player when a click merely left focus on a button", () => {
    const b = button();
    vi.spyOn(b, "matches").mockReturnValue(false);
    expect(keyBelongsToControl(b, " ")).toBe(false);
  });
  it("lets a keyboard-focused button take Space and Enter", () => {
    const b = button();
    vi.spyOn(b, "matches").mockImplementation((sel) => sel === ":focus-visible");
    expect(keyBelongsToControl(b, " ")).toBe(true);
    expect(keyBelongsToControl(b, "Enter")).toBe(true);
    expect(keyBelongsToControl(b, "k")).toBe(false);
  });
  it("always leaves keys to form fields", () => {
    expect(keyBelongsToControl(document.createElement("input"), "ArrowLeft")).toBe(true);
    expect(keyBelongsToControl(document.createElement("select"), " ")).toBe(true);
    expect(keyBelongsToControl(null, " ")).toBe(false);
  });
});

describe("toggleFullscreen", () => {
  it("falls back to the video's own full screen on iPhone", () => {
    const root = document.createElement("div") as HTMLDivElement & { requestFullscreen?: unknown };
    Object.defineProperty(root, "requestFullscreen", { value: undefined });
    const video = document.createElement("video") as HTMLVideoElement & { webkitEnterFullscreen: () => void };
    video.webkitEnterFullscreen = vi.fn();
    toggleFullscreen(root, video);
    expect(video.webkitEnterFullscreen).toHaveBeenCalled();
  });
  it("uses element full screen where it exists", () => {
    const root = document.createElement("div");
    const request = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(root, "requestFullscreen", { value: request });
    const video = document.createElement("video") as HTMLVideoElement & { webkitEnterFullscreen: () => void };
    video.webkitEnterFullscreen = vi.fn();
    toggleFullscreen(root, video);
    expect(request).toHaveBeenCalled();
    expect(video.webkitEnterFullscreen).not.toHaveBeenCalled();
  });
});

describe("tryPlay", () => {
  it("reports an autoplay refusal but not an interrupted load", async () => {
    const v = document.createElement("video");
    vi.spyOn(v, "play").mockRejectedValueOnce(new DOMException("no", "NotAllowedError"));
    expect(await tryPlay(v)).toBe(true);
    vi.spyOn(v, "play").mockRejectedValueOnce(new DOMException("aborted", "AbortError"));
    expect(await tryPlay(v)).toBe(false);
    vi.spyOn(v, "play").mockResolvedValueOnce(undefined);
    expect(await tryPlay(v)).toBe(false);
  });
});
