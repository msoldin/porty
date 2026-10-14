import { test, expect } from "@playwright/test";
import { readdir } from "node:fs/promises";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const { editions, stableTag } = JSON.parse(
  readFileSync(resolve(".vitepress/dist/editions.json"), "utf8"),
);
for (const edition of editions) {
  test.describe(edition.base, () => {
    test.use({ baseURL: `http://127.0.0.1:4174${edition.base}` });

    test("opens every guide directly and keeps links and images usable", async ({
      page,
      request,
    }) => {
      const errors: string[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      page.on("console", (message) => {
        if (message.type() === "error") errors.push(message.text());
      });
      const guides = edition.guides;
      for (const name of guides) {
        const route =
          name === "README.md" ? "" : name.replace(/\.md$/, ".html");
        const response = await page.goto(route || "./");
        expect(response?.status()).toBe(200);
        await expect(page.locator(".vp-doc h1")).toBeVisible();
        await expect(page.locator(".development-notice")).toBeVisible();
        await expect(page.locator(".development-notice")).toContainText(
          edition.ref !== "main"
            ? `Stable documentation — ${edition.ref}`
            : edition.base === "/porty/" && !stableTag
              ? "No stable release yet"
              : "Development documentation — main branch",
        );
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
      await expect(
        page.locator(".VPLocalSearchBox .result").first(),
      ).toBeVisible();
      const results = await page
        .locator(".VPLocalSearchBox .result")
        .evaluateAll((nodes) =>
          nodes.map((node) => (node as HTMLAnchorElement).href),
        );
      for (const href of results) {
        const path = new URL(href).pathname;
        expect(path.startsWith(edition.base)).toBe(true);
        if (edition.base === "/porty/")
          expect(path.startsWith("/porty/next/")).toBe(false);
      }
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

    test("switches editions with a full navigation and keeps source links version matched", async ({
      page,
    }) => {
      await page.goto("configuration.html");
      const source = page.locator(".edit-link-button");
      await expect(source).toHaveAttribute(
        "href",
        `https://github.com/msoldin/porty/${edition.ref === "main" ? "edit" : "blob"}/${edition.ref}/docs/configuration.md`,
      );
      // Exercise an interactive control so the client router is active before
      // switching; a click on server-rendered HTML can hide router interception.
      await page.getByRole("button", { name: "Search" }).first().click();
      await expect(page.getByRole("searchbox")).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(page.locator(".VPLocalSearchBox")).not.toBeVisible();
      const destination =
        edition.base === "/porty/" ? "/porty/next/" : "/porty/";
      const [response] = await Promise.all([
        page.waitForResponse(
          (response) =>
            response.url() === `http://127.0.0.1:4174${destination}` &&
            response.request().resourceType() === "document",
          { timeout: 5000 },
        ),
        page.locator(".development-notice a").click(),
      ]);
      expect(response.status()).toBe(200);
      await expect(page).toHaveURL(`http://127.0.0.1:4174${destination}`);
      await expect(page.locator(".vp-doc h1")).toBeVisible();
      await expect(page.locator(".development-notice a")).toHaveAttribute(
        "href",
        edition.base,
      );
    });
  });
}

test("excludes internal design content from the published artifact", async () => {
  const files = await readdir(resolve(".vitepress/dist"), { recursive: true });
  expect(
    files.filter((name) =>
      /superpowers|concept|generation\.json|preview\.html/.test(name),
    ),
  ).toEqual([]);
});
