import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { clampZoom, sceneProgress, stepIndex } from "./api";

afterEach(() => vi.unstubAllGlobals());

const adultLib = { id: "0190a8b0-0000-7000-8000-00000000cccc", name: "Privatno", kind: "adult", paths: ["/media/adult"], itemCount: 0,
  scan: { state: "idle", filesSeen: 1 } };
const movies = { id: "0190a8b0-0000-7000-8000-00000000aaaa", name: "Filmovi", kind: "movies", paths: ["/media/movies"], itemCount: 1,
  scan: { state: "idle", filesSeen: 1 } };
const scene = { id: "0190a8b0-0000-7000-8000-00000000dddd", libraryId: adultLib.id, kind: "scene", title: "Moonlit", date: "2024-01-15",
  studio: { id: "0190a8b0-0000-7000-8000-00000000eeee", name: "Velvetine" }, performers: [], tags: [], hasPoster: true, hasBackdrop: false,
  positionMs: 0, played: false, addedAt: new Date().toISOString() };
const status = { pinSet: false, unlocked: true, blur: true, metadataConfigured: true, hiddenTags: [] as string[], hideTagged: false, canDownload: false,
  libraries: [{ id: adultLib.id, name: "Privatno", sceneCount: 1, galleryCount: 1 }] };
const gallery = { id: "0190a8b0-0000-7000-8000-00000000ffff", libraryId: adultLib.id, title: "Gilded Hour", date: "2024-03-01",
  studio: scene.studio, performers: [{ id: "0190a8b0-0000-7000-8000-000000000001", name: "Ana Bee" }], tags: ["Outdoor"], imageCount: 3,
  coverImageId: "0190a8b0-0000-7000-8000-000000000101", addedAt: new Date().toISOString() };
const galleryDetail = { ...gallery, images: [1, 2, 3].map((n) => ({ id: `0190a8b0-0000-7000-8000-00000000010${n}`, position: n - 1, name: `${n}.jpg`, width: 800, height: 600 })) };

