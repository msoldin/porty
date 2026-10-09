import {
  test,
  expect,
  type Page,
  type TestInfo,
  type WebSocketRoute,
} from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Operation } from "../src/features/operations/types";

let server: ChildProcess, baseURL: string, dataDir: string;
test.beforeAll(async () => {
  dataDir = await mkdtemp(join(tmpdir(), "porty-overhaul-"));
  const port = 23000 + Math.floor(Math.random() * 1000);
  baseURL = `http://127.0.0.1:${port}`;
  server = spawn(
    process.env.PORTY_E2E_BINARY || join(tmpdir(), "porty-e2e"),
    ["--listen", `127.0.0.1:${port}`, "--data-dir", dataDir],
    { stdio: "ignore" },
  );
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(`${baseURL}/healthz`)).ok) return;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error("Porty did not start");
});
test.afterAll(async () => {
  if (server?.exitCode === null)
    await new Promise<void>((resolve) => {
      server.once("exit", () => resolve());
      server.kill("SIGTERM");
    });
  if (dataDir) await rm(dataDir, { recursive: true, force: true });
});

async function capture(page: Page, info: TestInfo, name: string) {
  for (const theme of ["light", "dark"]) {
    await page.evaluate(
      (value) => (document.documentElement.dataset.theme = value),
      theme,
    );
    await expect
      .poll(
        () =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        { message: `No page overflow: ${name}` },
      )
      .toBe(true);
    await page.screenshot({
      path: info.outputPath(`${name}-${theme}.png`),
      fullPage: true,
    });
  }
}
async function navigate(page: Page, name: string) {
  if (page.viewportSize()!.width < 768)
    await page.getByRole("button", { name: "Navigation", exact: true }).click();
  await page
    .getByRole("navigation", { name: "Main navigation" })
    .getByRole("link", { name, exact: true })
    .click();
}
async function fixture(page: Page) {
  const state = {
    content: "services:\n  web:\n    image: nginx:alpine\n",
    revision: "a".repeat(64),
    rejectReview: true,
    writes: [] as string[],
    operations: [] as Operation[],
    sockets: [] as WebSocketRoute[],
    sequence: 0,
    auditError: false,
  };
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.routeWebSocket("**/api/v1/stream", (socket) => {
    socket.onMessage((message) => {
      if (JSON.parse(String(message)).topic === "operations")
        state.sockets.push(socket);
    });
  });
  const stacks = [
    {
      id: "monitoring",
      directoryName: "monitoring",
      composeProjectName: "porty-monitoring",
    },
    {
      id: "sleeping",
      directoryName: "games",
      composeProjectName: "porty-games",
    },
    {
      id: "new",
      directoryName:
        "new-stack-with-a-very-long-name-for-configuration-and-observability",
      composeProjectName: "porty-new",
    },
  ];
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname.replace("/api/v1", "");
    if (request.method() !== "GET") state.writes.push(path);
    const id = path.split("/")[2];
    if (path.endsWith("/deployment-review"))
      return route.fulfill({
        json: {
          stackId: id,
          sourceRevision: state.revision,
          uncommittedChanges: true,
        },
      });
    if (path.endsWith("/actions/deploy")) {
      expect(request.headers()["if-match"]).toBe(`"${state.revision}"`);
      if (state.rejectReview) {
        state.rejectReview = false;
        state.revision = "b".repeat(64);
        return route.fulfill({
          status: 412,
          json: {
            error: {
              code: "DeploymentReviewChanged",
              message: "Configuration changed",
            },
          },
        });
      }
      const operation: Operation = {
        id: "op-one",
        kind: "deploy",
        scopeType: "stack",
        scopeId: id,
        status: "queued",
        outputTruncated: false,
      };
      state.operations = [operation];
      return route.fulfill({ status: 202, json: operation });
    }
    if (path.endsWith("/files")) {
      if (request.method() === "PUT") {
        expect(request.headers()["if-match"]).toBe('"file-hash"');
        state.content = request.postDataJSON().content;
        return route.fulfill({ json: { hash: "next-hash" } });
      }
      return route.fulfill({
        json: {
          path: "docker-compose.yml",
          content: state.content,
          hash: "file-hash",
          size: state.content.length,
        },
      });
    }
    if (path.endsWith("/tree"))
      return route.fulfill({
        json: [
          { path: "docker-compose.yml", isDirectory: false, editable: true },
        ],
      });
    if (path.endsWith("/diff"))
      return route.fulfill({ json: { diff: "+    image: nginx:alpine" } });
    if (path.endsWith("/state"))
      return route.fulfill({
        json: {
          runtime:
            id === "sleeping"
              ? "sleeping"
              : id === "new"
                ? "stopped"
                : "unhealthy",
          freshness:
            id === "new"
              ? "never_deployed"
              : id === "monitoring"
                ? "changes_pending"
                : "current",
          hasDeployed: id !== "new",
        },
      });
    if (path.endsWith("/containers"))
      return route.fulfill({
        json:
          id === "new"
            ? []
            : [
                {
                  id: `${id}-web`,
                  name: `${id}-web-1`,
                  service: "web",
                  state: id === "sleeping" ? "exited" : "running",
                  onDemandSleeping: id === "sleeping",
                  health: "",
                  image: "nginx:alpine",
                  ports: [],
                  networks: [],
                },
              ],
      });
    if (path.endsWith("/logs"))
      return route.fulfill({
        json: {
          output: "Application ready\nUpstream connection refused",
          truncated: false,
        },
      });
    if (path.endsWith("/auto-update"))
      return route.fulfill({
        json: {
          policy: {
            enabled: true,
            expression: "0 0 * * *",
            revision: 2,
            nextRunAt: "2026-10-10T00:00:00Z",
            pausedReason: "recovery_required",
          },
          available: true,
          eligible: false,
          eligibilityReason: "Stack is unhealthy",
          excluded: {},
        },
      });
    if (path.endsWith("/environment"))
      return route.fulfill({ json: { keys: [] } });
    if (path === "/audit" && state.auditError)
      return route.fulfill({
        status: 503,
        json: {
          error: { code: "Unavailable", message: "Audit storage unavailable" },
        },
      });
    const responses: Record<string, unknown> = {
      "/session": { username: "Administrator", csrfToken: "fixture" },
      "/repository/setup/status": {
        state: "ready",
        required: false,
        pathState: "worktree",
        modes: [],
        branch: "main",
        author: { name: "Porty", email: "porty@localhost" },
        defaultAuthor: { name: "Porty", email: "porty@localhost" },
        ssh: { usable: false },
        managedRemote: {
          name: "origin",
          url: "https://example.com/team/stacks.git",
          authType: "none",
          managed: true,
        },
      },
      "/repository/status": {
        configured: true,
        branch: "main",
        dirty: true,
        ahead: 1,
        behind: 0,
        paths: ["monitoring/docker-compose.yml"],
      },
      "/repository/history": [],
      "/stacks": stacks,
      "/operations": state.operations,
      "/audit": [],
      "/alerts": { items: [], total: 0, unacknowledgedCount: 0 },
    };
    if (path in responses) return route.fulfill({ json: responses[path] });
    if (path.endsWith("/deployments") || path.endsWith("/on-demand"))
      return route.fulfill({ json: [] });
    throw new Error(`Unhandled fixture request: ${request.method()} ${path}`);
  });
  return {
    state,
    errors,
    sendStatus: (status: string, output = "") => {
      const operation = { ...state.operations[0], status, output };
      state.operations = [operation];
      for (const socket of state.sockets)
        socket.send(
          JSON.stringify({
            type: "operation",
            subscriptionId: "operations",
            sequence: ++state.sequence,
            payload: operation,
          }),
        );
    },
  };
}

