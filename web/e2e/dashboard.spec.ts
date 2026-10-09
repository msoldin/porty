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

test("shows stale observation times and temperature scale without overflowing", async ({
  page,
}, info) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (event) => {
    if (
      event.type() === "error" &&
      !(
        event.text().includes("401") &&
        event.location().url.endsWith("/api/v1/session")
      )
    )
      errors.push(event.text());
  });
  const snapshot = metrics();
  const lastSuccessAt = "2026-10-08T10:15:00.000Z";
  for (const reading of Object.values(snapshot.current.readings)) {
    reading.state = "stale";
    reading.lastSuccessAt = lastSuccessAt;
  }
  await page.route("**/api/v1/monitoring*", (route) =>
    route.fulfill({ json: snapshot }),
  );
  await register(page);
  for (const name of [
    "CPU usage",
    "RAM usage",
    "Temperature",
    "GPU usage",
    "Download",
    "Upload",
    "Disk I/O",
    "/ · Disk fullness",
  ]) {
    const card = page.getByRole("article", { name });
    await expect(
      card.getByText("Last reading:", { exact: false }).first(),
    ).toBeVisible();
    await expect(card.locator("time").first()).toHaveAttribute(
      "datetime",
      lastSuccessAt,
    );
  }
  await expect(
    page
      .getByRole("group", { name: "Temperature history" })
      .getByText(/^Range:/),
  ).toBeVisible();
  await page.screenshot({
    path: info.outputPath("dashboard-stale.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "Download details" }).click();
  await expect(
    page
      .getByRole("article", { name: "Download" })
      .locator(".metric-detail-row time"),
  ).toHaveCount(2);
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Close", exact: true })
    .click();
  await page.setViewportSize({ width: 320, height: 844 });
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.screenshot({
    path: info.outputPath("dashboard-stale-details-320.png"),
    fullPage: true,
  });
  expect(errors).toEqual([]);
});

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
test("keeps the compact grid and details readable across themes and screen sizes", async ({
  page,
}, info) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (event) => {
    if (
      event.type() === "error" &&
      !(
        event.text().includes("401") &&
        event.location().url.endsWith("/api/v1/session")
      )
    )
      errors.push(event.text());
  });
  await page.route("**/api/v1/monitoring*", (route) =>
    route.fulfill({ json: metrics() }),
  );
  await register(page);
  await expect(page.getByText("Receiving metrics")).toBeVisible();
  const download = page.getByRole("article", { name: "Download" });
  const upload = page.getByRole("article", { name: "Upload" });
  const left = await download.boundingBox(),
    right = await upload.boundingBox();
  expect(left!.y).toBe(right!.y);
  expect(right!.x).toBeGreaterThan(left!.x);
  expect(left!.height).toBeLessThan(170);
  const heading = await page.locator(".host-dashboard-heading").boundingBox();
  const grid = await page.locator(".host-metric-grid").boundingBox();
  expect(heading!.x).toBe(grid!.x);
  expect(heading!.width).toBe(grid!.width);
  await expect(page.getByRole("combobox")).toHaveCount(0);
  for (const theme of ["light", "dark"]) {
    await page.evaluate(
      (theme) => (document.documentElement.dataset.theme = theme),
      theme,
    );
    await page.screenshot({
      path: info.outputPath("compact-" + theme + ".png"),
      fullPage: true,
    });
  }
  await page.getByRole("button", { name: "CPU details", exact: true }).click();
  const detail = page.getByRole("dialog", { name: "CPU details" });
  const history = detail.getByRole("group", { name: "CPU history" });
  await history.focus();
  await history.press("ArrowLeft");
  await expect(detail.locator(".chart-inspection")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(detail).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "CPU details", exact: true }),
  ).toBeFocused();
  // A touch/click on the large reading opens details; the chart remains independently inspectable.
  await download.click({ position: { x: 30, y: 45 } });
  await expect(
    page.getByRole("dialog", { name: "Download details" }),
  ).toBeVisible();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Close", exact: true })
    .click();
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
    await page.screenshot({
      path: info.outputPath("compact-" + width + ".png"),
      fullPage: true,
    });
  }
  expect(errors).toEqual([]);
});