describe("adult section", () => {
  beforeEach(() => setLanguage("en"));

  it("is not offered to an account without adult access, and adult libraries are never browsed", async () => {
    mockApi(({ path }) => (path === "/libraries" ? { status: 200, body: [movies, adultLib] } : { status: 200, body: [] }));
    const { router } = renderApp("/adult", { me: testUser({ isAdmin: true }) });
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(screen.queryByTestId("adult-link")).not.toBeInTheDocument();
    await screen.findAllByText("Filmovi");
    expect(screen.queryByText("Privatno")).not.toBeInTheDocument();
  });

  it("shows scenes with blurred pictures that a click reveals", async () => {
    mockApi(({ path }) => {
      if (path === "/adult/status") return { status: 200, body: status };
      if (path === "/adult/scenes") return { status: 200, body: { items: [scene], total: 1 } };
      return { status: 200, body: [] };
    });
    const { router } = renderApp("/adult/scenes", { me: testUser({ adultAccess: true }) });
    expect((await screen.findAllByTestId("adult-scene-card"))[0]).toHaveTextContent("Moonlit");
    const before = (await screen.findAllByTestId("adult-reveal")).length;
    fireEvent.click(screen.getAllByTestId("adult-reveal")[0]!);
    await waitFor(() => expect(screen.queryAllByTestId("adult-reveal")).toHaveLength(before - 1));
    // Revealing does not open the scene.
    expect(router.state.location.pathname).toBe("/adult/scenes");
  });

  it("asks for the PIN while the section is locked", async () => {
    let unlocked = false;
    mockApi(({ method, path }) => {
      if (path === "/adult/status") return { status: 200, body: { ...status, pinSet: true, unlocked } };
      if (method === "POST" && path === "/adult/unlock") {
        unlocked = true;
        return { status: 204 };
      }
      if (path === "/adult/scenes") return { status: 200, body: { items: [scene], total: 1 } };
      return { status: 200, body: [] };
    });
    renderApp("/adult/scenes", { me: testUser({ adultAccess: true }) });
    fireEvent.change(await screen.findByTestId("adult-pin"), { target: { value: "4321" } });
    fireEvent.click(screen.getByRole("button", { name: "Unlock" }));
    expect((await screen.findAllByTestId("adult-scene-card")).length).toBeGreaterThan(0);
  });

  it("has its own navigation and a home page with adult rows only", async () => {
    const seen: string[] = [];
    mockApi(({ path }) => {
      seen.push(path);
      if (path === "/adult/status") return { status: 200, body: status };
      if (path === "/adult/home") {
        return { status: 200, body: { continue: [], recentlyAdded: [scene], recentlyReleased: [scene], favoritePerformers: [], recentGalleries: [gallery] } };
      }
      return { status: 200, body: [] };
    });
    renderApp("/adult", { me: testUser({ adultAccess: true }) });
    const nav = await screen.findByTestId("adult-nav");
    for (const name of ["Home", "Scenes", "Movies", "Photos", "Performers", "Studios", "Tags"]) {
      expect(within(nav).getByRole("link", { name })).toBeInTheDocument();
    }
    expect(await screen.findByText("Recently added")).toBeInTheDocument();
    expect(within(screen.getByTestId("adult-home-galleries")).getByTestId("adult-gallery-card")).toHaveTextContent("Gilded Hour");
    // The regular home rows are never asked for here.
    expect(seen.some((p) => p.startsWith("/home"))).toBe(false);
  });

  it("opens a gallery in the viewer and steps through it with the keyboard", async () => {
    mockApi(({ path }) => {
      if (path === "/adult/status") return { status: 200, body: status };
      if (path === `/adult/galleries/${gallery.id}`) return { status: 200, body: galleryDetail };
      return { status: 200, body: [] };
    });
    const { router } = renderApp(`/adult/gallery/${gallery.id}`, { me: testUser({ adultAccess: true }) });
    expect(await screen.findByRole("heading", { name: "Gilded Hour" })).toBeInTheDocument();
    expect(screen.getByTestId("adult-breadcrumbs")).toHaveTextContent("Photos");
    expect(screen.getAllByTestId("adult-gallery-thumb")).toHaveLength(3);
    fireEvent.click(screen.getByTestId("adult-gallery-open"));
    const box = await screen.findByTestId("adult-lightbox");
    expect(screen.getByTestId("adult-lightbox-counter")).toHaveTextContent("1 / 3");
    expect(screen.getByTestId("adult-lightbox-image").getAttribute("src")).toContain("/adult/gallery-images/");
    // No download permission: no download button.
    expect(screen.queryByTestId("adult-download")).not.toBeInTheDocument();
    fireEvent.keyDown(box, { key: "ArrowLeft" });
    await waitFor(() => expect(screen.getByTestId("adult-lightbox-counter")).toHaveTextContent("3 / 3"));
    fireEvent.keyDown(screen.getByTestId("adult-lightbox"), { key: "ArrowRight" });
    await waitFor(() => expect(screen.getByTestId("adult-lightbox-counter")).toHaveTextContent("1 / 3"));
    expect(router.state.location.search).toMatchObject({ i: 0 });
    fireEvent.keyDown(screen.getByTestId("adult-lightbox"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByTestId("adult-lightbox")).not.toBeInTheDocument());
  });

  it("offers the download only with the download permission", async () => {
    mockApi(({ path }) => {
      if (path === "/adult/status") return { status: 200, body: { ...status, canDownload: true } };
      if (path === `/adult/galleries/${gallery.id}`) return { status: 200, body: galleryDetail };
      return { status: 200, body: [] };
    });
    renderApp(`/adult/gallery/${gallery.id}?i=1`, { me: testUser({ adultAccess: true }) });
    expect((await screen.findByTestId("adult-download")).getAttribute("href")).toBe(`/api/v1/adult/gallery-images/${galleryDetail.images[1]!.id}/download`);
  });

  it("hides a tag the viewer does not want to see", async () => {
    let saved: unknown;
    mockApi(({ method, path, body }) => {
      if (path === "/adult/status") return { status: 200, body: status };
      if (path === "/adult/tags") return { status: 200, body: { items: [{ name: "Outdoor", sceneCount: 2, galleryCount: 1, hidden: false }], total: 1 } };
      if (method === "PUT" && path === "/adult/preferences") {
        saved = body;
        return { status: 200, body: { ...status, hiddenTags: ["Outdoor"] } };
      }
      return { status: 200, body: [] };
    });
    renderApp("/adult/tags", { me: testUser({ adultAccess: true }) });
    expect(await screen.findByTestId("adult-tag")).toHaveTextContent("Outdoor");
    fireEvent.click(screen.getByTestId("adult-tag-toggle"));
    await waitFor(() => expect(saved).toEqual({ hiddenTags: ["Outdoor"] }));
  });

  it("steps and zooms within bounds", () => {
    expect(stepIndex(0, -1, 3)).toBe(2);
    expect(stepIndex(2, 1, 3)).toBe(0);
    expect(stepIndex(0, 1, 0)).toBe(0);
    expect(clampZoom(10)).toBe(4);
    expect(clampZoom(0.2)).toBe(1);
  });

  it("computes progress only for started, unwatched scenes", () => {
    expect(sceneProgress({ positionMs: 30_000, durationMs: 60_000, played: false })).toBe(50);
    expect(sceneProgress({ positionMs: 30_000, durationMs: 60_000, played: true })).toBe(0);
    expect(sceneProgress({ positionMs: 0, played: false })).toBe(0);
  });
});
