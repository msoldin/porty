import { expect, test, type Page } from "@playwright/test";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn, type ChildProcess } from "node:child_process";

let server: ChildProcess;
let dataDir: string;
let baseURL: string;

async function waitForServer(url: string) {
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      if ((await fetch(`${url}/healthz`)).ok) return;
    } catch {
      /* starting */
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error("Porty did not start");
}

test.beforeAll(async () => {
  dataDir = await mkdtemp(join(tmpdir(), "porty-e2e-"));
  const port = 18080 + Math.floor(Math.random() * 1000);
  baseURL = `http://127.0.0.1:${port}`;
  server = spawn(
    process.env.PORTY_E2E_BINARY || join(tmpdir(), "porty-e2e"),
    ["--listen", `127.0.0.1:${port}`, "--data-dir", dataDir],
    {
      env: process.env,
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
  await waitForServer(baseURL);
});

test.afterAll(() => {
  server?.kill("SIGTERM");
});

async function register(page: Page) {
  await page.goto(baseURL);
  await expect(page).toHaveTitle("Porty");
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password").fill("correct horse battery staple");
  await page.getByRole("button", { name: "Create administrator" }).click();
  await expect(
    page.getByRole("heading", { name: "Configure the stack repository" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Create local repository" }).click();
  await expect(page.getByLabel("Git author name")).toHaveValue("Porty");
  await expect(page.getByLabel("Git author email")).toHaveValue(
    "porty@localhost",
  );
  await page.getByLabel("Initial branch").fill("stacks");
  await page.getByRole("button", { name: "Create repository" }).click();
  await expect(page.getByRole("heading", { name: "Stacks" })).toBeVisible();
}

test("appearance follows the operating system and a saved override", async ({
  page,
}, testInfo) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await page.goto(baseURL);
  await expect(page.getByRole("heading", { name: "Porty" })).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () => getComputedStyle(document.documentElement).backgroundColor,
      ),
    )
    .toBe("rgb(17, 24, 39)");
  await page.screenshot({ path: testInfo.outputPath("dark-auth.png") });

  await page.emulateMedia({ colorScheme: "light" });
  await expect
    .poll(() =>
      page.evaluate(
        () => getComputedStyle(document.documentElement).backgroundColor,
      ),
    )
    .toBe("rgb(255, 255, 255)");
  await page.evaluate(() => localStorage.setItem("porty-theme", "dark"));
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect
    .poll(() =>
      page.evaluate(
        () => getComputedStyle(document.documentElement).backgroundColor,
      ),
    )
    .toBe("rgb(17, 24, 39)");
});

test("administrator creates, edits, commits, and deploys a stack", async ({
  page,
}, testInfo) => {
  await page.route("**/api/v1/stacks/*/state", async (route) => {
    await route.fulfill({
      json: { runtime: "stopped", freshness: "never_deployed" },
    });
  });
  await page.route("**/api/v1/stacks/*/actions/deploy", async (route) => {
    await route.fulfill({
      status: 202,
      json: {
        id: "opr_e2e_deploy",
        kind: "deploy",
        scopeType: "stack",
        scopeId: "paperless",
        status: "succeeded",
        output: "deployment completed",
        outputTruncated: false,
      },
    });
  });
  await register(page);
  await page.getByRole("button", { name: "New stack" }).click();
  await page.getByLabel("Stack name").fill("paperless");
  await page.getByRole("button", { name: "Create stack", exact: true }).click();
  await expect(page.getByRole("heading", { name: "paperless" })).toBeVisible();
  await page.route("**/api/v1/stacks/*/actions/logs", (route) =>
    route.fulfill({
      status: 202,
      json: {
        id: "opr_e2e_logs",
        kind: "logs",
        scopeType: "stack",
        scopeId: "paperless",
        status: "succeeded",
        outputTruncated: false,
      },
    }),
  );
  const logRequest = page.waitForRequest("**/api/v1/stacks/*/actions/logs");
  await page.getByRole("tab", { name: "Logs" }).click();
  await logRequest;
  await expect(page.getByText("Waiting for container output…")).toBeVisible();
  await expect(page.getByRole("button", { name: "Load logs" })).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath("live-logs.png") });
  await page.getByRole("tab", { name: "Editor" }).click();
  const editor = page.getByRole("textbox", { name: "File contents" });
  await editor.fill("services:\n  web:\n    image: nginx:alpine\n");
  await page.getByRole("button", { name: "Save file" }).click();
  await expect(page.getByText("Saved", { exact: true })).toBeVisible();
  await page.getByLabel("Commit message").fill("Add paperless stack");
  await page.getByRole("button", { name: "Commit stack" }).click();
  await expect(page.getByRole("status")).toContainText("Committed paperless");
  await page.getByRole("button", { name: "Deploy" }).click();
  await expect(page.getByLabel("Operation details")).toBeVisible();
  await expect(page.getByLabel("Operation details")).toContainText(
    /succeeded|running/,
  );
  await page.getByRole("button", { name: "Close operation" }).click();
  await page.getByRole("link", { name: "Settings" }).click();
  await expect(page.getByRole("button", { name: "Add remote" })).toBeVisible();
  await page.getByLabel("Theme").selectOption("dark");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  for (const actionName of ["Fetch", "Pull", "Push"]) {
    const action = page.getByRole("button", { name: actionName });
    if (await action.count()) {
      await expect(action).toBeDisabled();
    }
  }
  await page.screenshot({
    path: testInfo.outputPath("dark-settings.png"),
    fullPage: false,
  });
  await page.getByRole("link", { name: "Stacks" }).click();
  await page.getByRole("link", { name: "paperless" }).click();
  await page.getByRole("tab", { name: "Editor" }).click();
  await expect(page.locator(".cm-editor")).toBeVisible();
  expect(
    await page
      .locator(".cm-editor")
      .evaluate((element) => getComputedStyle(element).backgroundColor),
  ).toBe("rgb(25, 34, 53)");
  await page.screenshot({
    path: testInfo.outputPath("dark-editor.png"),
    fullPage: false,
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
