import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const lib = "0190a8b0-0000-7000-8000-00000000aaaa";
const now = new Date().toISOString();
const comic = { id: "0190a8b0-0000-7000-8000-0000000000c1", kind: "book", title: "Space Cats 003", authors: ["Mia Writer"],
  seriesName: "Space Cats", seriesIndex: 3, bookFormat: "cbz", addedAt: now, hasPoster: false, progression: 0.5 };
const book = { id: "0190a8b0-0000-7000-8000-0000000000e1", kind: "audiobook", title: "The Sound Book", authors: ["Lea Author"],
  addedAt: now, hasPoster: false, durationMs: 6000 };

function detail(item: object, extra: object = {}) {
  return { item, libraryId: lib, providerIds: {}, children: [], files: [], genres: [], cast: [], crew: [], hasBackdrop: false, ...extra };
}

describe("books", () => {
  beforeEach(() => {
    setLanguage("en");
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
  });

  it("lists a book library with authors and opens an author's titles", async () => {
    const asked: string[] = [];
    mockApi(({ path, query }) => {
      if (path === "/libraries") return { status: 200, body: [{ id: lib, name: "Comics", kind: "books", paths: ["/m"], itemCount: 1, scan: { state: "idle", filesSeen: 1 } }] };
      if (path === `/libraries/${lib}/authors`) return { status: 200, body: [{ name: "Mia Writer", count: 1, itemId: comic.id }] };
      if (path === `/libraries/${lib}/items`) {
        asked.push(query.toString());
        return { status: 200, body: { items: [comic], total: 1 } };
      }
      return { status: 200, body: [] };
    });
    renderApp(`/library/${lib}`, { me: testUser() });
    expect(await screen.findByTestId("books-library")).toHaveTextContent("1 book");
    expect(await screen.findByTestId("book-card")).toHaveTextContent("Space Cats 003");
    fireEvent.click(screen.getByRole("tab", { name: "Authors" }));
    fireEvent.click(await screen.findByRole("button", { name: /Mia Writer/ }));
    await waitFor(() => expect(asked.some((q) => q.includes("author=Mia+Writer"))).toBe(true));
  });

  it("reads a manga right to left, resumes and saves the page", async () => {
    const saved: { page?: number; progression: number }[] = [];
    mockApi(({ method, path, body }) => {
      if (path === `/items/${comic.id}`) return { status: 200, body: detail(comic, { book: { format: "cbz", authors: ["Mia Writer"], pageCount: 4, rightToLeft: true } }) };
      if (path === `/items/${comic.id}/read`) {
        return { status: 200, body: { format: "cbz", fileUrl: "/stream/file/f/file.cbz?sig=x", pageCount: 4, rightToLeft: true,
          pages: [0, 1, 2, 3].map((i) => `/stream/page/f/${i}?sig=x`), progress: { page: 1, progression: 0.5, finished: false } } };
      }
      if (method === "PUT" && path === `/items/${comic.id}/reading-progress`) { saved.push(body as { page: number; progression: number }); return { status: 204 }; }
      return { status: 200, body: [] };
    });
    renderApp(`/item/${comic.id}`, { me: testUser() });
    fireEvent.click(await screen.findByTestId("read"));
    expect(await screen.findByTestId("comic-position")).toHaveTextContent("Page 2 of 4");
    expect(screen.getByTestId("comic-rtl")).toHaveTextContent("RTL");
    fireEvent.keyDown(window, { key: "ArrowLeft" }); // manga: left is forward
    expect(screen.getByTestId("comic-position")).toHaveTextContent("Page 3 of 4");
    fireEvent.click(screen.getByTestId("comic-double"));
    expect(screen.getAllByTestId("comic-page")).toHaveLength(2);
    expect(screen.getByTestId("comic-position")).toHaveTextContent("Page 2–3 of 4");
    await waitFor(() => expect(saved.at(-1)).toMatchObject({ page: 2, progression: 0.75 }), { timeout: 4000 });
    fireEvent.keyDown(window, { key: "End" });
    expect(screen.getByTestId("comic-position")).toHaveTextContent("Page 4 of 4");
    await waitFor(() => expect(saved.at(-1)).toMatchObject({ page: 3, progression: 1, finished: true }), { timeout: 4000 });
  });

  it("plays an audiobook part from a chapter and changes the speed", async () => {
    const plays: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === `/items/${book.id}`) return { status: 200, body: detail(book, { book: { format: "folder", authors: ["Lea Author"], narrators: ["Nina"] } }) };
      if (path === `/items/${book.id}/listen`) {
        return { status: 200, body: { durationMs: 6000, positionMs: 0,
          parts: [{ fileId: "0190a8b0-0000-7000-8000-0000000000f1", title: "First", offsetMs: 0, durationMs: 3000 },
            { fileId: "0190a8b0-0000-7000-8000-0000000000f2", title: "Second", offsetMs: 3000, durationMs: 3000 }],
          chapters: [{ title: "First", startMs: 0, endMs: 3000 }, { title: "Second", startMs: 3000, endMs: 6000 }] } };
      }
      if (method === "POST" && path === `/items/${book.id}/playback`) {
        plays.push(body);
        return { status: 200, body: { method: "direct", url: "/stream/file/x/video.mp3", fileId: "0190a8b0-0000-7000-8000-0000000000f2", startMs: 0, audio: [], subtitles: [], durationMs: 3000 } };
      }
      return { status: 200, body: [] };
    });
    renderApp(`/item/${book.id}`, { me: testUser() });
    expect(await screen.findByTestId("book-page")).toHaveTextContent("Read by Nina");
    const list = await screen.findByTestId("chapter-list");
    fireEvent.click(list.querySelectorAll("button")[1]!);
    await waitFor(() => expect(plays).toHaveLength(1));
    expect(plays[0]).toMatchObject({ fileId: "0190a8b0-0000-7000-8000-0000000000f2" });
    expect(await screen.findByTestId("now-playing-bar")).toHaveTextContent("Second");
    fireEvent.change(screen.getByTestId("speed"), { target: { value: "1.5" } });
    expect((screen.getByTestId("speed") as HTMLSelectElement).value).toBe("1.5");
  });
});
