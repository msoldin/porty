import { test, expect } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { AutoUpdateStatus } from "../src/features/stacks/autoUpdateTypes";
let server: ChildProcess;
let baseURL: string;
test.beforeAll(async () => {
  const port = 20000 + Math.floor(Math.random() * 1000);
  baseURL = `http://127.0.0.1:${port}`;
  const dir = await mkdtemp(join(tmpdir(), "porty-auto-update-"));
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

test("edits UTC policy and keeps acknowledgment separate from verified resume", async ({
  page,
}, info) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const writes: string[] = [];
  let recovered = false;
  let acknowledged = false;
  let status: AutoUpdateStatus = {
    policy: {
      stackId: "s1",
      enabled: false,
      expression: "0 0 * * *",
      revision: 0,
      nextRunAt: "2026-09-28T00:00:00Z",
    },
    available: true,
    eligible: true,
    excluded: { builder: "locally built service" },
  };
  await page.routeWebSocket("**/api/v1/stream", (socket) =>
    socket.onMessage(() => {}),
  );
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname.replace("/api/v1", "");
    if (request.method() !== "GET") writes.push(path);
    if (path === "/stacks/s1/auto-update") {
      if (request.method() === "PUT") {
        const input = request.postDataJSON();
        expect(input).toEqual({
          enabled: true,
          expression: "0 0 * * *",
          expectedRevision: 0,
        });
        status = {
          ...status,
          policy: { ...status.policy, ...input, revision: 1 },
        };
      }
      return route.fulfill({ json: status });
    }
    if (path === "/stacks/s1/auto-update/resume") {
      expect(recovered).toBe(true);
      expect(request.postDataJSON()).toEqual({ expectedRevision: 2 });
      status = {
        ...status,
        policy: { ...status.policy, pausedReason: undefined, revision: 3 },
      };
      return route.fulfill({ json: status });
    }
    if (path === "/stacks/s1/actions/deploy") {
      recovered = true;
      return route.fulfill({
        status: 202,
        json: {
          id: "manual-recovery",
          kind: "deploy",
          scopeId: "s1",
          status: "succeeded",
          output: "Recovered manually",
        },
      });
    }
    if (path === "/alerts/a1/acknowledge") {
      acknowledged = true;
      return route.fulfill({ json: { id: "a1", revision: 2 } });
    }
    if (path === "/alerts")
      return route.fulfill({
        json: {
          items: status.policy.pausedReason
            ? [
                {
                  id: "a1",
                  key: {
                    stackId: "s1",
                    problem: "deployment",
                    target: "stack",
                  },
                  stackName: "paperless",
                  revision: acknowledged ? 2 : 1,
                  episode: 1,
                  count: 1,
                  summary: "Automatic update failed",
                  firstAt: "2026-09-27T00:00:00Z",
                  latestAt: "2026-09-27T00:00:00Z",
                  acknowledgedAt: acknowledged
                    ? "2026-09-27T01:00:00Z"
                    : undefined,
                  acknowledgedBy: acknowledged ? "admin" : undefined,
                  canResolveManually: true,
                },
              ]
            : [],
          total: status.policy.pausedReason ? 1 : 0,
          unacknowledgedCount:
            status.policy.pausedReason && !acknowledged ? 1 : 0,
        },
      });
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
      "/stacks/s1/environment": { keys: [] },
      "/stacks/s1/on-demand": [],
    };
    if (!(path in responses)) throw new Error(`Unexpected request ${path}`);
    return route.fulfill({ json: responses[path] });
  });
  await page.emulateMedia({ colorScheme: "dark" });
  await page.goto(`${baseURL}/#/stacks/s1`);
  await expect(page).toHaveTitle("Porty");
  await page.getByRole("tab", { name: "Settings" }).click();
  await expect(page.getByLabel("Cron schedule (UTC)")).toHaveValue("0 0 * * *");
  await expect(page.getByLabel("Enable automatic updates")).not.toBeChecked();
  await page.getByLabel("Enable automatic updates").check();
  await page.getByRole("button", { name: "Save schedule" }).click();
  await expect(page.getByText(/Next run.*28 Sept? 2026/)).toBeVisible();
  await page.locator(".auto-update-settings").screenshot({
    path: join(tmpdir(), `porty-auto-update-panel-${info.project.name}.png`),
  });
  status = {
    ...status,
    policy: {
      ...status.policy,
      revision: 2,
      pausedReason: "recovery_required",
    },
    lastRun: {
      id: "r1",
      outcome: "failed",
      reason: "recovery_required",
      scheduledAt: "2026-09-27T00:00:00Z",
    },
  };
  await page.getByRole("button", { name: "Refresh update status" }).click();
  await expect(page.getByText(/Automatic updates paused/)).toBeVisible();
  await page.getByRole("button", { name: "Review stack alerts" }).click();
  await page.getByRole("button", { name: "Acknowledge" }).click();
  await expect(page.getByText(/Acknowledged by admin/)).toBeVisible();
  await page.getByRole("tab", { name: "Settings" }).click();
  await expect(page.getByText(/Automatic updates paused/)).toBeVisible();
  await page.getByRole("button", { name: "Actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Deploy", exact: true }).click();
  await expect(page.getByText("Recovered manually")).toBeVisible();
  await page.getByRole("button", { name: "Close operation" }).click();
  await page
    .getByRole("button", { name: "Verify recovery and resume" })
    .click();
  await expect(page.getByText(/Automatic updates paused/)).toHaveCount(0);
  await expect(page.getByLabel("Enable automatic updates")).toBeChecked();
  await page.screenshot({
    path: join(tmpdir(), `porty-auto-update-${info.project.name}.png`),
    fullPage: true,
  });
  expect(writes).toEqual([
    "/stacks/s1/auto-update",
    "/alerts/a1/acknowledge",
    "/stacks/s1/actions/deploy",
    "/stacks/s1/auto-update/resume",
  ]);
  expect(errors).toEqual([]);
  expect(await page.locator("vite-error-overlay").count()).toBe(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
