import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { blockGeometry, shiftWindow, ticks, windowStart } from "./time";
import { behindLabel } from "./LivePlayer";

afterEach(() => vi.unstubAllGlobals());
beforeEach(() => setLanguage("en"));

const MIN = 60_000;
const chId = "0190a8b0-0000-7000-8000-0000000000c1";
const progA = "0190a8b0-0000-7000-8000-0000000000a1";
const progB = "0190a8b0-0000-7000-8000-0000000000b1";
const iso = (ms: number) => new Date(ms).toISOString();

const channel = { id: chId, number: "5", name: "Kino Plus", tunerId: "0190a8b0-0000-7000-8000-0000000000f1", favorite: false };

/** Programmes placed relative to the requested window start. */
function guideFor(startIso: string) {
  const s = new Date(startIso).getTime();
  return {
    start: startIso, end: iso(s + 3 * 60 * MIN),
    rows: [{
      channel,
      programs: [
        { id: progA, channelId: chId, title: "Morning News", start: iso(s - 30 * MIN), end: iso(s + 30 * MIN), categories: ["News"], category: "news", isNew: false },
        { id: progB, channelId: chId, title: "The Big Match", subtitle: "Final", start: iso(s + 30 * MIN), end: iso(s + 90 * MIN),
          categories: ["Sports"], category: "sports", isNew: true, season: 2, episode: 5 },
      ],
    }],
  };
}

describe("guide geometry", () => {
  it("clips programmes at the window edges and skips ones outside", () => {
    const s = Date.UTC(2026, 10, 11, 20, 0);
    const e = s + 3 * 60 * MIN;
    expect(blockGeometry({ start: iso(s - 30 * MIN), end: iso(s + 30 * MIN) }, s, e, 6)).toEqual({ left: 0, width: 180, clippedStart: true, clippedEnd: false });
    expect(blockGeometry({ start: iso(s + 150 * MIN), end: iso(s + 240 * MIN) }, s, e, 6)).toEqual({ left: 900, width: 180, clippedStart: false, clippedEnd: true });
    expect(blockGeometry({ start: iso(e), end: iso(e + MIN) }, s, e, 6)).toBeNull();
    expect(blockGeometry({ start: iso(s - 60 * MIN), end: iso(s) }, s, e, 6)).toBeNull();
  });
  it("starts at the half hour and ticks every 30 minutes", () => {
    const at = new Date(Date.UTC(2026, 10, 11, 20, 47, 13));
    expect(windowStart(at).toISOString()).toBe("2026-11-11T20:30:00.000Z");
    const s = windowStart(at).getTime();
    expect(ticks(s, shiftWindow(new Date(s), 3).getTime())).toHaveLength(6);
    expect(behindLabel(125)).toBe("−2:05");
  });
});

