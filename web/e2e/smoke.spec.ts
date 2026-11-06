import { test, expect } from "@playwright/test";

test("root redirects to sign-in or first-run setup", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/(login|setup)$/);
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(page.getByText(/Streamero/).first()).toBeVisible();
});

test("language can be switched to Croatian", async ({ page }) => {
  await page.goto("/login"); // a fresh server shows the setup wizard instead
  await page.getByLabel(/Language|Jezik/).selectOption("hr");
  await expect(page.getByRole("heading", { name: /^(Prijava|Postavljanje Streamera)$/ })).toBeVisible();
});

test("API reports system info", async ({ request }) => {
  const res = await request.get("/api/v1/system/info");
  expect(res.ok()).toBeTruthy();
  expect((await res.json()).product).toBe("Streamero");
});
