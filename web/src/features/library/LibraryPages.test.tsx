import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const lib = { id: "0190a8b0-0000-7000-8000-00000000aaaa", name: "Filmovi", kind: "movies", paths: ["/media/movies"], itemCount: 2,
  scan: { state: "idle", filesSeen: 3 } };
const movie = { id: "0190a8b0-0000-7000-8000-00000000bbbb", kind: "movie", title: "Big Test Movie", year: 2021, addedAt: new Date().toISOString(), hasPoster: false };

describe("library pages", () => {
  beforeEach(() => setLanguage("en"));

  it("shows libraries on home and the grid of a library", async () => {
    mockApi(({ path }) => {
      if (path === "/libraries") return { status: 200, body: [lib] };
      if (path === `/libraries/${lib.id}/items`) return { status: 200, body: { items: [movie], total: 1 } };
      return { status: 200, body: [] };
    });
    const { router } = renderApp("/", { me: testUser() });
    fireEvent.click(await screen.findByTestId("library-card"));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/library/${lib.id}`));
    expect(await screen.findByTestId("item-card")).toHaveTextContent("Big Test Movie");
    expect(screen.getByText("1 item")).toBeInTheDocument();
  });

  it("shows versions with resolution, HDR, audio and subtitles", async () => {
    mockApi(({ path }) => path === `/items/${movie.id}`
      ? { status: 200, body: { item: { ...movie, hasPoster: true, rating: 7.4 }, libraryId: lib.id, providerIds: { tmdb: "1" }, children: [],
          genres: ["Drama"], cast: [{ name: "Ana Glumica", role: "Ivana" }], crew: [{ name: "Ivo", role: "Director" }], hasBackdrop: true,
          overview: "Opis filma.", officialRating: "12", files: [
          { id: "f1", path: "/media/movies/Big/a.mkv", size: 1024 ** 3, container: "mkv", width: 3840, height: 2160, videoCodec: "hevc",
            hdr: "HDR10", label: "2160p", durationMs: 5_400_000, audio: [{ codec: "eac3", language: "eng", channels: 6 }],
            subtitles: [{ codec: "srt", language: "hr", forced: true, external: true }] },
          { id: "f2", path: "/media/movies/Big/b.mkv", size: 2048, container: "mkv", width: 1920, height: 1080, videoCodec: "h264" }] } }
      : { status: 200, body: [] });
    renderApp(`/item/${movie.id}`, { me: testUser() });
    expect(await screen.findByText("2 versions")).toBeInTheDocument();
    const files = screen.getAllByTestId("media-file");
    expect(files[0]).toHaveTextContent("4K");
    expect(files[0]).toHaveTextContent("HDR10");
    expect(files[0]).toHaveTextContent("ENG EAC3 5.1");
    expect(files[0]).toHaveTextContent("HR forced external");
    expect(files[1]).toHaveTextContent("1080p");
    expect(screen.getByTestId("overview")).toHaveTextContent("Opis filma.");
    expect(screen.getByTestId("cast")).toHaveTextContent("Ana Glumica");
    expect(screen.getByTestId("item-hero")).toHaveTextContent("★ 7.4");
    expect(screen.getByText(/Directed by/).parentElement).toHaveTextContent("Ivo");
  });

  it("saves a TMDb key and explains a rejected one", async () => {
    let calls = 0;
    mockApi(({ method, body }) => {
      if (method === "PUT") {
        calls++;
        return calls === 1
          ? { status: 400, body: { code: "invalid_tmdb_key", message: "" } }
          : { status: 200, body: { tmdbConfigured: true, language: "hr-HR", country: "HR" } };
      }
      void body;
      return { status: 200, body: { tmdbConfigured: false, language: "hr-HR", country: "HR" } };
    });
    renderApp("/settings/metadata", { me: testUser({ isAdmin: true }) });
    fireEvent.change(await screen.findByLabelText("TMDb API key or Read Access Token"), { target: { value: "bad" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("TMDb rejected this key");
    fireEvent.change(screen.getByLabelText("TMDb API key or Read Access Token"), { target: { value: "good" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("TMDb connected")).toBeInTheDocument();
  });

  it("lets an admin add a library with the folder picker", async () => {
    let created: unknown;
    mockApi(({ method, path, body, query }) => {
      if (path === "/fs/directories") {
        return query.get("path") === "/media"
          ? { status: 200, body: { path: "/media", directories: ["/media/movies"] } }
          : { status: 200, body: { directories: ["/media"] } };
      }
      if (method === "POST" && path === "/libraries") {
        created = body;
        return { status: 201, body: { ...lib, scan: { state: "queued" } } };
      }
      if (path === "/libraries") return { status: 200, body: created ? [lib] : [] };
      return { status: 200, body: [] };
    });
    renderApp("/settings/libraries", { me: testUser({ isAdmin: true }) });
    fireEvent.click(await screen.findByRole("button", { name: "Add library" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Filmovi" } });
    fireEvent.click(screen.getByRole("button", { name: "Add folder" }));
    fireEvent.click(await screen.findByRole("button", { name: /\/media/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Use this folder" }));
    expect(await screen.findByTestId("library-folder")).toHaveTextContent("/media");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(created).toEqual({ name: "Filmovi", kind: "movies", paths: ["/media"] }));
    expect(await screen.findByTestId("library-row")).toHaveTextContent("Filmovi");
  });
});

describe("browse", () => {
  beforeEach(() => setLanguage("en"));

  it("searches from the header", async () => {
    mockApi(({ path, query }) => path === "/search"
      ? { status: 200, body: query.get("q") === "matirx" ? [{ ...movie, title: "The Matrix" }] : [] }
      : { status: 200, body: [] });
    const { router } = renderApp("/", { me: testUser() });
    const box = await screen.findByLabelText("Search");
    fireEvent.change(box, { target: { value: "matirx" } });
    fireEvent.submit(box.closest("form")!);
    await waitFor(() => expect(router.state.location.pathname).toBe("/search"));
    expect(await screen.findByTestId("item-card")).toHaveTextContent("The Matrix");
  });

  it("toggles favorite and filters by genre", async () => {
    const puts: unknown[] = [];
    const queries: string[] = [];
    mockApi(({ method, path, body, query }) => {
      if (method === "PUT") {
        puts.push(body);
        return { status: 204 };
      }
      if (path === `/items/${movie.id}`) {
        return { status: 200, body: { item: movie, libraryId: lib.id, providerIds: {}, children: [], files: [], genres: [], cast: [], crew: [], hasBackdrop: false } };
      }
      if (path === "/libraries") return { status: 200, body: [lib] };
      if (path.endsWith("/genres")) return { status: 200, body: ["Drama", "Komedija"] };
      if (path.endsWith("/items")) {
        queries.push(query.toString());
        return { status: 200, body: { items: [movie], total: 1 } };
      }
      return { status: 200, body: [] };
    });
    renderApp(`/item/${movie.id}`, { me: testUser() });
    fireEvent.click(await screen.findByTestId("favorite"));
    await waitFor(() => expect(puts).toContainEqual({ favorite: true }));

    renderApp(`/library/${lib.id}`, { me: testUser() });
    fireEvent.change(await screen.findByLabelText("Genre"), { target: { value: "Drama" } });
    fireEvent.click(screen.getByLabelText("Unwatched only"));
    await waitFor(() => expect(queries.some((q) => q.includes("genre=Drama") && q.includes("unplayed=true"))).toBe(true));
  });
});