test("remembers separate disk gauges and shared network choices from customization", async ({
  page,
}, info) => {
  const snapshot = metrics();
  snapshot.inventory!.devices.push({
    id: "backup",
    kind: "filesystem",
    name: "/backup",
    default: false,
  });
  await page.route("**/api/v1/monitoring*", (route) =>
    route.fulfill({ json: snapshot }),
  );
  await register(page);
  const customize = page.getByRole("button", { name: "Customize dashboard" });
  const dataPath = "/srv/very-long-filesystem-path-for-application-storage";
  await expect(page.getByRole("meter", { name: /disk fullness$/ })).toHaveCount(
    1,
  );
  await customize.click();
  const dialog = page.getByRole("dialog", { name: "Customize dashboard" });
  for (const row of await dialog.locator(".disk-picker-options label").all()) {
    const box = await row.boundingBox();
    expect(box!.height).toBeLessThan(100);
    await expect(row.locator("span")).toBeInViewport();
  }
  await dialog.getByRole("checkbox", { name: dataPath, exact: true }).check();
  await expect(
    dialog.getByRole("checkbox", { name: "/backup", exact: true }),
  ).not.toBeChecked();
  await expect(dialog.getByText("2 of 3", { exact: true })).toBeVisible();
  await dialog
    .getByRole("combobox", { name: "Network interface" })
    .selectOption("eth1");
  await page.screenshot({
    path: info.outputPath("customize-dashboard.png"),
    fullPage: true,
  });
  await dialog.getByRole("button", { name: "Done", exact: true }).click();
  await expect(customize).toBeFocused();
  await expect(page.getByRole("meter", { name: /disk fullness$/ })).toHaveCount(
    2,
  );
  await expect(
    page
      .getByRole("article", { name: "Download" })
      .getByText("4 MB/s", { exact: true }),
  ).toBeVisible();
  await expect(
    page
      .getByRole("article", { name: "Upload" })
      .getByText("2 MB/s", { exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(page.getByRole("meter", { name: /disk fullness$/ })).toHaveCount(
    2,
  );
  await expect(
    page
      .getByRole("article", { name: "Download" })
      .getByText("4 MB/s", { exact: true }),
  ).toBeVisible();
  await page.evaluate(() => (document.documentElement.dataset.theme = "dark"));
  await page.screenshot({
    path: info.outputPath("selected-disks.png"),
    fullPage: true,
  });
  await page
    .getByRole("button", {
      name: dataPath + " filesystem details",
      exact: true,
    })
    .click();
  await expect(page.getByRole("dialog").getByRole("heading")).toContainText(
    dataPath,
  );
  await page.keyboard.press("Escape");
  await page.setViewportSize({ width: 320, height: 844 });
  await customize.click();
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await dialog.getByRole("checkbox", { name: dataPath, exact: true }).focus();
  await page.keyboard.press("Escape");
  await expect(customize).toBeFocused();
  await expect(dialog).toHaveCount(0);
});

test("explains unavailable temperature inside its details without a technical message list", async ({
  page,
}, info) => {
  const snapshot = metrics();
  snapshot.inventory!.devices = snapshot.inventory!.devices.filter(
    (device) => device.id !== "temp",
  );
  snapshot.coverage = [
    { source: "sensors", partial: true, omitted: 0, reason: "no_device" },
  ];
  await page.route("**/api/v1/monitoring*", (route) =>
    route.fulfill({ json: snapshot }),
  );
  await register(page);
  await expect(page.getByText("Metric availability and coverage")).toHaveCount(
    0,
  );
  await expect(page.getByText("No device detected")).toHaveCount(0);
  const temperature = page.getByRole("article", { name: "Temperature" });
  const temperatureBox = await temperature.boundingBox();
  const ioBox = await page
    .getByRole("article", { name: "Disk I/O" })
    .boundingBox();
  expect(temperatureBox!.height).toBe(ioBox!.height);
  await expect(
    temperature.getByText("Unavailable", { exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: info.outputPath("unavailable-dashboard.png"),
    fullPage: true,
  });
  await temperature
    .getByRole("button", { name: "Temperature details" })
    .click();
  const dialog = page.getByRole("dialog", { name: "Temperature details" });
  await expect(
    dialog.getByText("No temperature sensors are exposed to this environment."),
  ).toBeVisible();
  await expect(dialog).toBeInViewport();
  await expect(dialog.getByText("—", { exact: true })).toHaveCount(0);
  await page.screenshot({
    path: info.outputPath("temperature-explanation.png"),
    fullPage: true,
  });
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(
    temperature.getByRole("button", { name: "Temperature details" }),
  ).toBeFocused();
});

test("shows download hover readings only where a sample exists", async ({
  page,
}, info) => {
  const snapshot = metrics();
  snapshot.samples![81].readings["eth0.network_receive_rate"].value = 0;
  await page.route("**/api/v1/monitoring*", (route) =>
    route.fulfill({ json: snapshot }),
  );
  await register(page);
  const chart = page.getByRole("group", { name: "Download history" });
  await chart.scrollIntoViewIfNeeded();
  const box = (await chart.boundingBox())!;
  const inspect = async (index: number) => {
    const x = box.x + (box.width * index) / 150,
      y = box.y + box.height / 2;
    if (info.project.use.hasTouch) await page.touchscreen.tap(x, y);
    else await page.mouse.move(x, y);
  };
  await inspect(100);
  await expect(chart.locator("output")).toContainText("Download:");
  await inspect(80);
  await expect(chart.locator("output")).toHaveCount(0);
  await inspect(81);
  await expect(chart.locator("output")).toContainText("Download: 0 B/s");
  await chart.focus();
  await chart.press("Home");
  for (let i = 0; i < 80; i++) await chart.press("ArrowRight");
  await expect(chart.locator("output")).toHaveCount(0);
  await chart.press("ArrowRight");
  await expect(chart.locator("output")).toContainText("Download: 0 B/s");
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
