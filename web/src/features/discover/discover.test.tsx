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
