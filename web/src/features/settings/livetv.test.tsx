import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());
beforeEach(() => setLanguage("en"));

const settings = {
  prePaddingSeconds: 120, postPaddingSeconds: 300, timeshiftMinutes: 30,
  retention: { enabled: false, deleteAfterDays: 0, keepLast: 0 },
  postProcess: { command: "", args: [], timeoutMinutes: 60 },
  recordingsPath: "/recordings",
};

function mock(log: { method: string; path: string; body: unknown }[]) {
  return mockApi(({ method, path, body }) => {
    if (method !== "GET") log.push({ method, path, body });
    if (path === "/livetv/tuners/test") return { status: 200, body: { ok: true, message: "Found 42 channels.", channelCount: 42 } };
    if (path === "/livetv/settings" && method === "PUT") return { status: 200, body: { ...settings, ...(body as object) } };
    if (path === "/livetv/settings") return { status: 200, body: settings };
    if (path === "/livetv/mapping") return { status: 200, body: { channels: [], guideChannels: [] } };
    return { status: 200, body: [] };
  });
}

describe("Live TV settings", () => {
  it("tests an M3U playlist before saving it", async () => {
    const log: { method: string; path: string; body: unknown }[] = [];
    mock(log);
    renderApp("/settings/livetv", { me: testUser({ isAdmin: true }) });
    await screen.findByTestId("livetv-settings");
    fireEvent.click(screen.getByRole("button", { name: "Add tuner" }));
    const form = await screen.findByTestId("tuner-form");
    const test = within(form).getByRole("button", { name: "Test" });
    expect(test).toBeDisabled();
    fireEvent.change(within(form).getByLabelText("Name"), { target: { value: "IPTV" } });
    fireEvent.change(within(form).getByLabelText("Playlist URL"), { target: { value: "http://iptv.example/list.m3u" } });
    fireEvent.change(within(form).getByLabelText("Tuners"), { target: { value: "2" } });
    fireEvent.click(test);
    expect(await within(form).findByTestId("test-result")).toHaveTextContent("Found 42 channels.");
    expect(log).toEqual([{ method: "POST", path: "/livetv/tuners/test",
      body: { kind: "m3u", name: "IPTV", enabled: true, url: "http://iptv.example/list.m3u", tunerCount: 2 } }]);
  });

  it("lets only administrators set the post-processing command", async () => {
    const log: { method: string; path: string; body: unknown }[] = [];
    mock(log);
    renderApp("/settings/livetv", { me: testUser({ permissions: { ...testUser().permissions, manageLiveTv: true } }) });
    const dvr = await screen.findByTestId("dvr-settings");
    expect(within(dvr).getByTestId("post-process-readonly")).toHaveTextContent("Only administrators can change it.");
    expect(within(dvr).queryByLabelText("Program (absolute path)")).not.toBeInTheDocument();
    expect(dvr).toHaveTextContent("Only deletes files the DVR recorded itself");
  });

  it("saves the post-processing arguments one per line", async () => {
    const log: { method: string; path: string; body: unknown }[] = [];
    mock(log);
    renderApp("/settings/livetv", { me: testUser({ isAdmin: true }) });
    const dvr = await screen.findByTestId("dvr-settings");
    fireEvent.change(await within(dvr).findByLabelText("Program (absolute path)"), { target: { value: "/config/scripts/comskip" } });
    fireEvent.change(within(dvr).getByLabelText(/Arguments/), { target: { value: "--ini\n{path}\n" } });
    fireEvent.click(within(dvr).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(log).toHaveLength(1));
    expect(log[0]!.body).toEqual({ prePaddingSeconds: 120, postPaddingSeconds: 300, timeshiftMinutes: 30,
      retention: { enabled: false, deleteAfterDays: 0, keepLast: 0 },
      postProcess: { command: "/config/scripts/comskip", args: ["--ini", "{path}"], timeoutMinutes: 60 } });
  });

  it("is hidden from users who may not manage Live TV", async () => {
    mock([]);
    const { router } = renderApp("/settings/livetv", { me: testUser() });
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/account"));
  });
});
