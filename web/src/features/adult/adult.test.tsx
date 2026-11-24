import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { sceneProgress } from "./api";

afterEach(() => vi.unstubAllGlobals());

const adultLib = { id: "0190a8b0-0000-7000-8000-00000000cccc", name: "Privatno", kind: "adult", paths: ["/media/adult"], itemCount: 0,
  scan: { state: "idle", filesSeen: 1 } };
const movies = { id: "0190a8b0-0000-7000-8000-00000000aaaa", name: "Filmovi", kind: "movies", paths: ["/media/movies"], itemCount: 1,
  scan: { state: "idle", filesSeen: 1 } };
const scene = { id: "0190a8b0-0000-7000-8000-00000000dddd", libraryId: adultLib.id, kind: "scene", title: "Moonlit", date: "2024-01-15",
  studio: { id: "0190a8b0-0000-7000-8000-00000000eeee", name: "Velvetine" }, performers: [], tags: [], hasPoster: true, hasBackdrop: false,
  positionMs: 0, played: false, addedAt: new Date().toISOString() };
const status = { pinSet: false, unlocked: true, blur: true, metadataConfigured: true, libraries: [{ id: adultLib.id, name: "Privatno", sceneCount: 1 }] };

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
    const { router } = renderApp("/adult", { me: testUser({ adultAccess: true }) });
    expect((await screen.findAllByTestId("adult-scene-card"))[0]).toHaveTextContent("Moonlit");
    const before = (await screen.findAllByTestId("adult-reveal")).length;
    fireEvent.click(screen.getAllByTestId("adult-reveal")[0]!);
    await waitFor(() => expect(screen.queryAllByTestId("adult-reveal")).toHaveLength(before - 1));
    // Revealing does not open the scene.
    expect(router.state.location.pathname).toBe("/adult");
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
    renderApp("/adult", { me: testUser({ adultAccess: true }) });
    fireEvent.change(await screen.findByTestId("adult-pin"), { target: { value: "4321" } });
    fireEvent.click(screen.getByRole("button", { name: "Unlock" }));
    expect((await screen.findAllByTestId("adult-scene-card")).length).toBeGreaterThan(0);
  });

  it("computes progress only for started, unwatched scenes", () => {
    expect(sceneProgress({ positionMs: 30_000, durationMs: 60_000, played: false })).toBe(50);
    expect(sceneProgress({ positionMs: 30_000, durationMs: 60_000, played: true })).toBe(0);
    expect(sceneProgress({ positionMs: 0, played: false })).toBe(0);
  });
});
