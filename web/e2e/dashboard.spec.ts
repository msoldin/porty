import { test, expect, type Page } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { snapshotFixture } from "../src/features/dashboard/testFixtures";
import type {
  DeviceKind,
  MetricKind,
  Unit,
} from "../src/features/dashboard/types";

let server: ChildProcess, baseURL: string, dataDir: string;
test.beforeEach(async () => {
  dataDir = await mkdtemp(join(tmpdir(), "porty-dashboard-e2e-"));
  const port = 26000 + Math.floor(Math.random() * 1000);
  baseURL = "http://127.0.0.1:" + port;
  server = spawn(
    process.env.PORTY_E2E_BINARY || join(tmpdir(), "porty-e2e"),
    ["--listen", "127.0.0.1:" + port, "--data-dir", dataDir],
    { stdio: "ignore" },
  );
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(baseURL + "/healthz")).ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 50));
  }
  throw Error("Porty did not start");
});
test.afterEach(async () => {
  if (server?.exitCode === null)
    await new Promise<void>((resolve) => {
      server.once("exit", () => resolve());
      server.kill("SIGTERM");
    });
  await rm(dataDir, { recursive: true, force: true });
});
async function navigate(page: Page, name: string) {
  const nav = page.getByRole("navigation", { name: "Main navigation" });
  if (!(await nav.isVisible()))
    await page.getByRole("button", { name: "Navigation", exact: true }).click();
  await nav.getByRole("link", { name, exact: true }).click();
}
async function register(page: Page) {
  await page.goto(baseURL);
  await expect(page).toHaveTitle("Porty");
  await page.getByLabel("Username").fill("admin");
  await page
    .getByLabel("Password", { exact: true })
    .fill("correct horse battery staple");
  await page.getByRole("button", { name: "Create administrator" }).click();
  await expect(
    page.getByRole("heading", { name: "Dashboard", exact: true }),
  ).toBeVisible();
}
function metrics() {
  const s = snapshotFixture();
  const add = (
    id: string,
    kind: DeviceKind,
    name: string,
    metric: MetricKind,
    value: number,
    unit: Unit = "percent",
    primary = false,
  ) => {
    if (!s.inventory!.devices.some((d) => d.id === id))
      s.inventory!.devices.push({ id, kind, name, default: primary });
    const series = id + "." + metric;
    s.inventory!.series.push({ id: series, deviceId: id, metric, unit });
    s.current.readings[series] = {
      value,
      state: "available",
      sampledAt: s.serverTime,
      lastSuccessAt: s.serverTime,
    };
  };
  add("ram", "memory", "Host memory", "memory_total", 16 * 1024 ** 3, "bytes");
  add("ram", "memory", "Host memory", "memory_used", 6 * 1024 ** 3, "bytes");
  add("ram", "memory", "Host memory", "memory_percent", 37.5);
  add("temp", "sensor", "CPU package", "temperature", 52, "celsius", true);
  add("gpu", "gpu", "Intel UHD Graphics", "gpu_busy", 18);
  s.inventory!.devices.find((d) => d.id === "gpu")!.utilizationBasis =
    "busiest_engine";
  for (const [id, name, rate] of [
    ["eth0", "enp3s0", 2000000],
    ["eth1", "very-long-network-interface-name-for-monitoring", 4000000],
  ] as const) {
    add(
      id,
      "interface",
      name,
      "network_receive_rate",
      rate,
      "bytes_per_second",
      id === "eth0",
    );
    add(
      id,
      "interface",
      name,
      "network_send_rate",
      rate / 2,
      "bytes_per_second",
    );
  }
  add(
    "disk",
    "block",
    "Physical disks",
    "disk_read_rate",
    1500000,
    "bytes_per_second",
    true,
  );
  add(
    "disk",
    "block",
    "Physical disks",
    "disk_write_rate",
    800000,
    "bytes_per_second",
  );
  for (const [id, name, percent] of [
    ["root", "/", 68],
    ["data", "/srv/very-long-filesystem-path-for-application-storage", 23],
  ] as const) {
    add(id, "filesystem", name, "filesystem_percent", percent);
    add(id, "filesystem", name, "filesystem_total", 1000000000000, "bytes");
    add(
      id,
      "filesystem",
      name,
      "filesystem_used",
      percent * 10000000000,
      "bytes",
    );
    add(
      id,
      "filesystem",
      name,
      "filesystem_available",
      (100 - percent) * 10000000000,
      "bytes",
    );
  }
  s.samples = Array.from({ length: 151 }, (_, i) => {
    const time = new Date(
      Date.parse(s.serverTime) - (150 - i) * 2000,
    ).toISOString();
    return {
      sequence: String(i + 1),
      capturedAt: time,
      readings: Object.fromEntries(
        Object.entries(s.current.readings).map(([id, r]) => [
          id,
          {
            ...r,
            value:
              i === 80 ? null : (r.value ?? 0) * (0.8 + 0.2 * Math.sin(i / 8)),
            state: i === 80 ? "unavailable" : "available",
            sampledAt: time,
            lastSuccessAt: time,
          },
        ]),
      ),
    };
  });
  s.current.sequence = "151";
  return s;
}
test("opens metrics before repository setup and keeps them usable during a repository outage", async ({
  page,
}) => {
  await register(page);
  await expect(page.getByRole("heading", { name: "CPU usage" })).toBeVisible();
  await navigate(page, "Stacks");
  await expect(
    page.getByRole("heading", { name: "Configure the stack repository" }),
  ).toBeVisible();
  await expect(page.getByRole("main")).toHaveCount(1);
  await navigate(page, "Dashboard");
  await page.route("**/api/v1/repository/setup/status", (r) =>
    r.fulfill({
      status: 503,
      json: { error: { message: "Repository unavailable" } },
    }),
  );
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Dashboard", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Repository unavailable")).toHaveCount(0);
  await navigate(page, "Stacks");
  await expect(page.getByText("Repository unavailable")).toBeVisible();
  await navigate(page, "Settings");
  await expect(
    page.getByRole("heading", { name: "Change password" }),
  ).toBeVisible();
});
test("navigates dashboard to stacks and a container and back", async ({
  page,
}) => {
  await register(page);
  await navigate(page, "Stacks");
  await page.getByRole("button", { name: "Create local repository" }).click();
  await page
    .getByRole("button", { name: "Create repository", exact: true })
    .click();
  await page.route("**/api/v1/stacks/*/containers", (r) =>
    r.fulfill({
      json: [
        {
          id: "full-container-id",
          name: "web-1",
          service: "web",
          state: "running",
          health: "healthy",
          image: "example/web",
          networks: [],
          ports: [],
        },
      ],
    }),
  );
  await page.getByRole("button", { name: "New stack" }).click();
  await page.getByLabel("Stack name").fill("dashboard-test");
  await page.getByRole("button", { name: "Create stack", exact: true }).click();
  await page.getByRole("link", { name: "Open web-1" }).click();
  await expect(page.getByRole("heading", { name: "web-1" })).toBeVisible();
  await page
    .getByRole("navigation", { name: "Breadcrumb" })
    .getByRole("link", { name: "Stacks", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Stacks", exact: true }),
  ).toBeVisible();
  await navigate(page, "Dashboard");
  await expect(
    page.getByRole("heading", { name: "Dashboard", exact: true }),
  ).toBeVisible();
});
test("recovers from stale samples without inventing zeros", async ({
  page,
}) => {
  let mode = "fresh";
  await page.route("**/api/v1/monitoring*", (r) => {
    const s = metrics();
    if (mode === "stale") {
      s.current.readings["cpu.busy"].state = "stale";
      s.current.readings["cpu.busy"].reason = "collection_delayed";
    }
    if (mode === "recovered") s.current.readings["cpu.busy"].value = 42;
    return r.fulfill({ json: s });
  });
  await register(page);
  const cpu = page.getByRole("article", { name: "CPU usage" });
  await expect(cpu.getByText("25%", { exact: true })).toBeVisible();
  mode = "stale";
  await expect(cpu.getByText("Stale", { exact: true })).toBeVisible();
  await expect(cpu.getByText("25%", { exact: true })).toBeVisible();
  mode = "recovered";
  await expect(cpu.getByText("42%", { exact: true })).toBeVisible();
  await expect(cpu.getByText("Stale", { exact: true })).toHaveCount(0);
});
test("keeps selected device details readable on mobile and across themes", async ({
  page,
}, info) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (e) => {
    if (
      e.type() === "error" &&
      !(
        e.text().includes("401") && e.location().url.endsWith("/api/v1/session")
      )
    )
      errors.push(e.text());
  });
  await page.route("**/api/v1/monitoring*", (r) =>
    r.fulfill({ json: metrics() }),
  );
  await register(page);
  await expect(page.getByText("Receiving metrics")).toBeVisible();
  const chart = page.getByRole("group", { name: "CPU history" });
  await chart.focus();
  await chart.press("ArrowLeft");
  await expect(page.locator(".chart-inspection").first()).toBeVisible();
  for (const theme of ["light", "dark"]) {
    await page.evaluate(
      (theme) => (document.documentElement.dataset.theme = theme),
      theme,
    );
    await page.screenshot({
      path: info.outputPath("dashboard-" + theme + ".png"),
      fullPage: true,
    });
  }
  for (const button of await page.locator(".host-metric-toggle").all())
    await button.click();
  await page
    .getByLabel("Download interface", { exact: true })
    .selectOption("eth1");
  await expect(
    page.getByLabel("Upload interface", { exact: true }),
  ).toHaveValue("eth1");
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
    await page.screenshot({
      path: info.outputPath("dashboard-details-" + width + ".png"),
      fullPage: true,
    });
  }
  await page.reload();
  await page.getByRole("button", { name: "Download details" }).click();
  await expect(
    page.getByLabel("Download interface", { exact: true }),
  ).toHaveValue("eth1");
  expect(errors).toEqual([]);
});
test("clears metrics on logout and after session expiry", async ({ page }) => {
  await page.route("**/api/v1/monitoring*", (r) =>
    r.fulfill({ json: metrics() }),
  );
  await register(page);
  await expect(page.getByText("25%", { exact: true })).toBeVisible();
  if (
    !(await page
      .getByRole("button", { name: "Sign out", exact: true })
      .isVisible())
  )
    await page.getByRole("button", { name: "Navigation", exact: true }).click();
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Welcome back" }),
  ).toBeVisible();
  await expect(page.getByText("25%", { exact: true })).toHaveCount(0);
  await page.getByLabel("Username").fill("admin");
  await page
    .getByLabel("Password", { exact: true })
    .fill("correct horse battery staple");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Dashboard", exact: true }),
  ).toBeVisible();
  await page.route("**/api/v1/monitoring*", (r) =>
    r.fulfill({ status: 401, json: { error: { message: "Expired" } } }),
  );
  await page.route("**/api/v1/session/refresh", (r) =>
    r.fulfill({ status: 401, json: { error: { message: "Expired" } } }),
  );
  await expect(
    page.getByRole("heading", { name: "Welcome back" }),
  ).toBeVisible();
  await expect(page.getByText("25%", { exact: true })).toHaveCount(0);
});
