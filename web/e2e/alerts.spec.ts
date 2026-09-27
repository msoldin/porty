import { test, expect } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Alert } from "../src/features/alerts/types";
let server: ChildProcess;
let baseURL: string;
test.beforeAll(async () => {
  const port = 20000 + Math.floor(Math.random() * 1000);
  baseURL = `http://127.0.0.1:${port}`;
  const dir = await mkdtemp(join(tmpdir(), "porty-alerts-"));
  server = spawn(
    process.env.PORTY_E2E_BINARY || join(tmpdir(), "porty-e2e"),
    ["--listen", `127.0.0.1:${port}`, "--data-dir", dir],
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
test.afterAll(() => server?.kill("SIGTERM"));
test("reviews alerts globally and on a stack without changing runtime state", async ({
  page,
}, info) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  let item: Alert = {
    id: "a1",
    key: { stackId: "s1", problem: "deployment", target: "stack" },
    stackName: "paperless",
    revision: 1,
    episode: 1,
    count: 1,
    summary: "Deployment verification failed",
    operationId: "old-op",
    firstAt: "2026-09-27T00:00:00Z",
    latestAt: "2026-09-27T00:00:00Z",
    resolvedAt: "2026-09-27T00:02:00Z",
    canResolveManually: true,
  };
  let stale = true;
  const mutations: string[] = [];
  await page.routeWebSocket("**/api/v1/stream", (socket) =>
    socket.onMessage(() => {}),
  );
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace("/api/v1", "");
    if (route.request().method() !== "GET") mutations.push(path);
    if (path === "/alerts/a1/acknowledge") {
      if (stale) {
        stale = false;
        item = { ...item, revision: 2, count: 2 };
        return route.fulfill({
          status: 409,
          json: { error: { code: "AlertConflict", message: "Alert changed" } },
        });
      }
      item = {
        ...item,
        revision: 3,
        acknowledgedAt: "2026-09-27T01:00:00Z",
        acknowledgedBy: "admin",
      };
      return route.fulfill({ json: item });
    }
    if (path === "/alerts") {
      const included =
        url.searchParams.get("view") === "all" ||
        !item.acknowledgedAt ||
        !item.resolvedAt;
      return route.fulfill({
        json: {
          items: included ? [item] : [],
          total: included ? 1 : 0,
          unacknowledgedCount: item.acknowledgedAt ? 0 : 1,
        },
      });
    }
    const responses: Record<string, unknown> = {
      "/session": { username: "admin", csrfToken: "csrf" },
      "/repository/setup/status": {
        state: "ready",
        required: false,
        pathState: "worktree",
        modes: [],
      },
      "/repository/status": {
        configured: true,
        branch: "main",
        dirty: false,
        ahead: 0,
        behind: 0,
        paths: [],
      },
      "/repository/history": [],
      "/operations": [],
      "/audit": [],
      "/stacks": [
        {
          id: "s1",
          directoryName: "paperless",
          composeProjectName: "porty-paperless",
        },
      ],
      "/stacks/s1/state": {
        runtime: "running",
        freshness: "current",
        hasDeployed: true,
      },
      "/stacks/s1/containers": [],
      "/stacks/s1/deployments": [],
      "/operations/old-op": {
        id: "old-op",
        kind: "deploy",
        scopeId: "s1",
        status: "failed",
        output: "Historical failure output",
      },
      "/alerts/a1": {
        alert: item,
        history: [
          {
            id: "e1",
            alertId: "a1",
            episode: 1,
            kind: "failure",
            at: item.firstAt,
            operationId: "old-op",
          },
        ],
      },
    };
    if (!(path in responses)) throw new Error(`Unexpected request ${path}`);
    return route.fulfill({ json: responses[path] });
  });
  await page.goto(`${baseURL}/#/alerts`);
  await expect(page).toHaveTitle("Porty");
  await expect(page).toHaveURL(/#\/alerts$/);
  await expect(
    page.getByRole("heading", { name: "Alerts", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("1 unacknowledged alerts")).toBeVisible();
  await expect(page.getByText("Resolved", { exact: true })).toBeVisible();
  await page.screenshot({
    path: join(tmpdir(), `porty-alerts-${info.project.name}.png`),
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "View operation", exact: true })
    .click();
  await expect(page.getByText("Historical failure output")).toBeVisible();
  await page.getByRole("button", { name: "Close operation" }).click();
  await page.getByRole("button", { name: "History", exact: true }).click();
  await expect(
    page.getByRole("region", { name: "Alert history" }),
  ).toContainText("failure");
  await page.getByRole("button", { name: "Close history" }).click();
  await page.getByRole("button", { name: "Acknowledge" }).click();
  await expect(page.getByText(/This alert changed/)).toBeVisible();
  await page.getByRole("button", { name: "Acknowledge" }).click();
  await expect(page.getByText("No alerts in this view.")).toBeVisible();
  await expect(page.getByLabel("1 unacknowledged alerts")).toHaveCount(0);
  await page.getByLabel("Show").selectOption("all");
  await expect(page.getByText(/Acknowledged by admin/)).toBeVisible();
  await page.getByRole("link", { name: "paperless", exact: true }).click();
  await page.getByRole("tab", { name: "Alerts" }).click();
  await page.getByLabel("Show").selectOption("all");
  await expect(page.getByText(/Acknowledged by admin/)).toBeVisible();
  expect(mutations).toEqual([
    "/alerts/a1/acknowledge",
    "/alerts/a1/acknowledge",
  ]);
  expect(errors).toEqual([]);
  expect(await page.locator("vite-error-overlay").count()).toBe(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
