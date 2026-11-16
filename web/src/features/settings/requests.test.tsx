import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const current = {
  seerrConfigured: false, usersCanRequest: true, radarrConfigured: true, radarrUrl: "http://radarr:7878",
  sonarrConfigured: false, bazarrConfigured: false, lidarrConfigured: true, lidarrUrl: "http://lidarr:8686", prowlarrConfigured: false,
};

function mock(bodies: Record<string, unknown>[]) {
  return mockApi(({ method, path, body }) => {
    if (path === "/settings/requests" && method === "PUT") {
      bodies.push(body as Record<string, unknown>);
      return { status: 200, body: { ...current, prowlarrConfigured: true, prowlarrUrl: "http://prowlarr:9696" } };
    }
    if (path === "/settings/requests") return { status: 200, body: current };
    if (path === "/settings/requests/test") {
      return { status: 200, body: [{ app: "Lidarr", ok: true, version: "2.9.6" }, { app: "Prowlarr", ok: false, error: "Prowlarr answered HTTP 401" }] };
    }
    return { status: 200, body: [] };
  });
}

describe("Connection settings: Lidarr and Prowlarr", () => {
  beforeEach(() => setLanguage("en"));

  it("saves the Lidarr and Prowlarr connections and tests them", async () => {
    const bodies: Record<string, unknown>[] = [];
    mock(bodies);
    renderApp("/settings/requests", { me: testUser({ isAdmin: true }) });
    expect(await screen.findByLabelText("Lidarr address")).toHaveValue("http://lidarr:8686");
    expect(screen.getByText(/Used only by the Streamero Manage apps/)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Prowlarr address"), { target: { value: "http://prowlarr:9696" } });
    fireEvent.change(screen.getByLabelText("Prowlarr API key"), { target: { value: "prowlarr-key" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({ lidarrUrl: "http://lidarr:8686", prowlarrUrl: "http://prowlarr:9696", prowlarrApiKey: "prowlarr-key", lidarrApiKey: "" });
    await waitFor(() => expect(screen.getByLabelText("Prowlarr API key")).toHaveValue(""));

    fireEvent.click(screen.getByRole("button", { name: "Test connections" }));
    const checks = await screen.findByTestId("connection-checks");
    expect(checks).toHaveTextContent("Lidarrconnected, version 2.9.6");
    expect(checks).toHaveTextContent("Prowlarr answered HTTP 401");
  });

  it("is translated to Croatian", async () => {
    setLanguage("hr");
    mock([]);
    renderApp("/settings/requests", { me: testUser({ isAdmin: true }) });
    expect(await screen.findByLabelText("Adresa Lidarr")).toBeInTheDocument();
    expect(screen.getByText(/Koriste ih samo aplikacije Streamero Manage/)).toBeInTheDocument();
  });
});
