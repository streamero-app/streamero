import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { ClockSync, inviteLink, parseInvite, plan, sharedPosition, waitingFor, type SyncGroup } from "./engine";

const self = "0190a8b0-0000-7000-8000-0000000000a1";
const other = "0190a8b0-0000-7000-8000-0000000000a2";
const itemId = "0190a8b0-0000-7000-8000-00000000cccc";

function group(over: Partial<SyncGroup> = {}): SyncGroup {
  return {
    id: "0190a8b0-0000-7000-8000-0000000000g1", name: "Ana", state: "playing", index: 0, positionMs: 10_000, anchorAt: 1_000_000,
    speed: 1, version: 3, serverTime: 1_000_000, createdAt: new Date().toISOString(), selfId: self,
    queue: [{ id: "e1", itemId, title: "Film", durationMs: 6_000_000 }],
    members: [
      { id: self, name: "Ana", device: "Firefox", ready: true, buffering: false, connected: true, lagging: false },
      { id: other, name: "Ben", device: "iPhone", ready: true, buffering: false, connected: true, lagging: false },
    ],
    ...over,
  };
}

describe("sync engine", () => {
  it("keeps the clock sample with the shortest round trip", () => {
    const c = new ClockSync();
    c.add(1000, 1400, 6000); // rtt 400, offset 4800
    c.add(2000, 2020, 5810); // rtt 20, offset 3800
    c.add(3000, 3300, 9000); // rtt 300, offset 5850
    expect(c.offset).toBe(3800);
    expect(c.serverNow(10_000)).toBe(10_000 + 3800);
  });

  it("computes the shared position", () => {
    const g = group();
    expect(sharedPosition(g, 999_000)).toBe(10_000); // before the anchor
    expect(sharedPosition(g, 1_004_000)).toBe(14_000);
    expect(sharedPosition({ ...g, speed: 1.5 }, 1_004_000)).toBe(16_000);
    expect(sharedPosition({ ...g, state: "paused" }, 1_004_000)).toBe(10_000);
  });

  it("schedules a start in the future and reports ready once positioned", () => {
    const g = group({ anchorAt: 1_000_600, members: [{ id: self, name: "Ana", device: "", ready: false, buffering: false, connected: true, lagging: false }] });
    expect(plan(g, { itemId, positionMs: 0, canPlay: true, seeking: false }, 1_000_000))
      .toMatchObject({ play: false, startAt: 1_000_600, seekTo: 10_000, sendReady: false });
    expect(plan(g, { itemId, positionMs: 10_050, canPlay: true, seeking: false }, 1_000_000))
      .toMatchObject({ play: false, startAt: 1_000_600, sendReady: true });
  });

  it("follows a playing group and leaves small drift to the server", () => {
    const g = group();
    const near = plan(g, { itemId, positionMs: 14_500, canPlay: true, seeking: false }, 1_004_000);
    expect(near.play).toBe(true);
    expect(near.seekTo).toBeUndefined();
    expect(plan(g, { itemId, positionMs: 1_000, canPlay: true, seeking: false }, 1_004_000)).toMatchObject({ play: true, seekTo: 14_000 });
    expect(plan({ ...g, state: "paused" }, { itemId, positionMs: 10_900, canPlay: true, seeking: false }, 1_004_000))
      .toMatchObject({ play: false, seekTo: 10_000 });
    expect(plan(g, { itemId: "another", positionMs: 0, canPlay: true, seeking: false }, 1_004_000)).toMatchObject({ open: itemId });
  });

  it("names who the group waits for and reads invite links", () => {
    const g = group({ state: "waiting", members: [
      { id: self, name: "Ana", device: "", ready: true, buffering: false, connected: true, lagging: false },
      { id: other, name: "Ben", device: "", ready: false, buffering: true, connected: true, lagging: false },
      { id: "x", name: "Cleo", device: "", ready: false, buffering: false, connected: false, lagging: false },
    ] });
    expect(waitingFor(g).map((m) => m.name)).toEqual(["Ben"]);
    expect(inviteLink("abc", "https://demo.example")).toBe("https://demo.example/together/abc");
    expect(parseInvite("https://demo.example/together/0190A8B0-0000-7000-8000-0000000000AB ")).toBe("0190a8b0-0000-7000-8000-0000000000ab");
    expect(parseInvite("nonsense")).toBeNull();
  });
});

