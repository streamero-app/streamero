import { screen } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { qualityTags } from "./useEnhancements";

afterEach(() => vi.unstubAllGlobals());

const lib = { id: "0190a8b0-0000-7000-8000-00000000aaaa", name: "Movies", kind: "movies", paths: ["/m"], itemCount: 1, scan: { state: "idle", filesSeen: 1 } };
const movie = { id: "0190a8b0-0000-7000-8000-00000000bbbb", kind: "movie", title: "Sharp", year: 2024, addedAt: new Date().toISOString(), hasPoster: false,
  videoHeight: 2160, hdr: "Dolby Vision" };
const all = { qualityTags: true, randomButton: true, pauseScreen: true, arrLinks: true, discoverInSearch: true, removeFromContinue: true, shortcutsHelp: true, autoPip: false };

describe("enhancements", () => {
  beforeEach(() => setLanguage("en"));

  it("labels 4K and Dolby Vision on posters and offers a surprise", async () => {
    mockApi(({ path }) => {
      if (path === "/enhancements") return { status: 200, body: all };
      if (path === "/libraries") return { status: 200, body: [lib] };
      if (path === `/libraries/${lib.id}/items`) return { status: 200, body: { items: [movie], total: 1 } };
      return { status: 200, body: [] };
    });
    renderApp(`/library/${lib.id}`, { me: testUser() });
    expect(await screen.findByTestId("quality-tags")).toHaveTextContent("4KDV");
    expect(screen.getAllByRole("button", { name: /Surprise me/ }).length).toBeGreaterThan(0);
  });

  it("hides them when switched off", async () => {
    mockApi(({ path }) => {
      if (path === "/enhancements") return { status: 200, body: { ...all, qualityTags: false, randomButton: false } };
      if (path === "/libraries") return { status: 200, body: [lib] };
      if (path === `/libraries/${lib.id}/items`) return { status: 200, body: { items: [movie], total: 1 } };
      return { status: 200, body: [] };
    });
    renderApp(`/library/${lib.id}`, { me: testUser() });
    await screen.findAllByTestId("item-card");
    expect(screen.queryByTestId("quality-tags")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Surprise me/ })).not.toBeInTheDocument();
  });

  it("maps formats to short tags", () => {
    expect(qualityTags(1080, "HDR10")).toEqual(["HD", "HDR"]);
    expect(qualityTags(2160, "HLG")).toEqual(["4K", "HLG"]);
    expect(qualityTags(480)).toEqual([]);
  });
});
