import { test, expect, type Page } from "@playwright/test";

// Per-library access (D-054) end to end. Runs on the same fresh server as auth.spec.ts
// and reuses its administrator, so it waits until that spec has finished the setup.
const setupCode = process.env.E2E_SETUP_CODE;
const admin = { username: "e2e-admin", password: "e2e-admin-pass" };
const viewer = { username: "e2e-limited", password: "e2e-limited-pass" };

test.describe.configure({ mode: "serial" });
test.skip(!setupCode, "E2E_SETUP_CODE not set (needs a fresh server)");

async function signIn(page: Page, who: { username: string; password: string }) {
  await page.goto("/login");
  await page.getByLabel("Username").fill(who.username);
  await page.getByLabel("Password").fill(who.password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/$/);
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem("streamero.lang", "en"));
});

test("a user limited to one library never sees the other", async ({ page, browser, request }) => {
  test.setTimeout(240_000);
  // The setup wizard test in auth.spec.ts creates the administrator.
  await expect.poll(async () => (await (await request.get("/api/v1/system/info")).json()).setupCompleted,
    { timeout: 120_000 }).toBe(true);
  await signIn(page, admin);

  const libs: Record<string, string> = {};
  for (const [name, kind, folder] of [["Access Movies", "movies", "movies"], ["Access Shows", "shows", "shows"]] as const) {
    const res = await page.request.post("/api/v1/libraries", { data: { name, kind, paths: [`/fixtures/${folder}`] } });
    expect(res.status()).toBe(201);
    libs[kind] = (await res.json()).id;
  }
  await expect.poll(async () => {
    const list = await (await page.request.get("/api/v1/libraries")).json();
    return list.filter((l: { id: string; itemCount: number }) => Object.values(libs).includes(l.id) && l.itemCount > 0).length;
  }, { timeout: 120_000 }).toBe(2);

  // The administrator creates the user with only the movies library, through the Users page.
  await page.goto("/settings/users");
  const form = page.getByRole("button", { name: "Add user" }).locator("xpath=ancestor::form");
  await form.getByLabel("Username").fill(viewer.username);
  await form.getByLabel("Password").fill(viewer.password);
  await form.getByLabel("Only chosen libraries").check();
  await form.getByLabel(/Access Movies/).check();
  await form.getByRole("button", { name: "Add user" }).click();
  await expect(page.getByText("User created.")).toBeVisible();
  const row = page.getByTestId("user-row").filter({ hasText: viewer.username });
  await expect(row.getByTestId("user-access")).toHaveText("Access Movies");

  // The editor shows the same choice.
  await row.getByTestId("user-edit").click();
  const editor = row.getByTestId("user-editor");
  await expect(editor.getByLabel("Only chosen libraries")).toBeChecked();
  await expect(editor.getByLabel(/Access Movies/)).toBeChecked();
  await expect(editor.getByLabel(/Access Shows/)).not.toBeChecked();

  // The user signs in elsewhere and sees only the movies library.
  const ctx = await browser.newContext();
  const other = await ctx.newPage();
  await other.addInitScript(() => localStorage.setItem("streamero.lang", "en"));
  await signIn(other, viewer);
  await expect(other.getByTestId("home-page")).toBeVisible();
  await expect(other.getByTestId("library-card").filter({ hasText: "Access Movies" })).toBeVisible();
  await expect(other.getByTestId("library-card").filter({ hasText: "Access Shows" })).toHaveCount(0);
  await expect(other.getByText("Test Show")).toHaveCount(0);

  const list = await (await other.request.get("/api/v1/libraries")).json();
  expect(list.map((l: { id: string }) => l.id)).toContain(libs.movies);
  expect(list.map((l: { id: string }) => l.id)).not.toContain(libs.shows);
  expect((await other.request.get(`/api/v1/libraries/${libs.shows}/items`)).status()).toBe(404);

  // A direct link to the hidden library shows "not found" and none of its items.
  await other.goto(`/library/${libs.shows}`);
  // The items query retries before it gives up, so allow for its back-off.
  await expect(other.getByRole("alert")).toContainText("Not found", { timeout: 30_000 });
  await expect(other.getByTestId("item-card").filter({ hasText: "Test Show" })).toHaveCount(0);

  // The visible library works as usual.
  await other.goto(`/library/${libs.movies}`);
  await expect(other.getByTestId("item-card").filter({ hasText: "Big Test Movie" })).toBeVisible();
  await ctx.close();
});