/** A stand-in for the browser WebSocket that records what the page sends. */
class FakeSocket {
  static last: FakeSocket | null = null;
  readyState = 0;
  sent: Record<string, unknown>[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  constructor(public url: string) {
    FakeSocket.last = this;
    queueMicrotask(() => { this.readyState = 1; this.onopen?.(); });
  }
  send(data: string) { this.sent.push(JSON.parse(data) as Record<string, unknown>); }
  close() { this.readyState = 3; this.onclose?.(); }
  receive(m: unknown) { this.onmessage?.({ data: JSON.stringify(m) }); }
}

afterEach(() => { vi.unstubAllGlobals(); FakeSocket.last = null; });

describe("watch together in the player", () => {
  beforeEach(() => setLanguage("en"));
  const item = { id: itemId, kind: "movie", title: "Film", addedAt: new Date().toISOString(), hasPoster: false };

  it("rejoins the group after a reload, sends the play button to the group and shows who it waits for", async () => {
    mockApi(({ method, path }) => {
      if (path === "/syncplay/me") return { status: 200, body: group({ state: "paused" }) };
      if (method === "POST" && path.endsWith("/playback")) {
        return { status: 200, body: { fileId: "f", method: "direct", url: "/stream/file/f/video.mp4?exp=1&sig=x", startMs: 0, durationMs: 6_000_000, audio: [], subtitles: [] } };
      }
      if (method === "PUT") return { status: 204 };
      return { status: 200, body: { item, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false } };
    });
    vi.stubGlobal("WebSocket", FakeSocket);
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    renderApp(`/watch/${itemId}`, { me: testUser() });
    await waitFor(() => expect(FakeSocket.last?.readyState).toBe(1));
    const sock = FakeSocket.last!;
    expect(sock.url).toMatch(/\/api\/v1\/syncplay\/socket$/);
    await waitFor(() => expect(sock.sent.some((m) => m.type === "ping")).toBe(true));
    await screen.findByTestId("video");
    play.mockClear();

    // The play button asks the group instead of playing locally.
    fireEvent.click(screen.getByRole("button", { name: "Play" }));
    expect(sock.sent.at(-1)).toEqual({ type: "play" });
    expect(play).not.toHaveBeenCalled();

    // Ben buffers: everyone sees whom they wait for.
    sock.receive({ type: "state", serverTime: 1_000_000, group: group({ state: "waiting", version: 4, reason: "buffering", actor: "Ben", members: [
      { id: self, name: "Ana", device: "", ready: true, buffering: false, connected: true, lagging: false },
      { id: other, name: "Ben", device: "", ready: false, buffering: true, connected: true, lagging: false },
    ] }) });
    expect(await screen.findByTestId("together-waiting")).toHaveTextContent("Waiting for Ben");

    // The panel shows the invite link and members; leaving tells the server.
    fireEvent.click(screen.getByTestId("together-toggle"));
    expect(screen.getByTestId("together-link")).toHaveValue(`${location.origin}/together/${group().id}`);
    expect(screen.getByTestId("together-members")).toHaveTextContent("Buffering");
    fireEvent.click(screen.getByTestId("together-leave"));
    await waitFor(() => expect(sock.readyState).toBe(3));
    expect(screen.queryByTestId("together-waiting")).not.toBeInTheDocument();
  });

  it("joins from an invite link and opens the group's title", async () => {
    const joined: string[] = [];
    mockApi(({ method, path }) => {
      if (path === "/syncplay/me") return { status: 204 };
      if (method === "GET" && path.startsWith("/syncplay/groups/")) return { status: 200, body: group({ selfId: undefined }) };
      if (method === "POST" && path.endsWith("/join")) {
        joined.push(path);
        return { status: 200, body: group() };
      }
      if (method === "POST" && path.endsWith("/playback")) {
        return { status: 200, body: { fileId: "f", method: "direct", url: "/stream/file/f/video.mp4?exp=1&sig=x", startMs: 0, durationMs: 6_000_000, audio: [], subtitles: [] } };
      }
      return { status: 200, body: { item, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false } };
    });
    vi.stubGlobal("WebSocket", FakeSocket);
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    const { router } = renderApp(`/together/${group().id}`, { me: testUser() });
    expect(await screen.findByText("Ana's watch party")).toBeInTheDocument();
    expect(screen.getByText("Film")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Join" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/watch/${itemId}`));
    expect(joined).toEqual([`/syncplay/groups/${group().id}/join`]);
    await waitFor(() => expect(FakeSocket.last).not.toBeNull());
  });

  it("hides watch together without the permission", async () => {
    mockApi(() => ({ status: 200, body: [] }));
    renderApp("/together", { me: testUser({ permissions: { ...testUser().permissions, syncPlay: false } }) });
    expect(await screen.findByText("Watching together is turned off for this account.")).toBeInTheDocument();
  });
});
