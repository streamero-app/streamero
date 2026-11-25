import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const lib = (id: string, name: string, kind: string) => ({ id, name, kind, paths: [`/media/${id}`], itemCount: 1, scan: { state: "idle", filesSeen: 1 } });
const movies = lib("0190a8b0-0000-7000-8000-00000000a001", "Filmovi", "movies");
const shows = lib("0190a8b0-0000-7000-8000-00000000a002", "Serije", "shows");
const adult = lib("0190a8b0-0000-7000-8000-00000000a003", "Privatno", "adult");

const admin = testUser({ id: "0190a8b0-0000-7000-8000-000000000010", username: "boss", isAdmin: true });
const ana = testUser({ id: "0190a8b0-0000-7000-8000-000000000011", username: "ana" });

type Req = { method: string; path: string; body: unknown };

function serve(libraries: unknown[], users: unknown[] = [admin, ana], extra?: (r: Req) => { status: number; body?: unknown } | undefined) {
  const writes: Req[] = [];
  const fetch = mockApi((r) => {
    if (r.method !== "GET") writes.push(r);
    const own = extra?.(r);
    if (own) return own;
    if (r.path === "/libraries") return { status: 200, body: libraries };
    if (r.path === "/users" && r.method === "GET") return { status: 200, body: users };
    if (r.path.startsWith("/users/") && r.method === "PATCH") return { status: 200, body: { ...ana, ...(r.body as object) } };
    if (r.path === "/users" && r.method === "POST") return { status: 201, body: ana };
    return { status: 200, body: [] };
  });
  return { writes, fetch };
}

async function openEditor(username: string) {
  const row = (await screen.findAllByTestId("user-row")).find((r) => r.textContent?.includes(username))!;
  fireEvent.click(within(row).getByTestId("user-edit"));
  return within(row).getByTestId("user-editor");
}

describe("user editor and library access", () => {
  beforeEach(() => setLanguage("en"));

  it("restricts a user to chosen libraries and saves the list with allLibraries off", async () => {
    const { writes } = serve([movies, shows, adult]);
    renderApp("/settings/users", { me: admin });
    const editor = await openEditor("ana");
    fireEvent.click(within(editor).getByLabelText("Only chosen libraries"));
    // Adult libraries are never offered; a note says why.
    expect(within(editor).queryByLabelText(/Privatno/)).not.toBeInTheDocument();
    expect(within(editor).getByTestId("library-access-adult-note")).toBeInTheDocument();
    // allLibraries is not shown a second time among the permissions.
    expect(within(editor).queryByLabelText("Access to all libraries")).not.toBeInTheDocument();
    fireEvent.click(within(editor).getByLabelText(/Filmovi/));
    fireEvent.click(within(editor).getByLabelText(/Also give libraries created later/));
    fireEvent.click(within(editor).getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]!.path).toBe(`/users/${ana.id}`);
    expect(writes[0]!.body).toMatchObject({ permissions: { allLibraries: false, download: true }, libraryIds: [movies.id], newLibraries: true });
    expect(await within(editor).findByText("Saved.")).toBeInTheDocument();
  });

  it("renames a user and shows a taken username as an error", async () => {
    const { writes } = serve([movies], [admin, ana], (r) =>
      r.method === "PATCH" ? { status: 409, body: { code: "username_taken", message: "taken" } } : undefined);
    renderApp("/settings/users", { me: admin });
    const editor = await openEditor("ana");
    fireEvent.change(within(editor).getByLabelText("Username"), { target: { value: "boss" } });
    fireEvent.click(within(editor).getByRole("button", { name: "Save changes" }));
    expect(await within(editor).findByText("That username is already taken.")).toBeInTheDocument();
    expect(writes[0]!.body).toEqual({ username: "boss" });
  });

  it("explains instead of hiding library access and permissions for administrators", async () => {
    serve([movies, shows]);
    renderApp("/settings/users", { me: admin });
    const editor = await openEditor("boss");
    expect(within(editor).getByText("Administrators see every library.")).toBeInTheDocument();
    expect(within(editor).getByText(/Administrators can always do everything/)).toBeInTheDocument();
    expect(within(editor).queryByTestId("library-access")).not.toBeInTheDocument();
  });

  it("shows adult access disabled with a hint until an adult library exists", async () => {
    serve([movies]);
    renderApp("/settings/users", { me: admin });
    const editor = await openEditor("ana");
    expect(within(editor).getByTestId("adult-access")).toBeDisabled();
    expect(await within(editor).findByText(/Create an adult library first/)).toBeInTheDocument();
  });

  it("gives adult access after a confirmation when an adult library exists", async () => {
    const { writes } = serve([movies, adult], [admin, ana], (r) =>
      r.path.endsWith("/adult-access") ? { status: 200, body: { ...ana, adultAccess: true } } : undefined);
    renderApp("/settings/users", { me: admin });
    const editor = await openEditor("ana");
    fireEvent.click(within(editor).getByTestId("adult-access"));
    fireEvent.click(await screen.findByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(writes).toContainEqual(expect.objectContaining({ path: `/users/${ana.id}/adult-access`, body: { enabled: true, confirm: true } })));
  });

  it("signs a user out everywhere", async () => {
    const { writes } = serve([movies]);
    renderApp("/settings/users", { me: admin });
    const editor = await openEditor("ana");
    fireEvent.click(within(editor).getByRole("button", { name: "Sign out everywhere" }));
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Sign out everywhere" }));
    await waitFor(() => expect(writes[0]?.body).toEqual({ signOutEverywhere: true }));
  });

  it("creates a user with a chosen list of libraries", async () => {
    const { writes } = serve([movies, shows, adult]);
    renderApp("/settings/users", { me: admin });
    await screen.findAllByTestId("user-row");
    const form = screen.getByRole("button", { name: "Add user" }).closest("form")!;
    fireEvent.change(within(form).getByLabelText("Username"), { target: { value: "iva" } });
    fireEvent.change(within(form).getByLabelText("Password"), { target: { value: "iva-password" } });
    fireEvent.click(within(form).getByLabelText("Only chosen libraries"));
    fireEvent.click(await within(form).findByLabelText(/Serije/));
    fireEvent.click(within(form).getByRole("button", { name: "Add user" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]!.body).toMatchObject({ username: "iva", permissions: { allLibraries: false }, libraryIds: [shows.id], newLibraries: false });
  });
});

