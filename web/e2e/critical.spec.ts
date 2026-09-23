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

test("administrator creates, edits, commits, and deploys a stack", async ({
  page,
}, testInfo) => {
  await page.route("**/api/v1/stacks/*/state", async (route) => {
    await route.fulfill({ json: { runtime: "stopped", freshness: "never_deployed" } });
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
  for (const actionName of ["Fetch", "Pull", "Push"]) {
    const action = page.getByRole("button", { name: actionName });
    if (await action.count()) {
      await expect(action).toBeDisabled();
    }
  }
  await page.screenshot({
    path: testInfo.outputPath("critical-journey.png"),
    fullPage: false,
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
