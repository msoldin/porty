import { test, expect } from "@playwright/test";
import { readdir } from "node:fs/promises";
import { resolve } from "node:path";

test("opens every guide directly and keeps links and images usable", async ({
  page,
  request,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  const guides = (await readdir(process.cwd())).filter((name) =>
    name.endsWith(".md"),
  );
  for (const name of guides) {
    const route = name === "README.md" ? "" : name.replace(/\.md$/, ".html");
    const response = await page.goto(route || "./");
    expect(response?.status()).toBe(200);
    await expect(page.locator(".vp-doc h1")).toBeVisible();
    await expect(
      page.getByText("Development documentation — main branch", {
        exact: false,
      }),
    ).toBeVisible();
    const links = await page
      .locator(".vp-doc a[href]")
      .evaluateAll((nodes) =>
        nodes.map((node) => (node as HTMLAnchorElement).href),
      );
    for (const href of new Set(links)) {
      const url = new URL(href);
      if (url.origin !== "http://127.0.0.1:4174") continue;
      expect(url.pathname).toMatch(/^\/porty\//);
      const linked = await request.get(url.href);
      expect(linked.status(), href).toBe(200);
      if (url.hash) {
        const html = await linked.text();
        expect(html, href).toContain(
          `id="${decodeURIComponent(url.hash.slice(1))}"`,
        );
      }
    }
    for (const img of await page.locator(".vp-doc img").all()) {
      await img.scrollIntoViewIfNeeded();
      await expect
        .poll(() =>
          img.evaluate((node) => (node as HTMLImageElement).naturalWidth),
        )
        .toBeGreaterThan(0);
    }
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  }
  await page.reload();
  await expect(page.locator(".vp-doc h1")).toBeVisible();
  expect(errors).toEqual([]);
});

test("finds configuration with local search", async ({ page }) => {
  await page.goto("./");
  await page
    .getByRole("button", { name: "Search", exact: false })
    .first()
    .click();
  await page.getByRole("searchbox").fill("public_url");
  await expect(page.locator(".VPLocalSearchBox .result").first()).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".VPLocalSearchBox")).not.toBeVisible();
});

test("opens navigation on mobile and records the rendered page", async ({
  page,
}, info) => {
  await page.goto("./");
  if (info.project.name === "mobile") {
    await page.getByRole("button", { name: "Menu", exact: true }).click();
    await expect(page.locator(".VPSidebar")).toBeVisible();
    await page
      .locator(".VPSidebar")
      .getByRole("link", { name: "Configuration", exact: true })
      .click();
    await expect(page.locator("h1")).toContainText("Configuration");
  }
  await expect(page.locator(".VPBackdrop")).not.toBeVisible();
  await page.screenshot({
    animations: "disabled",
    path: info.outputPath("documentation.png"),
    fullPage: true,
  });
});

test("excludes internal design content from the published artifact", async () => {
  const files = await readdir(resolve(".vitepress/dist"), { recursive: true });
  expect(
    files.filter((name) =>
      /superpowers|concept|generation\.json|preview\.html/.test(name),
    ),
  ).toEqual([]);
});
