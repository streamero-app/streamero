import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { subtitleCss } from "@/features/player/PlayerPage";

afterEach(() => vi.unstubAllGlobals());

describe("profile preferences", () => {
  beforeEach(() => setLanguage("en"));

  it("saves controls and quality limits with the rest of the preferences", async () => {
    const puts: unknown[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/me/preferences" && method === "PUT") { puts.push(body); return { status: 200, body }; }
      if (path === "/me/preferences") return { status: 200, body: { subtitleMode: "smart", autoplayNext: true, audioLanguage: "hr" } };
      return { status: 200, body: [] };
    });
    renderApp("/settings/playback", { me: testUser() });
    fireEvent.change(await screen.findByLabelText("Skip forward"), { target: { value: "30" } });
    fireEvent.change(screen.getByLabelText("Intros and recaps"), { target: { value: "auto" } });
    fireEvent.change(screen.getByLabelText("Bitrate limit"), { target: { value: "4000" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(puts[0]).toMatchObject({ subtitleMode: "smart", audioLanguage: "hr", skipForwardSeconds: 30, skipIntro: "auto", maxBitrateKbps: 4000 }));
  });

  it("orders and hides home rows", async () => {
    const puts: { homeSections?: string[] }[] = [];
    mockApi(({ method, path, body }) => {
      if (path === "/me/preferences" && method === "PUT") { puts.push(body as never); return { status: 200, body }; }
      if (path === "/me/preferences") return { status: 200, body: { subtitleMode: "default", autoplayNext: true } };
      return { status: 200, body: [] };
    });
    renderApp("/settings/home", { me: testUser() });
    fireEvent.click(await screen.findByRole("switch", { name: "Collections" }));
    fireEvent.click(screen.getAllByRole("button", { name: "Move up" })[1]!);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(puts[0]?.homeSections?.slice(0, 2)).toEqual(["nextUp", "continue"]));
    expect(puts[0]?.homeSections).not.toContain("collections");
  });

  it("turns the subtitle look into cue styles", () => {
    const css = subtitleCss({ subtitleSize: "large", subtitleColor: "yellow", subtitleBackground: "box", subtitlePosition: 10 });
    expect(css).toContain("font-size:4.4vh");
    expect(css).toContain("color:#ffe14d");
    expect(css).toContain("background-color:rgb(0 0 0 / 0.75)");
    expect(css).toContain("translateY(-10vh)");
  });
});
