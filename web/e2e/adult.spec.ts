import { test, expect, type Page } from "@playwright/test";

// The adult section's navigation and gallery viewer end to end (D-058). Runs on the
// fresh server of auth.spec.ts with its administrator. The media are the synthetic
// test patterns of scripts/gen-fixtures.sh (adult-e2e/): one scene, its gallery
// next to it and a plain folder of pictures.
const setupCode = process.env.E2E_SETUP_CODE;
const admin = { username: "e2e-admin", password: "e2e-admin-pass" };

test.describe.configure({ mode: "serial" });
test.skip(!setupCode, "E2E_SETUP_CODE not set (needs a fresh server)");

async function signIn(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Username").fill(admin.username);
  await page.getByLabel("Password").fill(admin.password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/$/);
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem("streamero.lang", "en"));
});

test("browse the adult section and open a gallery", async ({ page, request }) => {
  test.setTimeout(240_000);
  await expect.poll(async () => (await (await request.get("/api/v1/system/info")).json()).setupCompleted,
    { timeout: 120_000 }).toBe(true);
  await signIn(page);

  // Nothing adult is offered before the account is given adult access.
  await expect(page.getByTestId("adult-link")).toHaveCount(0);
  const me = await (await page.request.get("/api/v1/auth/me")).json();
  expect((await page.request.put(`/api/v1/users/${me.id}/adult-access`, { data: { enabled: true, confirm: true } })).status()).toBe(200);
  const created = await page.request.post("/api/v1/libraries", {
    data: { name: "E2E Private", kind: "adult", paths: [process.env.E2E_ADULT_DIR ?? "/fixtures/adult-e2e"], confirmAdult: true },
  });
  expect(created.status()).toBe(201);
  const lib = (await created.json()).id as string;
  try {
    await expect.poll(async () => (await (await page.request.get("/api/v1/adult/galleries")).json()).total ?? 0,
      { timeout: 120_000 }).toBe(2);
    // Pictures stay blurred until clicked by default; the walk-through turns that off.
    expect((await page.request.put("/api/v1/adult/preferences", { data: { blur: false } })).status()).toBe(200);

    await page.goto("/");
    await page.getByTestId("adult-link").click();
    await expect(page).toHaveURL(/\/adult$/);
    const nav = page.getByTestId("adult-nav");
    for (const name of ["Home", "Scenes", "Movies", "Photos", "Performers", "Studios", "Tags"]) {
      await expect(nav.getByRole("link", { name })).toBeVisible();
    }
    await expect(page.getByTestId("adult-home-galleries").getByTestId("adult-gallery-card")).toHaveCount(2);

    // Scenes: the scene parsed from its Locarr-style name.
    await nav.getByRole("link", { name: "Scenes" }).click();
    await expect(page.getByTestId("adult-scene-card")).toHaveCount(1);
    await expect(page.getByTestId("adult-scene-card")).toContainText("Morning Light");

    // Photos → a gallery → the viewer.
    await nav.getByRole("link", { name: "Photos" }).click();
    await expect(page).toHaveURL(/\/adult\/galleries/);
    await expect(page.getByTestId("adult-gallery-card")).toHaveCount(2);
    await page.getByTestId("adult-gallery-card").filter({ hasText: "Morning Light" }).click();
    await expect(page.getByRole("heading", { name: "Morning Light" })).toBeVisible();
    await expect(page.getByTestId("adult-breadcrumbs")).toContainText("Photos");
    await expect(page.getByTestId("adult-gallery-thumb")).toHaveCount(4);
    // The gallery belongs to the scene of the same studio and date.
    await expect(page.getByTestId("adult-gallery-scene")).toContainText("Morning Light");
    const thumb = page.getByTestId("adult-gallery-thumb").first().locator("img");
    await expect.poll(() => thumb.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0);

    await page.getByTestId("adult-gallery-open").click();
    const box = page.getByTestId("adult-lightbox");
    await expect(box).toBeVisible();
    await expect(page.getByTestId("adult-lightbox-counter")).toHaveText("1 / 4");
    const big = page.getByTestId("adult-lightbox-image");
    await expect.poll(() => big.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0);
    await page.keyboard.press("ArrowRight");
    await expect(page.getByTestId("adult-lightbox-counter")).toHaveText("2 / 4");
    await page.keyboard.press("ArrowLeft");
    await page.keyboard.press("ArrowLeft");
    await expect(page.getByTestId("adult-lightbox-counter")).toHaveText("4 / 4");
    await page.getByTestId("adult-lightbox-next").click();
    await expect(page.getByTestId("adult-lightbox-counter")).toHaveText("1 / 4");
    // The administrator may download originals.
    await expect(page.getByTestId("adult-download")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(box).toHaveCount(0);
    // Pictures are never cached by the browser.
    const res = await page.request.get(await thumb.getAttribute("src") ?? "");
    expect(res.status()).toBe(200);
    expect(res.headers()["cache-control"]).toBe("private, no-store");

    // Breadcrumb back to Photos, then the performer with the galleries tab.
    await page.getByTestId("adult-breadcrumbs").getByRole("link", { name: "Photos" }).click();
    await expect(page).toHaveURL(/\/adult\/galleries/);
    await nav.getByRole("link", { name: "Performers" }).click();
    await page.getByTestId("adult-tile").filter({ hasText: "Ana Bee" }).click();
    await expect(page.getByRole("heading", { name: "Ana Bee" })).toBeVisible();
    await page.getByTestId("adult-tab-galleries").click();
    await expect(page.getByTestId("adult-gallery-card")).toHaveCount(1);
    await page.getByTestId("adult-favorite").click();
    await expect(page.getByTestId("adult-favorite")).toHaveAttribute("aria-pressed", "true");
    await page.goBack();
    await expect(page.getByTestId("adult-tile").filter({ hasText: "Ana Bee" })).toBeVisible();

    // Studios and tags.
    await nav.getByRole("link", { name: "Studios" }).click();
    await page.getByTestId("adult-tile").filter({ hasText: "Lumen Studio" }).click();
    await expect(page.getByRole("heading", { name: "Lumen Studio" })).toBeVisible();
    await nav.getByRole("link", { name: "Tags" }).click();
    await expect(page.getByPlaceholder("Search tags")).toBeVisible();

    // Search inside the section.
    await page.getByTestId("adult-search").fill("garden");
    await page.getByTestId("adult-search").press("Enter");
    await expect(page.getByTestId("adult-search-results").getByTestId("adult-gallery-card")).toContainText("Garden Set");

    // The regular search never shows any of it.
    const regular = await (await page.request.get("/api/v1/search?q=Morning")).text();
    expect(regular).not.toContain("Morning Light");
  } finally {
    await page.request.delete(`/api/v1/libraries/${lib}`);
    await page.request.put(`/api/v1/users/${me.id}/adult-access`, { data: { enabled: false } });
  }
});