describe("Live TV guide", () => {
  function mock(posts: { path: string; body: unknown }[], starts: string[]) {
    return mockApi(({ method, path, body, query }) => {
      if (path === "/livetv/guide") {
        starts.push(query.get("start")!);
        return { status: 200, body: guideFor(query.get("start")!) };
      }
      if (path === "/livetv/recordings" && method === "POST") {
        posts.push({ path, body });
        return { status: 201, body: { id: "0190a8b0-0000-7000-8000-0000000000d1", title: "The Big Match", start: iso(Date.now()), end: iso(Date.now()),
          prePaddingSeconds: 120, postPaddingSeconds: 300, priority: 0, status: "scheduled", createdAt: iso(Date.now()) } };
      }
      if (path === "/livetv/series-rules" && method === "POST") {
        posts.push({ path, body });
        return { status: 201, body: { id: "0190a8b0-0000-7000-8000-0000000000e1", title: "The Big Match", newOnly: true, priority: 0, keepLast: 0, upcoming: 3, createdAt: iso(Date.now()) } };
      }
      return { status: 200, body: [] };
    });
  }

  it("lays programmes out on the time grid and moves the window", async () => {
    const starts: string[] = [];
    mock([], starts);
    renderApp("/livetv", { me: testUser() });
    const blocks = await screen.findAllByTestId("guide-program");
    expect(blocks).toHaveLength(2);
    // 6 px per minute: the first started 30 min before the window (clipped), the second 30 min in.
    expect(blocks[0]).toHaveStyle({ left: "0px" });
    expect(blocks[0]!.style.width).toBe("178px");
    expect(blocks[1]).toHaveStyle({ left: "180px" });
    expect(blocks[1]!.style.width).toBe("358px");
    expect(blocks[1]).toHaveTextContent("The Big Match");
    expect(screen.getAllByTestId("guide-tick")).toHaveLength(6);
    expect(screen.getByTestId("guide-now-line")).toBeInTheDocument();
    const first = starts[0]!;
    expect(new Date(first).getTime() % (30 * MIN)).toBe(0);

    fireEvent.click(screen.getByRole("button", { name: "Later" }));
    await waitFor(() => expect(starts.at(-1)).toBe(iso(new Date(first).getTime() + 3 * 60 * MIN)));
    expect(screen.queryByTestId("guide-now-line")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Earlier" }));
    fireEvent.click(screen.getByRole("button", { name: "Earlier" }));
    await waitFor(() => expect(starts.at(-1)).toBe(iso(new Date(first).getTime() - 3 * 60 * MIN)));
    fireEvent.click(screen.getByRole("button", { name: "Sports" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Sports" })).toHaveAttribute("aria-pressed", "true"));
  });

  it("schedules a recording and a series from the programme dialog", async () => {
    const posts: { path: string; body: unknown }[] = [];
    mock(posts, []);
    renderApp("/livetv", { me: testUser({ permissions: { ...testUser().permissions, manageLiveTv: true } }) });
    fireEvent.click((await screen.findAllByTestId("guide-program"))[1]!);
    const dialog = await screen.findByTestId("program-dialog");
    expect(dialog).toHaveTextContent("S2 E5 · Final");
    expect(within(dialog).queryByRole("button", { name: "Watch" })).not.toBeInTheDocument(); // not airing yet

    fireEvent.click(within(dialog).getByRole("button", { name: "Record" }));
    await waitFor(() => expect(posts).toHaveLength(1));
    expect(posts[0]).toEqual({ path: "/livetv/recordings", body: { programId: progB, channelId: chId } });
    expect(await within(dialog).findByText("Scheduled")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Cancel recording" })).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole("checkbox", { name: "New episodes only" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Record series" }));
    await waitFor(() => expect(posts).toHaveLength(2));
    expect(posts[1]).toEqual({ path: "/livetv/series-rules", body: { programId: progB, channelId: chId, newOnly: false } });
    expect(await within(dialog).findByText("Series recording is on")).toBeInTheDocument();
  });

  it("offers no recording to viewers without manageLiveTv", async () => {
    mock([], []);
    renderApp("/livetv", { me: testUser() });
    fireEvent.click((await screen.findAllByTestId("guide-program"))[1]!);
    const dialog = await screen.findByTestId("program-dialog");
    expect(within(dialog).queryByRole("button", { name: "Record" })).not.toBeInTheDocument();
  });

  it("is closed to accounts without the liveTv permission", async () => {
    mock([], []);
    renderApp("/livetv", { me: testUser({ permissions: { ...testUser().permissions, liveTv: false } }) });
    expect(await screen.findByTestId("livetv-denied")).toBeInTheDocument();
  });

  it("is translated to Croatian", async () => {
    setLanguage("hr");
    mock([], []);
    renderApp("/livetv", { me: testUser() });
    await screen.findAllByTestId("guide-program");
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("TV uživo");
    expect(screen.getByRole("button", { name: "Kasnije" })).toBeInTheDocument();
  });
});

describe("Series rules page", () => {
  it("creates a rule for one channel with priority and padding", async () => {
    const bodies: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/livetv/series-rules" && method === "POST") {
        bodies.push(body);
        return { status: 201, body: { id: "0190a8b0-0000-7000-8000-0000000000e2", title: "Dnevnik", newOnly: false, priority: 5, keepLast: 3, upcoming: 1, createdAt: iso(Date.now()) } };
      }
      if (path === "/livetv/channels") return { status: 200, body: [channel] };
      return { status: 200, body: [] };
    });
    renderApp("/livetv/series", { me: testUser({ isAdmin: true }) });
    fireEvent.click(await screen.findByRole("button", { name: "New series rule" }));
    const form = await screen.findByTestId("series-form");
    fireEvent.change(within(form).getByLabelText("Title"), { target: { value: " Dnevnik " } });
    await within(form).findByRole("option", { name: "5 · Kino Plus" });
    fireEvent.change(within(form).getByLabelText("Channel"), { target: { value: chId } });
    fireEvent.click(within(form).getByLabelText("All airings"));
    fireEvent.change(within(form).getByLabelText("Priority"), { target: { value: "5" } });
    fireEvent.change(within(form).getByLabelText("Keep newest"), { target: { value: "3" } });
    fireEvent.change(within(form).getByLabelText("End late (min)"), { target: { value: "10" } });
    fireEvent.click(within(form).getByRole("button", { name: "Create rule" }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toEqual({ title: "Dnevnik", newOnly: false, priority: 5, keepLast: 3, channelId: chId, postPaddingSeconds: 600 });
  });
});

describe("Channels and Home", () => {
  const withNow = {
    ...channel,
    now: { id: progA, channelId: chId, title: "Evening News", start: iso(Date.now() - 10 * MIN), end: iso(Date.now() + 20 * MIN), categories: [], isNew: false },
  };

  it("toggles a favourite channel", async () => {
    const calls: string[] = [];
    mockApi(({ method, path }) => {
      if (path === `/livetv/channels/${chId}/favorite`) {
        calls.push(method);
        return { status: 204 };
      }
      if (path === "/livetv/channels") return { status: 200, body: [withNow] };
      return { status: 200, body: [] };
    });
    renderApp("/livetv/channels", { me: testUser() });
    expect(await screen.findByText("Evening News")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Add Kino Plus to favourites" }));
    await waitFor(() => expect(calls).toEqual(["PUT"]));
  });

  it("shows the On now row on Home only when something is on", async () => {
    mockApi(({ path }) => {
      if (path === "/livetv/on-now") return { status: 200, body: [withNow] };
      if (path === "/me/preferences") return { status: 200, body: {} };
      return { status: 200, body: [] };
    });
    renderApp("/", { me: testUser() });
    const row = await screen.findByTestId("on-now");
    expect(within(row).getByTestId("on-now-card")).toHaveAttribute("href", `/livetv/watch/${chId}`);
    expect(row).toHaveTextContent("Evening News");
  });
});