test("reviews saved edits, rejects stale confirmation, and follows real operation statuses", async ({
  page,
}, info) => {
  const { state, errors, sendStatus } = await fixture(page);
  await page.goto(`${baseURL}/#/stacks/monitoring`);
  await page.getByRole("tab", { name: "Compose & files", exact: true }).click();
  const editor = page.getByRole("textbox", { name: "File contents" });
  await expect(editor).toContainText("nginx:alpine");
  await editor.fill("services:\n  web:\n    image: nginx:latest\n");
  const deploy = page.getByRole("button", {
    name: "Deploy changes…",
    exact: true,
  });
  await deploy.click();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(editor).toContainText("nginx:latest");
  expect(state.writes).toEqual([]);
  await deploy.click();
  await page.getByRole("button", { name: "Discard and continue" }).click();
  await expect(
    page.getByRole("dialog", { name: "Deploy monitoring?" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(editor).toContainText("nginx:alpine");
  await editor.fill("services:\n  web:\n    image: nginx:latest\n");
  await deploy.click();
  await page.getByRole("button", { name: "Save and continue" }).click();
  const dialog = page.getByRole("dialog", { name: "Deploy monitoring?" });
  await expect(
    dialog.getByRole("button", { name: "Deploy stack", exact: true }),
  ).toBeEnabled();
  expect(state.writes).toEqual(["/stacks/monitoring/files"]);
  await capture(page, info, "deployment-review");
  await dialog
    .getByRole("button", { name: "Deploy stack", exact: true })
    .click();
  await expect(dialog).toContainText("Saved configuration changed");
  expect(state.writes.filter((path) => path.endsWith("/deploy"))).toHaveLength(
    1,
  );
  await dialog
    .getByRole("button", { name: "Deploy stack", exact: true })
    .click();
  const details = page.getByLabel("Operation details", { exact: true });
  await expect(details).toContainText("Queued");
  await expect.poll(() => state.sockets.length).toBeGreaterThan(0);
  sendStatus("running", "Pulling image");
  await expect(details).toContainText("Running");
  sendStatus("failed", "Image pull failed");
  await expect(details).toContainText("Failed");
  await capture(page, info, "failed-operation");
  await page.getByRole("button", { name: "Close operation" }).click();
  await navigate(page, "Operations");
  await page.getByRole("button", { name: "deploy", exact: true }).click();
  await expect(details).toContainText("Image pull failed");
  sendStatus("succeeded", "Deployment verified");
  await expect(details).toContainText("Succeeded");
  await page.getByRole("button", { name: "Close operation" }).click();
  expect(errors).toEqual([]);
});

test("keeps long names, editor, repository and settings usable across themes and narrow screens", async ({
  page,
}, info) => {
  const { errors } = await fixture(page);
  await page.goto(baseURL);
  await expect(
    page
      .getByRole("region", { name: "Stacks table" })
      .getByText("Never deployed", { exact: true }),
  ).toBeVisible();
  await expect(
    page
      .getByRole("region", { name: "Stacks table" })
      .getByText("Uncommitted files", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("No health check").first()).toBeVisible();
  await capture(page, info, "stacks");
  await page
    .getByRole("region", { name: "Stacks table" })
    .getByRole("link", { name: "monitoring", exact: true })
    .click();
  await capture(page, info, "stack-detail");
  const colors = await page
    .getByRole("button", { name: "Deploy changes…", exact: true })
    .evaluate((element) => ({
      bg: getComputedStyle(element).backgroundColor,
      fg: getComputedStyle(element).color,
    }));
  expect(colors).toEqual({ bg: "rgb(37, 107, 183)", fg: "rgb(255, 255, 255)" });
  await page.getByRole("link", { name: "Open monitoring-web-1" }).click();
  await page.getByRole("tab", { name: "Logs", exact: true }).click();
  await expect(page.getByText(/Upstream connection refused/)).toBeVisible();
  await capture(page, info, "container-logs");
  await page
    .getByRole("navigation", { name: "Breadcrumb" })
    .getByRole("link", { name: "monitoring", exact: true })
    .click();
  await page.getByRole("tab", { name: "Compose & files", exact: true }).click();
  await expect(
    page.getByRole("textbox", { name: "File contents" }),
  ).toContainText("nginx:alpine");
  await capture(page, info, "editor");
  await page.getByRole("tab", { name: "Settings", exact: true }).click();
  await expect(page.getByText("Execution: Paused")).toBeVisible();
  await capture(page, info, "stack-settings");
  await navigate(page, "Repository");
  await expect(
    page.getByRole("button", { name: "Push", exact: true }),
  ).toBeEnabled();
  await capture(page, info, "repository");
  await page.getByRole("button", { name: "Push", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Cancel", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("button", { name: "Push", exact: true }),
  ).toBeFocused();
  await navigate(page, "Settings");
  await expect(page.getByRole("region", { name: "Account" })).toBeVisible();
  await capture(page, info, "account-settings");
  if (info.project.name === "mobile") {
    await page.setViewportSize({ width: 320, height: 844 });
    await capture(page, info, "account-settings-320");
    await navigate(page, "Stacks");
    await capture(page, info, "long-stacks-320");
    await page
      .getByRole("region", { name: "Stacks table" })
      .getByRole("link", { name: "monitoring", exact: true })
      .click();
    await page.getByRole("tab", { name: "Settings", exact: true }).click();
    await capture(page, info, "stack-settings-320");
    await page
      .getByRole("tab", { name: "Compose & files", exact: true })
      .click();
    await expect(
      page.getByRole("textbox", { name: "File contents" }),
    ).toContainText("nginx:alpine");
    await capture(page, info, "editor-320");
  }
  expect(errors).toEqual([]);
});

test("distinguishes failed audit loading from an empty history", async ({
  page,
}, info) => {
  const { state, errors } = await fixture(page);
  state.auditError = true;
  await page.goto(`${baseURL}/#/audit`);
  await expect(page.getByText(/Audit log could not be loaded/)).toBeVisible();
  await expect(page.getByText("No audit events recorded.")).toHaveCount(0);
  await capture(page, info, "audit-error");
  state.auditError = false;
  await page.getByRole("button", { name: "Retry audit log" }).click();
  await expect(page.getByText("No audit events recorded.")).toBeVisible();
  await capture(page, info, "audit-empty");
  expect(errors).toEqual([]);
});
test("keeps long stack names inside their cells and all repository controls reachable", async ({
  page,
}) => {
  await fixture(page);
  await page.goto(baseURL);
  const name = page
    .getByRole("region", { name: "Stacks table" })
    .getByRole("link", {
      name: "new-stack-with-a-very-long-name-for-configuration-and-observability",
      exact: true,
    });
  await expect(name).toBeVisible();
  expect(
    await name.evaluate(
      (element) => element.scrollWidth <= element.clientWidth + 1,
    ),
  ).toBe(true);
  await navigate(page, "Repository");
  for (const action of ["Fetch", "Pull", "Push"])
    await expect(
      page.getByRole("button", { name: action, exact: true }),
    ).toBeVisible();
});
test("shows loading honestly while inventory is pending", async ({
  page,
}, info) => {
  await fixture(page);
  let release!: () => void;
  const ready = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/v1/stacks", async (route) => {
    await ready;
    await route.fulfill({ json: [] });
  });
  await page.goto(baseURL);
  await expect(page.getByText("Loading stacks…")).toBeVisible();
  await capture(page, info, "inventory-loading");
  release();
  await expect(page.getByText("Loading stacks…")).toHaveCount(0);
  await capture(page, info, "inventory-empty");
});
test("keeps the mobile Save control unobscured and the dark alert count readable", async ({
  page,
}) => {
  await fixture(page);
  await page.route("**/api/v1/alerts?*", (route) =>
    route.fulfill({ json: { items: [], total: 1, unacknowledgedCount: 1 } }),
  );
  await page.setViewportSize({ width: 320, height: 844 });
  await page.goto(`${baseURL}/#/stacks/monitoring`);
  await page.getByRole("tab", { name: "Compose & files", exact: true }).click();
  await page.getByRole("textbox", { name: "File contents" }).fill("edited");
  const save = page.getByRole("button", { name: "Save file" });
  await save.scrollIntoViewIfNeeded();
  expect(
    await save.evaluate((element) => {
      const button = element.getBoundingClientRect();
      const pane = element.parentElement!.getBoundingClientRect();
      return button.top >= pane.top && button.bottom <= pane.bottom;
    }),
  ).toBe(true);
  await page.evaluate(() => (document.documentElement.dataset.theme = "dark"));
  await page.getByRole("button", { name: "Navigation", exact: true }).click();
  const count = page.getByLabel("1 unacknowledged alerts");
  await expect(count).toBeVisible();
  const contrast = await count.evaluate((element) => {
    const style = getComputedStyle(element);
    const luminance = (color: string) => {
      const values = color
        .match(/[\d.]+/g)!
        .slice(0, 3)
        .map(Number)
        .map((value) => value / 255)
        .map((value) =>
          value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4,
        );
      return values[0] * 0.2126 + values[1] * 0.7152 + values[2] * 0.0722;
    };
    const values = [
      luminance(style.color),
      luminance(style.backgroundColor),
    ].sort((a, b) => b - a);
    return (values[0] + 0.05) / (values[1] + 0.05);
  });
  expect(contrast).toBeGreaterThanOrEqual(4.5);
});
test("preserves theme through registration setup and password-change sign out", async ({
  page,
}, info) => {
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto(baseURL);
  await page.evaluate(() => localStorage.setItem("porty-theme", "dark"));
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  if (info.project.name === "mobile")
    await page.setViewportSize({ width: 320, height: 844 });
  await capture(page, info, "registration");
  await page.getByLabel("Username").fill("admin");
  await page
    .getByLabel("Password", { exact: true })
    .fill("correct horse battery staple");
  await page.getByRole("button", { name: "Create administrator" }).click();
  await expect(
    page.getByRole("heading", { name: "Configure the stack repository" }),
  ).toBeVisible();
  await capture(page, info, "setup-choices");
  await page.getByRole("button", { name: "Create local repository" }).click();
  await capture(page, info, "setup-local");
  await page
    .getByRole("button", { name: "Create repository", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Stacks", level: 1 }),
  ).toBeVisible();
  await navigate(page, "Settings");
  await page
    .getByLabel("Current password")
    .fill("correct horse battery staple");
  await page.getByLabel("New password").fill("updated horse battery staple");
  await page
    .getByRole("button", { name: "Change password", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Welcome back" }),
  ).toBeVisible();
  await capture(page, info, "login");
  await page.getByLabel("Username").fill("admin");
  await page
    .getByLabel("Password", { exact: true })
    .fill("updated horse battery staple");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Settings", level: 1 }),
  ).toBeVisible();
});

test("keeps runtime and deployment readable at 320px and opens container logs directly", async ({
  page,
}) => {
  await fixture(page);
  await page.setViewportSize({ width: 320, height: 844 });
  await page.goto(baseURL);
  const table = page.getByRole("region", { name: "Stacks table" });
  for (const text of ["Sleeping", "Current", "Never deployed"]) {
    const badge = table.getByText(text, { exact: true });
    await badge.scrollIntoViewIfNeeded();
    expect(
      await badge.evaluate((el) => {
        const r = el.getBoundingClientRect();
        const hit = document.elementFromPoint(
          r.x + r.width / 2,
          r.y + r.height / 2,
        );
        return (
          r.left >= 0 && r.right <= innerWidth && !!hit && el.contains(hit)
        );
      }),
    ).toBe(true);
  }
  await page
    .getByRole("checkbox", { name: "Select all visible stacks" })
    .check();
  await expect(
    page.getByText("3 stacks selected", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Clear selection" }).click();
  const summary = page.getByRole("region", { name: "Repository summary" });
  expect(
    await summary.evaluate(
      (el) =>
        el.getBoundingClientRect().top >
        document.querySelector(".stack-table")!.getBoundingClientRect().bottom,
    ),
  ).toBe(true);
  await page
    .getByRole("link", { name: "View logs for monitoring-web-1" })
    .click();
  await expect(
    page.getByRole("tab", { name: "Logs", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await expect(page.getByText(/Upstream connection refused/)).toBeVisible();
});

test("fills the available workspace with the file editor and resizes its panes", async ({
  page,
}, info) => {
  const { state, errors } = await fixture(page);
  state.content =
    "services:\n" +
    Array.from({ length: 200 }, (_, i) => `  # configuration line ${i}`).join(
      "\n",
    );
  await page.setViewportSize({ width: 1536, height: 900 });
  await page.goto(`${baseURL}/#/stacks/monitoring`);
  await page.getByRole("tab", { name: "Compose & files", exact: true }).click();
  await expect(
    page.getByRole("textbox", { name: "File contents" }),
  ).toBeVisible();
  const editor = page.getByRole("region", { name: "File editor", exact: true });
  let previousHeight = 0;
  for (const height of [900, 1200]) {
    await page.setViewportSize({ width: 1536, height });
    await expect
      .poll(async () => {
        const rect = await editor.boundingBox();
        return Math.abs(rect!.y + rect!.height - height);
      })
      .toBeLessThanOrEqual(2);
    const rect = (await editor.boundingBox())!;
    if (previousHeight)
      expect(rect.height - previousHeight).toBeCloseTo(300, 0);
    previousHeight = rect.height;
    await expect(
      page.getByRole("button", { name: "Commit stack", exact: true }),
    ).toBeInViewport();
    expect(
      await page
        .locator(".cm-scroller")
        .evaluate((el) => el.scrollHeight > el.clientHeight),
    ).toBe(true);
    await capture(page, info, `editor-height-${height}`);
  }
  for (const width of [1100, 1024, 768, 390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    expect((await editor.boundingBox())!.height).toBeGreaterThanOrEqual(384);
    await page
      .getByRole("button", { name: "Commit stack", exact: true })
      .scrollIntoViewIfNeeded();
    await expect(
      page.getByRole("button", { name: "Commit stack", exact: true }),
    ).toBeInViewport();
    await capture(page, info, `editor-width-${width}`);
  }
  expect(errors).toEqual([]);
});
