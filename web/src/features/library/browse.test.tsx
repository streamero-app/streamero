import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { gridWindow } from "@/components/useWindowedGrid";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { validateLibrarySearch } from "./LibraryPages";
import { USER_DATA_KEYS } from "./userData";

afterEach(() => vi.unstubAllGlobals());
beforeEach(() => setLanguage("en"));

const libA = { id: "0190a8b0-0000-7000-8000-00000000aaa1", name: "Filmovi", kind: "movies", paths: ["/a"], itemCount: 1, scan: { state: "idle", filesSeen: 1 } };
const libB = { id: "0190a8b0-0000-7000-8000-00000000aaa2", name: "Serije", kind: "shows", paths: ["/b"], itemCount: 1, scan: { state: "idle", filesSeen: 1 } };
const movie = (id: string, title: string, over: object = {}) =>
  ({ id, kind: "movie", title, year: 2020, addedAt: new Date().toISOString(), hasPoster: false, ...over });

describe("windowed grid", () => {
  it("renders the rows near the screen and pads the rest", () => {
    // 1000 items, 5 per row, rows 300 px high, grid starting 400 px down, viewport 900 px.
    const top = gridWindow(1000, 5, 300, 400, 0, 900);
    expect(top.start).toBe(0);
    expect(top.end).toBe(25); // 2 rows fit under the 400 px header + 3 overscan
    expect(top.padTop).toBe(0);
    expect(top.padBottom).toBe((200 - 5) * 300);

    const middle = gridWindow(1000, 5, 300, 400, 30_400, 900);
    expect(middle.start).toBe((100 - 3) * 5);
    expect(middle.padTop).toBe(97 * 300);
    expect(middle.end).toBe((103 + 3) * 5);
    // Padding plus rendered rows always add up to the whole grid.
    expect(middle.padTop / 300 + (middle.end - middle.start) / 5 + middle.padBottom / 300).toBe(200);
  });

  it("renders a first batch before it can measure, and nothing for no items", () => {
    expect(gridWindow(5000, 0, 0, 0, 0, 0)).toEqual({ start: 0, end: 120, padTop: 0, padBottom: 0 });
    expect(gridWindow(0, 5, 300, 0, 0, 900).end).toBe(0);
  });

  it("never renders an empty window when scrolled past the end", () => {
    const w = gridWindow(12, 4, 300, 0, 99_999, 900);
    expect(w.end - w.start).toBeGreaterThan(0);
  });
});

describe("library address", () => {
  it("keeps known filters and drops the rest", () => {
    expect(validateLibrarySearch({ tab: "genres", sort: "rating", genre: "Drama", status: "unplayed", decade: "1990", letter: "B" }))
      .toEqual({ tab: "genres", sort: "rating", genre: "Drama", status: "unplayed", decade: 1990, letter: "B" });
    expect(validateLibrarySearch({ tab: "all", sort: "added", status: "evil", decade: "1995", letter: "<", genre: "" })).toEqual({});
  });

  it("puts filters in the address and starts each library fresh", async () => {
    const asked: string[] = [];
    mockApi(({ path, query }) => {
      if (path === "/libraries") return { status: 200, body: [libA, libB] };
      if (path === `/libraries/${libA.id}/genres`) return { status: 200, body: ["Drama"] };
      if (path.endsWith("/items")) {
        asked.push(`${path}?${query.toString()}`);
        const lib = path.includes(libA.id) ? "A" : "B";
        return { status: 200, body: { items: [movie(`0190a8b0-0000-7000-8000-0000000000${lib === "A" ? "a" : "b"}1`, `${lib} title`)], total: 1 } };
      }
      return { status: 200, body: [] };
    });
    const { router } = renderApp(`/library/${libA.id}`, { me: testUser() });
    fireEvent.change(await screen.findByLabelText("Genre"), { target: { value: "Drama" } });
    await waitFor(() => expect(router.state.location.search).toMatchObject({ genre: "Drama" }));
    await waitFor(() => expect(asked.some((u) => u.includes("genre=Drama"))).toBe(true));
    fireEvent.click(screen.getByRole("button", { name: "B" }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ genre: "Drama", letter: "B" }));

    // Clearing resets every filter at once.
    fireEvent.click(screen.getByRole("button", { name: /Clear filters/ }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));

    // Another library: nothing of the first one carries over.
    await router.navigate({ to: "/library/$libraryId", params: { libraryId: libB.id } });
    expect(await screen.findByRole("heading", { level: 1, name: "Serije" })).toBeInTheDocument();
    expect((await screen.findAllByText("B title")).length).toBeGreaterThan(0);
    expect(screen.queryByText("A title")).not.toBeInTheDocument();
    expect(router.state.location.search).toEqual({});
  }, 20_000);

  it("moves between tabs with the arrow keys", async () => {
    mockApi(({ path }) => (path === "/libraries" ? { status: 200, body: [libA] }
      : path.endsWith("/items") ? { status: 200, body: { items: [], total: 0 } } : { status: 200, body: [] }));
    const { router } = renderApp(`/library/${libA.id}`, { me: testUser() });
    const all = await screen.findByRole("tab", { name: "All" });
    expect(all).toHaveAttribute("tabindex", "0");
    expect(screen.getByRole("tab", { name: "Genres" })).toHaveAttribute("tabindex", "-1");
    all.focus();
    fireEvent.keyDown(all, { key: "ArrowRight" });
    await waitFor(() => expect(router.state.location.search).toMatchObject({ tab: "suggested" }));
    expect(screen.getByRole("tab", { name: "Suggested" })).toHaveFocus();
  });
});

