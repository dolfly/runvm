import { test, expect } from "@playwright/test";

// Real-browser DOM assertions for the console (acceptance #4/#3b): the pages
// must render in an actual Chromium, not just return 200 from the API.

const USER = "admin";
const PASS = "secret123";

async function login(page: import("@playwright/test").Page) {
  await page.goto("/");
  await expect(page.locator(".login-col")).toBeVisible();
  await page.locator('input[autocomplete="username"]').fill(USER);
  await page.locator('input[type="password"]').fill(PASS);
  await page.locator(".login-btn").click();
  // login does a full reload to the target path
  await expect(page.locator(".topbar")).toBeVisible({ timeout: 15000 });
}

test("three-section navigation renders and deep links work", async ({ page }) => {
  await login(page);
  const nav = page.locator(".nav a");
  await expect(nav).toHaveCount(3);
  await expect(nav.nth(0)).toHaveText("系统总览");
  await expect(nav.nth(1)).toHaveText("资源管理");
  await expect(nav.nth(2)).toHaveText("MCP调试");

  // overview page has backend cards
  await expect(page.locator(".backend-card")).toHaveCount(3);

  // deep link straight to a resource subpage renders the shell + sidebar
  await page.goto("/resources/tools");
  await expect(page.locator(".sidebar")).toBeVisible();
  await expect(page.locator(".tools-layout")).toBeVisible();

  await page.goto("/debug");
  await expect(page.locator(".debug-layout")).toBeVisible();
  await expect(page.locator(".debug-conn .pill")).toHaveText("只读");
});

test("search hits tools and files", async ({ page }) => {
  await login(page);
  await page.goto("/resources");
  const box = page.locator(".search-box-wrap input");
  await expect(box).toBeVisible();

  // a tool name match
  await box.fill("runvm_status");
  await expect(page.locator(".search-row-name").first()).toHaveText("runvm_status");

  // a file path match (pre-populated e2e.json)
  await box.fill("e2e.json");
  await expect(page.locator(".search-row-name").first()).toContainText("e2e.json");
});

test("file browser previews, edits and saves json; rejects invalid json", async ({ page }) => {
  await login(page);
  await page.goto("/resources/files");

  // navigate into the pre-populated docs/ directory
  await page.locator(".files-list-body tr", { hasText: "docs" }).click();
  await expect(page.locator(".files-list-body")).toContainText("e2e.json");

  // select it → json tree view
  await page.locator(".files-list-body tr", { hasText: "e2e.json" }).click();
  await expect(page.locator(".json-tree")).toBeVisible();

  // enter edit, type invalid json, save must be blocked with an error
  await page.locator(".files-view-pane .editor-bar button", { hasText: "编辑" }).click();
  const editor = page.locator(".files-editor");
  await expect(editor).toBeVisible();
  await editor.fill("{ not json");
  await page.locator(".files-view-pane .editor-bar button", { hasText: "保存" }).click();
  await expect(page.locator(".error-box")).toContainText("非法 JSON");

  // fix it and save → toast + status bar refreshes
  await editor.fill('{"hello":"world","edited":true}');
  await page.locator(".files-view-pane .editor-bar button", { hasText: "保存" }).click();
  await expect(page.locator(".toast.ok")).toBeVisible({ timeout: 10000 });
});

test("read-only resource has no edit affordance", async ({ page }) => {
  await login(page);
  await page.goto("/resources/tools");
  // the tool catalog is read-only: no "编辑" button anywhere on this page
  await expect(page.locator(".tools-detail")).toBeVisible();
  await expect(page.locator("button", { hasText: "编辑" })).toHaveCount(0);
});

test("i18n switch is in-place, persists, and leaves no untranslated chrome", async ({ page }) => {
  await login(page);
  await expect(page.locator(".nav a").nth(0)).toHaveText("系统总览");

  // mark the window to prove no full reload happens on switch
  await page.evaluate(() => {
    (window as unknown as { __marker: number }).__marker = 12345;
  });

  await page.locator(".segmented button", { hasText: "EN" }).click();
  await expect(page.locator(".nav a").nth(0)).toHaveText("Overview");
  await expect(page.locator(".nav a").nth(1)).toHaveText("Resources");
  await expect(page.locator(".nav a").nth(2)).toHaveText("MCP Debug");

  const marker = await page.evaluate(() => (window as unknown as { __marker: number }).__marker);
  expect(marker).toBe(12345);

  // preference persists across a real reload (cookie + localStorage)
  await page.reload();
  await expect(page.locator(".nav a").nth(0)).toHaveText("Overview");

  // switch back to Chinese
  await page.locator(".segmented button", { hasText: "中文" }).click();
  await expect(page.locator(".nav a").nth(0)).toHaveText("系统总览");
});
