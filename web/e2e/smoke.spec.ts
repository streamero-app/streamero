import { test, expect } from "@playwright/test";

test("root redirects to login and shows server version", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(page.getByText(/Streamero/).first()).toBeVisible();
});

test("language can be switched to Croatian", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel(/Language|Jezik/).selectOption("hr");
  await expect(page.getByRole("heading", { name: "Prijava" })).toBeVisible();
});

test("API reports system info", async ({ request }) => {
  const res = await request.get("/api/v1/system/info");
  expect(res.ok()).toBeTruthy();
  expect((await res.json()).product).toBe("Streamero");
});
