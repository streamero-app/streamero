import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const a = { id: "0190a8b0-0000-7000-8000-0000000000a1", kind: "movie", title: "Alpha", year: 2001, addedAt: new Date().toISOString(), hasPoster: false };
const b = { ...a, id: "0190a8b0-0000-7000-8000-0000000000b2", title: "Beta", year: 2002 };
const pl = { id: "0190a8b0-0000-7000-8000-0000000000c3", name: "Vikend", itemCount: 2, durationMs: 0, public: false, mine: true, coverItemIds: [] };

describe("playlists and collections", () => {
  beforeEach(() => setLanguage("en"));

  it("reorders and removes playlist items and plays all in order", async () => {
    const puts: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "PUT") {
        puts.push(body);
        return { status: 200, body: pl };
      }
      if (path === `/playlists/${pl.id}`) return { status: 200, body: { playlist: pl, items: [a, b] } };
      return { status: 200, body: [] };
    });
    renderApp(`/playlist/${pl.id}`, { me: testUser() });
    expect(await screen.findAllByTestId("playlist-row")).toHaveLength(2);
    fireEvent.click(screen.getAllByRole("button", { name: "Move down" })[0]!);
    await waitFor(() => expect(puts).toContainEqual({ name: "Vikend", public: false, itemIds: [b.id, a.id] }));
    fireEvent.click(screen.getAllByRole("button", { name: "Remove from playlist" })[1]!);
    await waitFor(() => expect(puts).toContainEqual({ name: "Vikend", public: false, itemIds: [a.id] }));
    expect(screen.getByTestId("play-all")).toHaveAttribute("href", `/watch/${a.id}?playlist=${pl.id}`);
  });

  it("adds a title to a playlist from its page", async () => {
    const posts: { path: string; body: unknown }[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "POST") {
        posts.push({ path, body });
        return { status: 200, body: pl };
      }
      if (path === "/playlists") return { status: 200, body: [pl] };
      if (path === `/items/${a.id}`) {
        return { status: 200, body: { item: a, libraryId: "l", providerIds: {}, files: [], children: [], genres: [], cast: [], crew: [], hasBackdrop: false,
          collections: [{ id: "c1", name: "Saga", itemCount: 2, hasPoster: false, hasBackdrop: false, auto: true }] } };
      }
      return { status: 200, body: [] };
    });
    renderApp(`/item/${a.id}`, { me: testUser() });
    expect(await screen.findByRole("link", { name: "Saga" })).toHaveAttribute("href", "/collection/c1");
    fireEvent.click(screen.getByRole("button", { name: "Add to playlist" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Vikend" }));
    await waitFor(() => expect(posts).toContainEqual({ path: `/playlists/${pl.id}/items`, body: { itemIds: [a.id] } }));
    expect(await screen.findByText("Added to Vikend")).toBeInTheDocument();
  });
});
