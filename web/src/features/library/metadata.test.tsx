import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const movie = { id: "0190a8b0-0000-7000-8000-0000000000d4", kind: "movie", title: "Wrong", year: 1999, addedAt: new Date().toISOString(), hasPoster: false };
const detail = { item: movie, libraryId: "l", providerIds: {}, files: [], children: [], genres: ["Drama"], cast: [], crew: [], hasBackdrop: false, overview: "Old." };

describe("metadata editing", () => {
  beforeEach(() => setLanguage("en"));

  it("lets an admin edit and lock metadata", async () => {
    const puts: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "PUT") {
        puts.push(body);
        return { status: 204 };
      }
      if (path === `/items/${movie.id}`) return { status: 200, body: detail };
      return { status: 200, body: [] };
    });
    renderApp(`/item/${movie.id}`, { me: testUser({ isAdmin: true }) });
    fireEvent.click(await screen.findByRole("button", { name: "More" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Edit metadata" }));
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Right" } });
    fireEvent.change(screen.getByLabelText("Genres"), { target: { value: "Drama, Thriller ," } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(puts).toHaveLength(1));
    expect(puts[0]).toMatchObject({ title: "Right", year: 1999, genres: ["Drama", "Thriller"], overview: "Old.", locked: true });
  });

  it("identifies a title from TMDb results", async () => {
    const posts: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (method === "POST") {
        posts.push({ path, body });
        return { status: 204 };
      }
      if (path === `/items/${movie.id}/identify`) return { status: 200, body: [{ tmdbId: 77, title: "Right", year: 1999 }] };
      if (path === `/items/${movie.id}`) return { status: 200, body: detail };
      return { status: 200, body: [] };
    });
    renderApp(`/item/${movie.id}`, { me: testUser({ isAdmin: true }) });
    fireEvent.click(await screen.findByRole("button", { name: "More" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Identify" }));
    fireEvent.click(await screen.findByTestId("identify-result"));
    await waitFor(() => expect(posts).toContainEqual({ path: `/items/${movie.id}/identify`, body: { tmdbId: 77 } }));
  });

  it("hides the menu from regular users", async () => {
    mockApi(({ path }) => (path === `/items/${movie.id}` ? { status: 200, body: detail } : { status: 200, body: [] }));
    renderApp(`/item/${movie.id}`, { me: testUser() });
    await screen.findByTestId("item-hero");
    expect(screen.queryByRole("button", { name: "More" })).not.toBeInTheDocument();
  });
});