describe("invites and notifications", () => {
  beforeEach(() => setLanguage("en"));

  it("sends the library choice with a new invite and shows it on existing invites", async () => {
    const invite = { id: "0190a8b0-0000-7000-8000-00000000f001", note: "Sestra", createdBy: "boss", createdAt: new Date().toISOString(),
      expiresAt: new Date(Date.now() + 86_400_000).toISOString(), maxUses: 1, uses: 0,
      permissions: { ...ana.permissions, allLibraries: false }, libraryIds: [movies.id], newLibraries: false };
    const { writes } = serve([movies, shows, adult], [], (r) => {
      if (r.path === "/invites" && r.method === "GET") return { status: 200, body: [invite] };
      if (r.path === "/invites" && r.method === "POST") return { status: 201, body: { token: "tok", invite } };
      return undefined;
    });
    renderApp("/settings/invites", { me: admin });
    expect(await screen.findByTestId("invite-access")).toHaveTextContent("Filmovi");
    fireEvent.click(screen.getByLabelText("Only chosen libraries"));
    expect(screen.queryByLabelText(/Privatno/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByLabelText(/Serije/));
    fireEvent.click(screen.getByRole("button", { name: "Create invite link" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]!.body).toMatchObject({ permissions: { allLibraries: false }, libraryIds: [shows.id], newLibraries: false });
  });

  it("saves a per-target library filter", async () => {
    const target = { id: "t1", name: "Filmovi", kind: "discord", url: "https://discord.com/api/webhooks/1/abc", events: ["item.added"], enabled: true, libraryIds: [] };
    const { writes } = serve([movies, shows], [], (r) => {
      if (r.path === "/settings/notifications" && r.method === "PUT") return { status: 200, body: r.body };
      if (r.path === "/settings/notifications") return { status: 200, body: { targets: [target] } };
      return undefined;
    });
    renderApp("/settings/notifications", { me: admin });
    const t = await screen.findByTestId("notification-target");
    // An empty list means every library.
    expect(within(t).getByLabelText("All libraries")).toBeChecked();
    fireEvent.click(within(t).getByLabelText("Only chosen libraries"));
    fireEvent.click(within(t).getByLabelText(/Filmovi/));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect((writes[0]!.body as { targets: { libraryIds?: string[] }[] }).targets[0]!.libraryIds).toEqual([movies.id]);
  });
});

describe("adult metadata card", () => {
  beforeEach(() => setLanguage("en"));

  it("tells an administrator to create an adult library first", async () => {
    serve([movies], [], (r) => (r.path === "/settings/metadata" ? { status: 200, body: { tmdbConfigured: false } } : undefined));
    renderApp("/settings/metadata", { me: admin });
    const hint = await screen.findByTestId("adult-metadata-hint");
    expect(screen.getByText("Adult metadata (ThePornDB / StashDB)")).toBeInTheDocument();
    expect(hint).toHaveTextContent("Create a library of the kind Adult in Settings → Libraries first");
    expect(within(hint).getByRole("link", { name: "Open Libraries" })).toHaveAttribute("href", "/settings/libraries");
  });
});
