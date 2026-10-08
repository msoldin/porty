import { test, expect } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { OnDemandGroup } from "../src/features/stacks/onDemandTypes";
let server: ChildProcess;
let baseURL: string;
let dataDir: string;
test.beforeAll(async () => {
  const port = 22000 + Math.floor(Math.random() * 1000);
  baseURL = `http://127.0.0.1:${port}`;
  dataDir = await mkdtemp(join(tmpdir(), "porty-on-demand-browser-"));
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
test("configures single-attempt wake and explicitly holds and resumes a group", async ({
  page,
}, info) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  let groups: OnDemandGroup[] = [];
  await page.routeWebSocket("**/api/v1/stream", (socket) =>
    socket.onMessage(() => {}),
  );
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname.replace("/api/v1", "");
    if (path === "/stacks/s1/on-demand") {
      if (request.method() === "POST") {
        const input = request.postDataJSON();
        expect(input.wakeThreshold).toBe(1);
        expect(input.members).toEqual(["game"]);
        expect(input.expectedRevision).toBe(0);
        groups = [
          { ...input, id: "g1", stackId: "s1", revision: 1, phase: "running" },
        ];
        return route.fulfill({ status: 201, json: groups[0] });
      }
      return route.fulfill({ json: groups });
    }
    if (path.endsWith("/on-demand/g1/hold")) {
      expect(request.postDataJSON()).toEqual({ expectedRevision: 1 });
      groups = [
        {
          ...groups[0],
          revision: 2,
          holdReason: "Manually held. Resume explicitly.",
        },
      ];
      return route.fulfill({ status: 204 });
    }
    if (path.endsWith("/on-demand/g1/resume")) {
      expect(request.postDataJSON()).toEqual({ expectedRevision: 2 });
      groups = [{ ...groups[0], revision: 3, holdReason: undefined }];
      return route.fulfill({ json: groups[0] });
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
      "/alerts": { items: [], total: 0, unacknowledgedCount: 0 },
      "/stacks": [
        {
          id: "s1",
          directoryName: "minecraft",
          composeProjectName: "porty-minecraft",
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
      "/stacks/s1/auto-update": {
        policy: { enabled: false, revision: 0, expression: "0 0 * * *" },
        available: true,
        eligible: true,
        excluded: {},
      },
    };
    if (!(path in responses)) throw new Error(`Unexpected request ${path}`);
    return route.fulfill({ json: responses[path] });
  });
  await page.goto(`${baseURL}/#/stacks/s1`);
  await expect(page).toHaveTitle("Porty");
  await page.getByRole("tab", { name: "Settings" }).click();
  const panel = page.getByRole("region", { name: "On-demand containers" });
  await expect(
    panel.getByRole("heading", { name: "On-demand containers" }),
  ).toBeVisible();
  await panel.getByRole("button", { name: "Add on-demand group" }).click();
  await panel.getByLabel("Group name").fill("Minecraft");
  await panel.getByLabel("Services", { exact: true }).fill("game");
  await expect(panel.getByLabel("Wake attempts")).toHaveValue("1");
  await panel.screenshot({
    path: join(tmpdir(), `porty-on-demand-form-${info.project.name}.png`),
  });
  await panel.getByRole("button", { name: "Save group" }).click();
  await expect(
    panel.getByRole("heading", { name: "Minecraft", exact: true }),
  ).toBeVisible();
  await panel.getByRole("button", { name: "Hold Minecraft" }).click();
  await expect(
    panel.getByText("Manually held. Resume explicitly."),
  ).toBeVisible();
  await panel.getByRole("button", { name: "Resume Minecraft" }).click();
  await expect(
    panel.getByText("Manually held. Resume explicitly."),
  ).toHaveCount(0);
  await expect(
    panel.getByRole("button", { name: "Hold Minecraft" }),
  ).toBeEnabled();
  await panel.screenshot({
    path: join(tmpdir(), `porty-on-demand-${info.project.name}.png`),
  });
  expect(errors).toEqual([]);
  expect(await page.locator("vite-error-overlay").count()).toBe(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
