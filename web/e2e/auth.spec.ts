import { test, expect, type Page } from "@playwright/test";

// Full P1 flow against a FRESH server (CI sets STREAMERO_SETUP_CODE=E2E_SETUP_CODE).
// Never point this at a server that is already set up or that you care about.
const setupCode = process.env.E2E_SETUP_CODE;
const admin = { username: "e2e-admin", password: "e2e-admin-pass" };
const user = { username: "e2e-ana", password: "e2e-ana-pass" };

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

test("setup wizard creates the admin and signs in", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/setup$/);
  await page.getByLabel("Setup code").fill("WRONG-CODE");
  await page.getByLabel("Username").fill(admin.username);
  await page.getByLabel("Password", { exact: true }).fill(admin.password);
  await page.getByLabel("Confirm password").fill(admin.password);
  await page.getByRole("button", { name: "Finish setup" }).click();
  await expect(page.getByRole("alert")).toHaveText("The setup code is not correct.");

  await page.getByLabel("Setup code").fill(setupCode!.toLowerCase());
  await page.getByLabel("Server name").fill("E2E Kino");
  await page.getByRole("button", { name: "Finish setup" }).click();
  await expect(page.getByRole("heading", { name: `Welcome, ${admin.username}` })).toBeVisible();
  await expect(page.getByText("E2E Kino").first()).toBeVisible();
});

test("admin adds a user; user signs in; admin signs the device out", async ({ page, browser }) => {
  await signIn(page, admin);
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  await page.getByRole("link", { name: "Users" }).click();
  await page.getByLabel("Username").fill(user.username);
  await page.getByLabel("Password").fill(user.password);
  await page.getByRole("button", { name: "Add user" }).click();
  await expect(page.getByText("User created.")).toBeVisible();
  await expect(page.getByTestId("user-row").filter({ hasText: user.username })).toBeVisible();

  // The new user signs in from another browser.
  const ctx = await browser.newContext();
  const other = await ctx.newPage();
  await other.addInitScript(() => localStorage.setItem("streamero.lang", "en"));
  await signIn(other, user);
  await other.goto("/settings/users");
  await expect(other).toHaveURL(/\/settings\/account$/); // not an admin

  // Admin sees the device and signs it out; the user is out immediately.
  await page.getByRole("link", { name: "Devices" }).click();
  const row = page.getByTestId("session-row").filter({ hasText: user.username });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "Sign out" }).click();
  await expect(row).toHaveCount(0);
  await other.goto("/settings/devices");
  await expect(other).toHaveURL(/\/login$/);
  await ctx.close();
});

test("Quick Connect signs in a second browser after approval", async ({ page, browser }) => {
  const ctx = await browser.newContext();
  const tv = await ctx.newPage();
  await tv.addInitScript(() => localStorage.setItem("streamero.lang", "en"));
  await tv.goto("/login");
  await tv.getByRole("button", { name: "Sign in with Quick Connect" }).click();
  const code = (await tv.getByLabel("Quick Connect code").textContent())!.trim();
  expect(code).toMatch(/^\d{6}$/);

  await signIn(page, admin);
  await page.goto("/settings/quickconnect");
  await page.getByLabel("Quick Connect code").fill(code);
  await page.getByRole("button", { name: "Approve" }).click();
  await expect(page.getByText(/is now signed in/)).toBeVisible();

  await expect(tv.getByRole("heading", { name: `Welcome, ${admin.username}` })).toBeVisible({ timeout: 15_000 });
  await ctx.close();
});

test("an invite link creates an account", async ({ page, browser }) => {
  await signIn(page, admin);
  await page.goto("/settings/invites");
  await page.getByLabel("Note (optional)").fill("e2e");
  await page.getByRole("button", { name: "Create invite link" }).click();
  const link = (await page.getByTestId("new-invite").locator("code").textContent())!.trim();
  expect(link).toMatch(/\/invite\/[\w-]+$/);

  const ctx = await browser.newContext();
  const guest = await ctx.newPage();
  await guest.addInitScript(() => localStorage.setItem("streamero.lang", "en"));
  await guest.goto(new URL(link).pathname);
  await guest.getByLabel("Username").fill("e2e-guest");
  await guest.getByLabel("Password", { exact: true }).fill("e2e-guest-pass");
  await guest.getByLabel("Confirm password").fill("e2e-guest-pass");
  await guest.getByRole("button", { name: "Create account" }).click();
  await expect(guest.getByRole("heading", { name: "Welcome, e2e-guest" })).toBeVisible();
  // Single use by default.
  await guest.goto(new URL(link).pathname);
  await expect(guest.getByRole("heading", { name: "Invite not valid" })).toBeVisible();
  await ctx.close();
});

test("admin adds a library; the scan finds real movies with versions", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/settings/libraries");
  await page.getByRole("button", { name: "Add library" }).click();
  await page.getByLabel("Name").fill("Fixture Movies");
  await page.getByRole("button", { name: "Add folder" }).click();
  const picker = page.getByTestId("folder-picker");
  await picker.getByRole("button", { name: /\/fixtures$/ }).click();
  await picker.getByRole("button", { name: /movies$/ }).click();
  await picker.getByRole("button", { name: "Use this folder" }).click();
  await expect(page.getByTestId("library-folder")).toHaveText(/\/fixtures\/movies/);
  await page.getByRole("button", { name: "Save" }).click();

  // Scan runs in the worker with real ffprobe.
  const row = page.getByTestId("library-row").filter({ hasText: "Fixture Movies" });
  await expect(row).toContainText(/[1-9]\d* items/, { timeout: 90_000 });

  await page.goto("/");
  await page.getByTestId("library-card").filter({ hasText: "Fixture Movies" }).click();
  await page.getByTestId("item-card").filter({ hasText: "Big Test Movie" }).click();
  await expect(page.getByText("2 versions")).toBeVisible();
  await expect(page.getByTestId("media-file").filter({ hasText: "4K" })).toBeVisible();
  await expect(page.getByTestId("media-file").first()).toContainText("Subtitles");
});

test("wrong passwords lock the account", async ({ page }) => {
  await page.goto("/login");
  for (let i = 0; i < 5; i++) {
    await page.getByLabel("Username").fill(user.username);
    await page.getByLabel("Password").fill("not-the-password");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByRole("alert")).toBeVisible();
  }
  await expect(page.getByRole("alert")).toContainText("locked");
});

test("sign out returns to sign-in", async ({ page }) => {
  await signIn(page, admin);
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto("/settings/account");
  await expect(page).toHaveURL(/\/login$/);
});
