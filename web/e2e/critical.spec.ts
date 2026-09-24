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
  await page.emulateMedia({ colorScheme: "light" });
  const initialViewport = page.viewportSize()!;
  let stoppedStackId = "";
  let rejectDeployId = "";
  await page.route("**/api/v1/stacks/*/state", async (route) => {
    const stopped =
      stoppedStackId &&
      route
        .request()
        .url()
        .includes("/stacks/" + stoppedStackId + "/state");
    await route.fulfill({
      json: {
        runtime: stopped ? "stopped" : "running",
        freshness: "current",
        hasDeployed: true,
      },
    });
  });
  await page.route("**/api/v1/stacks/*/containers", (route) =>
    route.fulfill({
      json: [
        {
          id: "full-id-a",
          name: "web-1",
          service: "web",
          state: "running",
          health: "healthy",
          image: "ghcr.io/example/web:1.4",
          networks: ["front", "back"],
          ports: [
            {
              host: "127.0.0.1",
              publishedPort: 8080,
              targetPort: 80,
              protocol: "tcp",
            },
          ],
        },
        {
          id: "full-id-b",
          name: "web-2",
          service: "web",
          state: "running",
          health: "healthy",
          image: "ghcr.io/example/web:1.4",
          networks: ["front"],
          ports: [
            {
              host: "::1",
              publishedPort: 8443,
              targetPort: 443,
              protocol: "tcp",
            },
          ],
        },
        {
          id: "full-id-c",
          name: "worker-1",
          service: "worker",
          state: "exited",
          health: "",
          image: "worker:2.0",
          networks: [],
          ports: [],
        },
      ],
    }),
  );
  await page.route("**/api/v1/stacks/*/containers/actions/stop", (route) =>
    route.fulfill({
      status: 202,
      json: {
        id: "opr_e2e_container",
        kind: "container_batch_stop",
        scopeType: "stack",
        scopeId: "paperless",
        status: "queued",
        outputTruncated: false,
      },
    }),
  );
  await page.route("**/api/v1/stacks/*/actions/deploy", async (route) => {
    const scopeId = route
      .request()
      .url()
      .split("/actions/")[0]
      .split("/")
      .pop()!;
    if (scopeId === rejectDeployId) {
      await route.fulfill({
        status: 409,
        json: {
          error: {
            code: "OperationConflict",
            message: "A conflicting operation is in progress",
          },
        },
      });
      return;
    }
    await route.fulfill({
      status: 202,
      json: {
        id: "opr_e2e_deploy_" + scopeId,
        kind: "deploy",
        scopeType: "stack",
        scopeId,
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
  const browserProblems: string[] = [];
  page.on("console", (entry) => {
    if (entry.type() === "error" || entry.type() === "warning")
      browserProblems.push(entry.text());
  });
  page.on("pageerror", (error) => browserProblems.push(error.message));
  await expect(page.getByRole("heading", { name: "paperless" })).toBeVisible();
  await expect(
    page.getByRole("checkbox", { name: "Select web-2" }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "Services" })).toBeVisible();
  await page.getByRole("button", { name: "Actions" }).click();
  await expect(page.getByRole("menuitem", { name: "Stop" })).toHaveAttribute(
    "aria-disabled",
    "false",
  );
  await expect(page.getByRole("menuitem", { name: "Restart" })).toHaveAttribute(
    "aria-disabled",
    "false",
  );
  await page.getByRole("menu").press("Escape");
  for (const label of [
    "Refresh status",
    "Start stack",
    "Pull images",
    "Recreate containers",
  ])
    await expect(page.getByRole("button", { name: label })).toHaveCount(0);
  await page.screenshot({
    path: testInfo.outputPath("stack-services.png"),
    fullPage: true,
  });
  await page.route("**/api/v1/stacks/*/containers/actions/restart", (route) =>
    route.fulfill({
      status: 202,
      json: {
        id: "opr_e2e_restart",
        kind: "container_batch_restart",
        scopeType: "stack",
        scopeId: "paperless",
        status: "succeeded",
        outputTruncated: false,
      },
    }),
  );
  const restartRequest = page.waitForRequest(
    "**/api/v1/stacks/*/containers/actions/restart",
  );
  await page.getByRole("checkbox", { name: "Select web-1" }).check();
  await page.getByRole("checkbox", { name: "Select web-2" }).check();
  await page.getByRole("button", { name: "Restart selected" }).click();
  expect((await restartRequest).postDataJSON()).toEqual({
    containerIds: ["full-id-a", "full-id-b"],
  });
  await expect(page.getByLabel("Operation details")).toBeVisible();
  await page.getByRole("button", { name: "Close operation" }).click();
  await page.getByRole("checkbox", { name: "Select web-1" }).uncheck();
  const containerRequest = page.waitForRequest(
    "**/api/v1/stacks/*/containers/actions/stop",
  );
  await page.getByRole("checkbox", { name: "Select web-2" }).check();
  await page.getByRole("button", { name: "Actions" }).click();
  await page.screenshot({
    path: testInfo.outputPath("services-selected-light.png"),
    fullPage: true,
  });
  await page.getByRole("menu").press("Escape");
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Stop selected" }).click();
  expect((await containerRequest).postDataJSON()).toEqual({
    containerIds: ["full-id-b"],
  });
  await expect(page.getByLabel("Operation details")).toBeVisible();
  await page.getByRole("button", { name: "Close operation" }).click();
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
  await page.getByRole("button", { name: "Actions" }).click();
  await page.getByRole("menuitem", { name: "Deploy" }).click();
  await expect(page.getByLabel("Operation details")).toBeVisible();
  await expect(page.getByLabel("Operation details")).toContainText(
    /succeeded|running/,
  );
  await page.getByRole("button", { name: "Close operation" }).click();
  await page.getByRole("link", { name: "All stacks" }).click();
  await page.getByRole("button", { name: "New stack" }).click();
  await page.getByLabel("Stack name").fill("monitoring");
  await page.getByRole("button", { name: "Create stack", exact: true }).click();
  await page.getByRole("link", { name: "All stacks" }).click();
  stoppedStackId = (await page
    .getByRole("link", { name: "monitoring" })
    .getAttribute("href"))!
    .split("/")
    .pop()!;
  rejectDeployId = stoppedStackId;
  await page.reload();
  await expect(
    page.getByRole("link", { name: "monitoring" }).locator("xpath=../.."),
  ).toContainText("STOPPED");
  await page.getByRole("checkbox", { name: "Select paperless" }).check();
  await page.getByRole("checkbox", { name: "Select monitoring" }).check();
  await page.getByRole("button", { name: "Actions" }).click();
  await expect(page.getByRole("menuitem", { name: "Restart" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await expect(page.getByRole("menuitem", { name: "Stop" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await page.getByRole("menuitem", { name: "Deploy" }).click();
  await expect(page.getByRole("status")).toContainText("paperless: accepted");
  await expect(page.getByRole("status")).toContainText(
    "monitoring: A conflicting operation is in progress",
  );
  await page.getByRole("checkbox", { name: "Select paperless" }).check();
  await page.getByRole("button", { name: "Actions" }).click();
  await page.screenshot({
    path: testInfo.outputPath("stacks-selected-light.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 320, height: 844 });
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      ),
    )
    .toBeLessThanOrEqual(0);
  await page.screenshot({
    path: testInfo.outputPath("stacks-selected-light-320.png"),
    fullPage: true,
  });
  await page
    .getByRole("region", { name: "Stacks table" })
    .evaluate((element) => {
      element.scrollLeft = element.scrollWidth;
    });
  await expect(page.getByRole("link", { name: "paperless" })).toBeInViewport();
  await page.getByRole("menu").press("Escape");
  await page.getByRole("link", { name: "paperless" }).click();
  await page.getByRole("checkbox", { name: "Select web-2" }).check();
  await page.getByRole("button", { name: "Actions" }).click();
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      ),
    )
    .toBeLessThanOrEqual(0);
  await page.screenshot({
    path: testInfo.outputPath("services-selected-light-320.png"),
    fullPage: true,
  });
  await page
    .getByRole("region", { name: "Services table" })
    .scrollIntoViewIfNeeded();
  await page
    .getByRole("region", { name: "Services table" })
    .evaluate((element) => {
      element.scrollLeft = element.scrollWidth;
    });
  await expect(
    page
      .getByRole("checkbox", { name: "Select web-2" })
      .locator("xpath=../..")
      .locator(".service-identity"),
  ).toBeInViewport();
  await page.getByRole("menu").press("Escape");
  await page.setViewportSize(initialViewport);
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
  await page.getByRole("checkbox", { name: "Select paperless" }).check();
  await page.getByRole("button", { name: "Actions" }).click();
  await page.screenshot({
    path: testInfo.outputPath("stacks-selected-dark.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 320, height: 844 });
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      ),
    )
    .toBeLessThanOrEqual(0);
  await page.screenshot({
    path: testInfo.outputPath("stacks-selected-dark-320.png"),
    fullPage: true,
  });
  await page.getByRole("menu").press("Escape");
  await page.getByRole("link", { name: "paperless" }).click();
  await page.getByRole("checkbox", { name: "Select web-2" }).check();
  await page.getByRole("button", { name: "Actions" }).click();
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      ),
    )
    .toBeLessThanOrEqual(0);
  await page.screenshot({
    path: testInfo.outputPath("services-selected-dark-320.png"),
    fullPage: true,
  });
  await page.getByRole("menu").press("Escape");
  await page.setViewportSize(initialViewport);
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
  expect(
    browserProblems.filter((problem) => !problem.includes("409 (Conflict)")),
  ).toEqual([]);
});
