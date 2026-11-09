import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

describe("discover", () => {
  beforeEach(() => setLanguage("en"));

  it("searches and requests a title that is not here yet", async () => {
    const posts: unknown[] = [];
    mockApi(({ method, path, body, query }) => {
      if (method === "POST" && path === "/discover/request") { posts.push(body); return { status: 200, body: { status: "processing" } }; }
      if (path === "/discover") {
        return { status: 200, body: { configured: true, canRequest: true, page: 1, totalPages: 1, titles: query.get("q") === "dune"
          ? [{ tmdbId: 438631, mediaType: "movie", title: "Dune", year: 2021, status: "none" },
             { tmdbId: 1, mediaType: "movie", title: "Here Already", status: "available", itemId: "0190a8b0-0000-7000-8000-00000000bbbb" }]
          : [] } };
      }
      return { status: 200, body: [] };
    });
    renderApp("/discover?q=dune", { me: testUser() });
    const cards = await screen.findAllByTestId("discover-card");
    expect(cards[1]).toHaveTextContent("Watch");
    fireEvent.click(screen.getByTestId("request"));
    await waitFor(() => expect(posts).toContainEqual({ tmdbId: 438631, mediaType: "movie" }));
    expect(await screen.findAllByText("Downloading")).not.toHaveLength(0);
  });

  it("explains when requests are not set up", async () => {
    mockApi(({ path }) => (path === "/discover"
      ? { status: 200, body: { configured: false, canRequest: false, page: 1, totalPages: 1, titles: [] } }
      : { status: 200, body: [] }));
    renderApp("/discover", { me: testUser({ isAdmin: true }) });
    expect(await screen.findByText("Requests are not set up on this server yet.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Connect Seerr" })).toBeInTheDocument();
  });
});

describe("subtitles", () => {
  beforeEach(() => setLanguage("en"));

  it("finds subtitles through Bazarr and downloads one", async () => {
    const posts: unknown[] = [];
    const movie = { id: "0190a8b0-0000-7000-8000-00000000dddd", kind: "movie", title: "Film", addedAt: new Date().toISOString(), hasPoster: false };
    mockApi(({ method, path, body }) => {
      if (path === `/items/${movie.id}`) {
        return { status: 200, body: { item: movie, libraryId: "l", providerIds: { tmdb: "1" }, children: [], genres: [], cast: [], crew: [], hasBackdrop: false,
          files: [{ id: "f", path: "/m/f.mkv", size: 1, container: "mkv" }] } };
      }
      if (path.endsWith("/subtitles/search")) {
        return { status: 200, body: { configured: true, offers: [{ provider: "opensubtitles", language: "hr", forced: false, hearingImpaired: false, score: 91, subtitle: "x", originalFormat: false, release: ["Film.1080p"] }] } };
      }
      if (method === "POST" && path.endsWith("/subtitles/download")) { posts.push(body); return { status: 204 }; }
      return { status: 200, body: [] };
    });
    renderApp(`/item/${movie.id}`, { me: testUser({ isAdmin: true }) });
    fireEvent.click(await screen.findByTestId("find-subtitles"));
    expect(await screen.findByTestId("subtitle-offers")).toHaveTextContent("Croatian");
    fireEvent.click(screen.getByRole("button", { name: "Download" }));
    await waitFor(() => expect(posts).toContainEqual(expect.objectContaining({ subtitle: "x", language: "hr" })));
  });
});
