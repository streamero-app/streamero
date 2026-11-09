import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { detectAudioProfile } from "@/features/player/deviceProfile";

afterEach(() => vi.unstubAllGlobals());

const album = { id: "0190a8b0-0000-7000-8000-0000000000a1", kind: "album", title: "First", year: 2001, artist: "Band", addedAt: new Date().toISOString(), hasPoster: false };
const tracks = [1, 2].map((n) => ({ id: `0190a8b0-0000-7000-8000-00000000000${n}`, kind: "track", title: `Song ${n}`, episodeNumber: n, durationMs: 185_000,
  artist: "Band", album: "First", parentId: album.id, addedAt: new Date().toISOString(), hasPoster: false }));

describe("music", () => {
  beforeEach(() => {
    setLanguage("en");
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
  });

  it("plays an album from the chosen song and shows the now-playing bar", async () => {
    const plays: string[] = [];
    mockApi(({ method, path }) => {
      if (path === `/items/${album.id}`) {
        return { status: 200, body: { item: album, libraryId: "0190a8b0-0000-7000-8000-00000000aaaa", parentId: "0190a8b0-0000-7000-8000-0000000000b1",
          providerIds: {}, children: tracks, files: [], genres: ["Rock"], cast: [], crew: [], hasBackdrop: false } };
      }
      if (method === "POST" && path.endsWith("/playback")) {
        plays.push(path);
        return { status: 200, body: { method: "direct", url: "/stream/file/x.flac", itemId: tracks[1]!.id, fileId: tracks[1]!.id, startMs: 0, audio: [], subtitles: [], durationMs: 185_000 } };
      }
      return { status: 200, body: [] };
    });
    renderApp(`/item/${album.id}`, { me: testUser() });
    expect(await screen.findByTestId("album-page")).toHaveTextContent("Band");
    fireEvent.click(screen.getAllByTestId("track-row")[1]!);
    await waitFor(() => expect(plays).toContain(`/items/${tracks[1]!.id}/playback`));
    expect(await screen.findByTestId("now-playing-bar")).toHaveTextContent("Song 2");
    fireEvent.click(screen.getByRole("button", { name: "Queue" }));
    expect(screen.getByTestId("music-queue")).toHaveTextContent("Song 1");
  });

  it("asks only for what the browser plays", () => {
    const p = detectAudioProfile((mime) => mime === "audio/mpeg" || mime === "audio/flac");
    expect(p.containers).toEqual(["mp3", "flac"]);
    expect(p.audioCodecs).toEqual(["mp3", "flac"]);
    expect(p.videoCodecs).toEqual([]);
  });
});
