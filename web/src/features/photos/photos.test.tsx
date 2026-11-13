import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const lib = "0190a8b0-0000-7000-8000-00000000bbbb";
const now = new Date().toISOString();
const album = { id: "0190a8b0-0000-7000-8000-0000000000a1", kind: "photoalbum", title: "Summer", childCount: 2, addedAt: now, hasPoster: false };
const photos = [1, 2].map((n) => ({ id: `0190a8b0-0000-7000-8000-00000000000${n}`, kind: "photo", title: `IMG_${n}`, addedAt: now,
  hasPoster: true, takenAt: `2023-07-1${n}T10:00:00Z`, width: 800, height: 600 }));

describe("photos", () => {
  beforeEach(() => setLanguage("en"));

  it("shows albums, opens a picture in the lightbox and runs a slideshow", async () => {
    const viewed: string[] = [];
    const favs: unknown[] = [];
    mockApi(({ method, path, query, body }) => {
      if (path === "/libraries") return { status: 200, body: [{ id: lib, name: "Photos", kind: "photos", paths: ["/m"], itemCount: 2, scan: { state: "idle", filesSeen: 2 } }] };
      if (path === `/libraries/${lib}/items`) {
        if (query.get("parentId") === album.id) return { status: 200, body: { items: photos, total: 2 } };
        return { status: 200, body: { items: [album], total: 1 } };
      }
      if (path === `/items/${album.id}`) return { status: 200, body: { item: album, libraryId: lib, providerIds: {}, children: [], files: [], genres: [], cast: [], crew: [], hasBackdrop: false } };
      const m = path.match(/^\/items\/([^/]+)\/photo$/);
      if (m) { viewed.push(m[1]!); return { status: 200, body: { url: `/stream/file/${m[1]}/file.jpg?sig=x`, mimeType: "image/jpeg", width: 800, height: 600 } }; }
      if (method === "PUT" && path.endsWith("/user-data")) { favs.push(body); return { status: 204 }; }
      return { status: 200, body: [] };
    });
    renderApp(`/library/${lib}`, { me: testUser() });
    fireEvent.click(await screen.findByTestId("photo-album"));
    expect(await screen.findByTestId("photo-album-page")).toHaveTextContent("Summer");
    const thumbs = await screen.findAllByTestId("photo-thumb");
    expect(screen.getByTestId("photo-grid")).toHaveTextContent("July 2023");
    fireEvent.click(thumbs[1]!);
    expect(await screen.findByTestId("lightbox-image")).toHaveAttribute("src", `/stream/file/${photos[1]!.id}/file.jpg?sig=x`);
    fireEvent.keyDown(window, { key: "ArrowRight" });
    await waitFor(() => expect(viewed).toContain(photos[0]!.id));
    fireEvent.click(screen.getByRole("button", { name: "Favourite" }));
    await waitFor(() => expect(favs).toEqual([{ favorite: true }]));
    fireEvent.change(screen.getByTestId("slideshow-interval"), { target: { value: "3" } });
    expect((screen.getByTestId("slideshow-interval") as HTMLSelectElement).value).toBe("3");
    fireEvent.keyDown(window, { key: "Escape" });
    await waitFor(() => expect(screen.queryByTestId("lightbox")).toBeNull());
  });
});