describe("rows after a change", () => {
  it("covers every row that shows watched, progress or favourites", () => {
    for (const key of ["item", "library-items", "favorites", "continue", "continue-reading", "next-up", "latest", "row", "books", "photos", "search"]) {
      expect(USER_DATA_KEYS).toContain(key);
    }
  });

  it("removing from Continue watching refreshes the other rows too", async () => {
    const started = movie("0190a8b0-0000-7000-8000-0000000000c1", "Half Seen", { positionMs: 60_000, durationMs: 600_000, hasBackdrop: false });
    let cleared = false;
    mockApi(({ method, path }) => {
      if (method === "PUT" && path.endsWith("/progress")) { cleared = true; return { status: 204 }; }
      if (path === "/home/continue") return { status: 200, body: cleared ? [] : [started] };
      if (path === "/home/latest") return { status: 200, body: [started] };
      if (path === "/enhancements") return { status: 200, body: { qualityTags: false, randomButton: false, pauseScreen: false, arrLinks: false,
        discoverInSearch: false, removeFromContinue: true, shortcutsHelp: false, autoPip: false } };
      return { status: 200, body: [] };
    });
    const { qc } = renderApp("/", { me: testUser() });
    fireEvent.click(await screen.findByRole("button", { name: /Remove .*Half Seen/ }));
    await waitFor(() => expect(cleared).toBe(true));
    // Not only Continue watching: the latest row (which shows the progress line) is stale too.
    await waitFor(() => expect(qc.getQueryState(["latest"])?.isInvalidated || (qc.getQueryState(["latest"])?.dataUpdateCount ?? 0) > 1).toBe(true));
    await waitFor(() => expect(screen.queryByTestId("continue")).not.toBeInTheDocument());
  });
});

describe("title page", () => {
  it("starts fresh on another title (no version or track choice carries over)", async () => {
    const files = (id: string) => [
      { id: `${id}-1`, path: "/a.mkv", size: 1, container: "mkv", width: 1920, height: 1080, videoCodec: "h264", audio: [{ codec: "aac", language: "eng" }, { codec: "ac3", language: "hrv" }] },
      { id: `${id}-2`, path: "/b.mkv", size: 1, container: "mkv", width: 3840, height: 2160, videoCodec: "hevc" },
    ];
    const one = movie("0190a8b0-0000-7000-8000-0000000000d1", "One");
    const two = movie("0190a8b0-0000-7000-8000-0000000000d2", "Two");
    mockApi(({ path }) => {
      for (const m of [one, two]) {
        if (path === `/items/${m.id}`) return { status: 200, body: { item: m, libraryId: libA.id, providerIds: {}, children: [], genres: [], cast: [], crew: [],
          hasBackdrop: false, files: files(m.id) } };
      }
      return { status: 200, body: [] };
    });
    const { router } = renderApp(`/item/${one.id}`, { me: testUser() });
    fireEvent.change(await screen.findByRole("combobox", { name: /Version/i }), { target: { value: "1" } });
    expect(screen.getByTestId("play").getAttribute("href")).toContain(`file=${one.id}-2`);
    await router.navigate({ to: "/item/$itemId", params: { itemId: two.id } });
    // The new title starts on its own first version, not on "the second one" of the previous title.
    await waitFor(() => expect(screen.getByTestId("play").getAttribute("href")).toContain(`file=${two.id}-1`));
  });
});
