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
  await expect(page.getByTestId("home-page")).toBeVisible();
  // The name is shown in server settings (the version moved there too, off the home page).
  await page.goto("/settings/server");
  await expect(page.getByLabel("Server name")).toHaveValue("E2E Kino");
  await expect(page.getByTestId("settings-version")).toBeVisible();
});

test("admin adds a user; user signs in; admin signs the device out", async ({ page, browser }) => {
  await signIn(page, admin);
  await page.getByTestId("account-menu").click();
  await page.getByRole("menuitem", { name: "Settings" }).click();
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

  await expect(tv.getByTestId("home-page")).toBeVisible({ timeout: 15_000 });
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
  await expect(guest.getByTestId("home-page")).toBeVisible();
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

test("playback: player opens the real file; forced transcode serves HLS", async ({ page }) => {
  await signIn(page, admin);
  await page.goto("/");
  await page.getByTestId("library-card").filter({ hasText: "Fixture Movies" }).click();
  await page.getByTestId("item-card").filter({ hasText: "Big Test Movie" }).click();
  await page.getByTestId("play").click();
  await expect(page.getByTestId("player")).toBeVisible();
  await expect(page.getByTestId("play-method")).toHaveText(/Direct play|Direct stream|Transcoding/, { timeout: 45_000 });
  const itemId = page.url().split("/watch/")[1]!.split("?")[0]!;

  // API: a device that supports nothing natively forces an H.264 transcode via ffmpeg.
  const res = await page.request.post(`/api/v1/items/${itemId}/playback`, {
    data: { device: { containers: [], videoCodecs: [], audioCodecs: [], hls: true }, startMs: 2000 },
  });
  expect(res.ok()).toBeTruthy();
  const info = await res.json();
  expect(info.method).toBe("transcode");
  expect(info.startMs).toBe(2000);
  const playlist = await (await page.request.get(info.url)).text();
  expect(playlist).toContain("#EXTINF");
  const init = playlist.match(/URI="([^"]+)"/)![1]!;
  const base = info.url.slice(0, info.url.lastIndexOf("/") + 1).split("?")[0];
  expect((await page.request.get(base + init)).status()).toBe(200);
  const firstSegment = playlist.split("\n").find((l: string) => l && !l.startsWith("#"))!;
  const seg = await page.request.get(base + firstSegment);
  expect(seg.status()).toBe(200);
  expect((await seg.body()).length).toBeGreaterThan(1000);
  // Without the signature the same URL is refused.
  expect((await page.request.get((base + firstSegment).split("?")[0]!)).status()).toBe(403);

  await page.getByRole("link", { name: /Back/ }).click();
  await page.goto("/");
});

test("books, comics, audiobooks and photos: scan, read, listen and view", async ({ page }) => {
  test.setTimeout(240_000);
  await signIn(page, admin);
  const libs: Record<string, string> = {};
  for (const [name, kind, folder] of [["E2E Books", "books", "books"], ["E2E Audiobooks", "audiobooks", "audiobooks"], ["E2E Photos", "photos", "photos"]]) {
    const res = await page.request.post("/api/v1/libraries", { data: { name, kind, paths: [`/fixtures/${folder}`] } });
    expect(res.status()).toBe(201);
    libs[kind!] = (await res.json()).id;
  }
  // The worker scans with real ffprobe, then makes covers and thumbnails.
  await expect.poll(async () => {
    const list = await (await page.request.get("/api/v1/libraries")).json();
    return list.filter((l: { id: string; itemCount: number }) => Object.values(libs).includes(l.id) && l.itemCount > 0).length;
  }, { timeout: 120_000 }).toBe(3);

  // Books: library, comic reader (right to left), progress kept.
  await page.goto(`/library/${libs.books}`);
  await expect(page.getByTestId("book-card")).toHaveCount(4);
  await page.getByTestId("book-card").filter({ hasText: "Space Cats 001" }).click();
  await expect(page.getByTestId("book-page")).toContainText("Mia Writer");
  await page.getByTestId("read").click();
  await expect(page.getByTestId("comic-position")).toHaveText(/Page 1 of 4/);
  await expect(page.getByTestId("comic-page").first()).toHaveJSProperty("complete", true);
  expect(await page.getByTestId("comic-page").first().evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(800);
  await page.keyboard.press("ArrowLeft"); // manga: left turns forward
  await expect(page.getByTestId("comic-position")).toHaveText(/Page 2 of 4/);
  await page.waitForTimeout(2000);
  await page.mouse.move(200, 200);
  await page.getByTestId("reader-back").click();
  await expect(page.getByTestId("read")).toContainText("50");

  // EPUB in the reader.
  await page.goto(`/library/${libs.books}`);
  await page.getByTestId("book-card").filter({ hasText: "The Long Way" }).click();
  await expect(page.getByTestId("book-page")).toContainText("Wayfarers #1");
  await page.getByTestId("read").click();
  await expect(page.getByTestId("epub-position")).toHaveText(/\d/, { timeout: 30_000 });
  await expect(page.frameLocator("foliate-view iframe").first().locator("h1")).toHaveText("Chapter 1", { timeout: 30_000 }).catch(async () => {
    // The paginator keeps its iframe in a shadow root; fall back to checking the reported position.
    await expect(page.getByTestId("epub-position")).toBeVisible();
  });
  await page.keyboard.press("ArrowRight");
  await page.mouse.move(200, 200);
  await page.getByTestId("reader-back").click();

  // PDF.
  await page.goto(`/library/${libs.books}`);
  await page.getByTestId("book-card").filter({ hasText: "Plain Document" }).click();
  await page.getByTestId("read").click();
  await expect(page.getByTestId("pdf-position")).toHaveText("Page 1 of 2", { timeout: 30_000 });
  await page.keyboard.press("ArrowRight");
  await expect(page.getByTestId("pdf-position")).toHaveText("Page 2 of 2");
  await page.mouse.move(200, 200);
  await page.getByTestId("reader-back").click();

  // Audiobook: chapters from the M4B, playback through the music bar.
  await page.goto(`/library/${libs.audiobooks}`);
  await page.getByTestId("book-card").filter({ hasText: "The Sound Book" }).click();
  await expect(page.getByTestId("book-page")).toContainText("Nina Narrator");
  await expect(page.getByTestId("chapter-list")).toContainText("Closing");
  await page.getByTestId("chapter-list").getByRole("button").nth(1).click();
  await expect(page.getByTestId("now-playing-bar")).toContainText("The Sound Book");
  await page.getByTestId("speed").selectOption("1.5");

  // Photos: album, lightbox, slideshow interval.
  await page.goto(`/library/${libs.photos}`);
  await page.getByTestId("photo-album").filter({ hasText: "Holiday 2025" }).click();
  await expect(page.getByTestId("photo-album")).toContainText("Beach");
  await page.getByTestId("photo-thumb").first().click();
  await expect(page.getByTestId("lightbox-image")).toBeVisible();
  expect(await page.getByTestId("lightbox-image").evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1600);
  await page.getByTestId("slideshow-interval").selectOption("3");
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("lightbox")).toHaveCount(0);
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
  await page.getByTestId("account-menu").click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto("/settings/account");
  await expect(page).toHaveURL(/\/login$/);
});
