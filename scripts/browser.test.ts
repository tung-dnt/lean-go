import process from "node:process";
import { expect, test } from "@playwright/test";

const email = process.env.LEANGO_BROWSER_EMAIL;
const password = process.env.LEANGO_BROWSER_PASSWORD;
if (!email || !password) throw new Error("Run just browser to seed the synthetic account");

const browserTest = test.extend<{ browserHealth: void }>({
  browserHealth: [async ({ page }, use) => {
    const errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    page.on("console", message => {
      if (message.type() === "error") errors.push(message.text());
    });
    page.on("requestfailed", request => {
      if (new URL(request.url()).pathname.startsWith("/static/")) {
        errors.push(`${request.url()}: ${request.failure()?.errorText}`);
      }
    });
    page.on("response", response => {
      if (new URL(response.url()).pathname.startsWith("/static/") && !response.ok()) {
        errors.push(`${response.url()}: HTTP ${response.status()}`);
      }
    });
    await use();
    expect(errors, "JavaScript and static assets must stay healthy throughout the journey").toEqual([]);
  }, { auto: true }],
});

browserTest("login errors, authentication, hover-safe logout and history", async ({ page }) => {
  await page.goto("/");
  const initialDocument = await page.evaluate(() => performance.timeOrigin);
  await page.getByRole("link", { name: "Log in", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.locator("#login")).toBeVisible();
  expect(await page.evaluate(() => performance.timeOrigin)).toBe(initialDocument);

  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Password", { exact: true }).fill("wrong-password");
  await page.getByRole("button", { name: "Login", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Invalid credentials");
  await expect(page.getByLabel("Email address")).toHaveValue(email);
  await expect(page.getByLabel("Password", { exact: true })).toHaveValue("");

  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Login", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Notes", exact: true })).toBeVisible();
  await expect(page.getByRole("alert")).toContainText("You are now logged in");

  const logout = page.getByRole("link", { name: "Log out", exact: true });
  const speculativeLogout = page.waitForRequest(
    request => new URL(request.url()).pathname === "/logout",
    { timeout: 1_000 },
  );
  await logout.hover();
  await expect(speculativeLogout).rejects.toThrow(/Timeout/);
  await page.reload();
  await expect(logout).toBeVisible();
  await expect(page.getByRole("alert")).toHaveCount(0);

  await logout.click();
  await expect(page.getByRole("link", { name: "Log in", exact: true })).toBeVisible();
  await expect(logout).toHaveCount(0);
  await page.goBack();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.locator("#login")).toBeVisible();
  await expect(logout).toHaveCount(0);
  await page.goForward();
  await expect(page.getByRole("link", { name: "Log in", exact: true })).toBeVisible();
  await expect(logout).toHaveCount(0);
});

browserTest("note validation, Unicode counter, persistence and admin deletion", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Login", exact: true }).click();
  await expect(page.locator("#new-note")).toBeVisible();

  const counter = page.locator("[x-data='charCount'] [x-text]");
  await expect(counter).toHaveText("0 / 5000");
  await page.getByLabel("Note", { exact: true }).fill("Hi 🌍");
  await expect(counter).toHaveText("4 / 5000");
  const initialDocument = await page.evaluate(() => performance.timeOrigin);
  await page.getByRole("button", { name: "Post", exact: true }).click();
  await expect(page.locator("input[name='title'].input-error")).toBeVisible();
  await expect(page.getByLabel("Note", { exact: true })).toHaveValue("Hi 🌍");
  await expect(counter).toHaveText("4 / 5000");
  expect(await page.evaluate(() => performance.timeOrigin)).toBe(initialDocument);
  await page.getByLabel("Note", { exact: true }).fill("After swap 🌍");
  await expect(counter).toHaveText("12 / 5000");

  await page.getByLabel("Title", { exact: true }).fill("Browser regression note");
  await page.getByRole("button", { name: "Post", exact: true }).click();
  await expect(page).toHaveURL(/\/notes\/\d+$/);
  await expect(page.getByRole("heading", { name: "Browser regression note", exact: true })).toBeVisible();
  await expect(page.locator("#note")).toContainText("After swap 🌍");
  await page.reload();
  await expect(page.locator("#note")).toContainText("After swap 🌍");
  await page.getByRole("link", { name: "All notes", exact: true }).click();
  await expect(page.locator("#notes").getByRole("link", { name: "Browser regression note", exact: true })).toBeVisible();
  await page.locator("#notes").getByRole("link", { name: "Browser regression note", exact: true }).click();
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Notes", exact: true })).toBeVisible();
  await expect(page.locator("#notes")).not.toContainText("Browser regression note");
  await page.reload();
  await expect(page.locator("#notes")).not.toContainText("Browser regression note");
});

browserTest("repeated htmx navigation and history reinitialize Alpine", async ({ page }) => {
  await page.goto("/");
  const initialDocument = await page.evaluate(() => performance.timeOrigin);
  const counter = page.locator("[x-data='charCount'] [x-text]");
  for (let visit = 0; visit < 2; visit++) {
    await page.getByRole("link", { name: "Log in", exact: true }).click();
    await expect(page.locator("#login")).toBeVisible();
    await page.getByRole("link", { name: "Cancel", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Notes", exact: true })).toBeVisible();
    await page.getByLabel("Note", { exact: true }).fill("Back 🌍");
    await expect(counter).toHaveText("6 / 5000");
    expect(await page.evaluate(() => performance.timeOrigin)).toBe(initialDocument);
  }
  await page.goBack();
  await expect(page.locator("#login")).toBeVisible();
  await page.goForward();
  await expect(page.locator("#new-note")).toBeVisible();
  await page.getByLabel("Note", { exact: true }).fill("Forward 🌍");
  await expect(counter).toHaveText("9 / 5000");
});
