import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const userId = "0190a8b0-0000-7000-8000-00000000aaaa";
const kidId = "0190a8b0-0000-7000-8000-00000000cccc";

const overview = {
  settings: { enabled: false, friendlyName: "", language: "en" },
  status: { available: true, running: false, httpAddr: ":8788", ssdp: true, interfaces: ["eth0"] },
  users: [{ id: userId, username: "ana", profiles: [{ id: kidId, name: "Lea", isKid: true }] }],
};

function mock(bodies: unknown[]) {
  return mockApi(({ method, path, body }) => {
    if (path === "/settings/dlna" && method === "PUT") {
      bodies.push(body);
      return { status: 200, body: { ...overview, settings: body, status: { ...overview.status, running: true } } };
    }
    if (path === "/settings/dlna") return { status: 200, body: overview };
    if (path === "/settings/server") return { status: 200, body: { serverName: "Kino" } };
    return { status: 200, body: [] };
  });
}

describe("DLNA settings", () => {
  beforeEach(() => setLanguage("en"));

  it("needs a user before it can be turned on, then saves user, profile and name", async () => {
    const bodies: unknown[] = [];
    mock(bodies);
    renderApp("/settings/server", { me: testUser({ isAdmin: true }) });
    const section = await screen.findByTestId("dlna-settings");
    expect(section).toHaveTextContent("DLNA for TVs");
    expect(section).toHaveTextContent("Announced on: eth0");

    fireEvent.click(screen.getByRole("switch", { name: "Share with TVs on this network" }));
    expect(screen.getByText("Choose a user before turning DLNA on.")).toBeInTheDocument();
    const save = screen.getAllByRole("button", { name: "Save" }).at(-1)!;
    expect(save).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Act as user"), { target: { value: userId } });
    fireEvent.change(screen.getByLabelText("Profile"), { target: { value: kidId } });
    expect(screen.getByRole("option", { name: "Lea (kids)" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Name on the TV"), { target: { value: "Dnevna soba" } });
    fireEvent.change(screen.getByLabelText("Folder names on the TV"), { target: { value: "hr" } });
    expect(save).toBeEnabled();
    fireEvent.click(save);
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toEqual({ enabled: true, friendlyName: "Dnevna soba", language: "hr", userId, profileId: kidId });
    expect(await screen.findByText("Serving on :8788")).toBeInTheDocument();
  });

  it("is translated to Croatian", async () => {
    setLanguage("hr");
    mock([]);
    renderApp("/settings/server", { me: testUser({ isAdmin: true }) });
    const section = await screen.findByTestId("dlna-settings");
    expect(section).toHaveTextContent("DLNA za televizore");
    expect(screen.getByLabelText("Radi kao korisnik")).toBeInTheDocument();
  });
});
