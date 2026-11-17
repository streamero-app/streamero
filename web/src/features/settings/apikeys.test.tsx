import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const key = (name: string, readOnly: boolean) => ({
  id: `0190a8b0-0000-7000-8000-0000000000${readOnly ? "01" : "02"}`, name, prefix: "sk_abc123", createdBy: "Admin",
  createdAt: "2026-11-17T10:00:00Z", readOnly,
});

function mock(bodies: unknown[]) {
  return mockApi(({ method, path, body }) => {
    if (path === "/apikeys" && method === "POST") {
      bodies.push(body);
      const b = body as { name: string; readOnly: boolean };
      return { status: 201, body: { key: "sk_secret", apiKey: key(b.name, b.readOnly) } };
    }
    if (path === "/apikeys") return { status: 200, body: [key("Dashboard", true), key("Seerr", false)] };
    if (path === "/settings/server") return { status: 200, body: { serverName: "Kino" } };
    return { status: 200, body: [] };
  });
}

describe("API key settings", () => {
  beforeEach(() => setLanguage("en"));

  it("creates a read-only key and marks read-only keys in the list", async () => {
    const bodies: unknown[] = [];
    mock(bodies);
    renderApp("/settings/apikeys", { me: testUser({ isAdmin: true }) });

    const rows = await screen.findAllByTestId("api-key-row");
    expect(rows).toHaveLength(2);
    expect(within(rows[0]!).getByText("Read-only")).toBeInTheDocument();
    expect(within(rows[1]!).queryByText("Read-only")).not.toBeInTheDocument();

    const box = screen.getByRole("checkbox", { name: "Read-only (for dashboards)" });
    expect(box).not.toBeChecked();
    expect(box).toHaveAccessibleDescription(/cannot change anything/);

    // Unticked: an ordinary key.
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Scripts" } });
    fireEvent.click(screen.getByRole("button", { name: "Create API key" }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toEqual({ name: "Scripts", readOnly: false });

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Homepage" } });
    fireEvent.click(box);
    fireEvent.click(screen.getByRole("button", { name: "Create API key" }));
    await waitFor(() => expect(bodies).toHaveLength(2));
    expect(bodies[1]).toEqual({ name: "Homepage", readOnly: true });
    expect(await screen.findByTestId("new-api-key")).toHaveTextContent("sk_secret");
    // The box does not stay ticked for the next key.
    await waitFor(() => expect(box).not.toBeChecked());
  });

  it("is translated to Croatian", async () => {
    setLanguage("hr");
    mock([]);
    renderApp("/settings/apikeys", { me: testUser({ isAdmin: true }) });
    const rows = await screen.findAllByTestId("api-key-row");
    expect(within(rows[0]!).getByText("Samo čitanje")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Samo za čitanje (za nadzorne ploče)" })).toBeInTheDocument();
    expect(screen.getByText(/ali ne može ništa mijenjati/)).toBeInTheDocument();
  });
});
