import { fireEvent, render, screen, within } from "@testing-library/preact";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Dashboard } from "./Dashboard";
import { useMonitoring } from "./useMonitoring";
import { mergeMonitoring } from "./monitoringState";
import { snapshotFixture } from "./testFixtures";
import type { DeviceKind, MetricKind, Unit } from "./types";
vi.mock("./useMonitoring", () => ({ useMonitoring: vi.fn() }));
function fixture() {
  const response = snapshotFixture();
  const add = (
    id: string,
    kind: DeviceKind,
    name: string,
    metric: MetricKind,
    value: number,
    unit: Unit = "percent",
    primary = false,
  ) => {
    if (!response.inventory!.devices.some((d) => d.id === id))
      response.inventory!.devices.push({ id, kind, name, default: primary });
    const seriesId = id + "." + metric;
    response.inventory!.series.push({
      id: seriesId,
      deviceId: id,
      metric,
      unit,
    });
    response.current.readings[seriesId] = {
      value,
      state: "available",
      sampledAt: response.serverTime,
      lastSuccessAt: response.serverTime,
    };
  };
  add("memory", "memory", "Host memory", "memory_percent", 40);
  add("memory", "memory", "Host memory", "memory_total", 1073741824, "bytes");
  add("memory", "memory", "Host memory", "memory_used", 429496730, "bytes");
  add("temp", "sensor", "CPU package", "temperature", 52, "celsius", true);
  add("gpu", "gpu", "Intel UHD", "gpu_busy", 8);
  add(
    "eth0",
    "interface",
    "eth0",
    "network_receive_rate",
    2000000,
    "bytes_per_second",
    true,
  );
  add(
    "eth0",
    "interface",
    "eth0",
    "network_send_rate",
    1000000,
    "bytes_per_second",
  );
  add(
    "eth1",
    "interface",
    "eth1",
    "network_receive_rate",
    4000000,
    "bytes_per_second",
  );
  add(
    "eth1",
    "interface",
    "eth1",
    "network_send_rate",
    3000000,
    "bytes_per_second",
  );
  add(
    "disk-total",
    "block",
    "Physical disks",
    "disk_read_rate",
    5000000,
    "bytes_per_second",
    true,
  );
  add(
    "disk-total",
    "block",
    "Physical disks",
    "disk_write_rate",
    1000000,
    "bytes_per_second",
  );
  add("fs-root", "filesystem", "/", "filesystem_percent", 99);
  add("fs-root", "filesystem", "/", "filesystem_total", 1000000000, "bytes");
  add("fs-root", "filesystem", "/", "filesystem_used", 990000000, "bytes");
  add("fs-data", "filesystem", "/data", "filesystem_percent", 10);
  add(
    "fs-data",
    "filesystem",
    "/data",
    "filesystem_total",
    1000000000,
    "bytes",
  );
  add("fs-data", "filesystem", "/data", "filesystem_used", 100000000, "bytes");
  const state = mergeMonitoring(undefined, response);
  return {
    state,
    loading: false,
    error: "",
    stale: false,
    serverNow: Date.parse(response.serverTime),
    retry: vi.fn(),
  };
}
beforeEach(() => {
  localStorage.clear();
  vi.mocked(useMonitoring).mockReturnValue(fixture());
});
afterEach(() => vi.restoreAllMocks());
it("disables device choices when no matching hardware is available", () => {
  const value = fixture();
  value.state.inventory.devices = value.state.inventory.devices.filter(
    (device) => device.kind !== "sensor" && device.kind !== "gpu",
  );
  vi.mocked(useMonitoring).mockReturnValue(value);
  render(<Dashboard onUnauthorized={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  expect(
    screen.getByRole("combobox", { name: "Temperature sensor" }),
  ).toBeDisabled();
  expect(screen.getByRole("combobox", { name: "GPU device" })).toBeDisabled();
  expect(
    screen.getByRole("combobox", { name: "Network interface" }),
  ).toBeEnabled();
});
it("keeps general monitoring problems in customization without exposing internal source names", () => {
  const value = fixture();
  value.state.coverage = [
    {
      source: "internal-collector-123",
      reason: "read_failed",
      partial: true,
      omitted: 0,
    },
  ];
  vi.mocked(useMonitoring).mockReturnValue(value);
  render(<Dashboard onUnauthorized={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(
    dialog.getByText(
      "Porty could not read this metric. It will try again automatically.",
    ),
  ).toBeVisible();
  expect(dialog.queryByText(/internal-collector/)).not.toBeInTheDocument();
});
it("explains missing temperature sensors in the tile details without a technical coverage list", () => {
  const value = fixture();
  value.state.inventory.devices = value.state.inventory.devices.filter(
    (device) => device.id !== "temp",
  );
  value.state.coverage = [
    { source: "sensors", reason: "no_device", partial: true, omitted: 0 },
  ];
  vi.mocked(useMonitoring).mockReturnValue(value);
  render(<Dashboard onUnauthorized={() => {}} />);
  expect(
    screen.queryByText("Metric availability and coverage"),
  ).not.toBeInTheDocument();
  const temperature = within(
    screen.getByRole("article", { name: "Temperature" }),
  );
  expect(temperature.getByText("Unavailable")).toBeVisible();
  fireEvent.click(
    temperature.getByRole("button", { name: "Temperature details" }),
  );
  const dialog = within(
    screen.getByRole("dialog", { name: "Temperature details" }),
  );
  expect(
    dialog.getByText("No temperature sensors are exposed to this environment."),
  ).toBeVisible();
  expect(dialog.queryByText("sensors:")).not.toBeInTheDocument();
  expect(
    dialog.queryByRole("group", { name: "Temperature history" }),
  ).not.toBeInTheDocument();
});
it("keeps incomplete disk activity explanations with disk activity details", () => {
  const value = fixture();
  value.state.coverage = [
    { source: "disks", reason: "partial_coverage", partial: true, omitted: 2 },
  ];
  vi.mocked(useMonitoring).mockReturnValue(value);
  render(<Dashboard onUnauthorized={() => {}} />);
  fireEvent.click(
    screen.getByRole("button", { name: "Disk activity details" }),
  );
  expect(
    within(screen.getByRole("dialog")).getByText(
      "Some disks aren’t included in these readings. At least 2 devices aren’t shown.",
    ),
  ).toBeVisible();
});
it("keeps customization in one dialog and gives each selected disk its own gauge tile", () => {
  render(<Dashboard onUnauthorized={() => {}} />);
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  const dialog = within(
    screen.getByRole("dialog", { name: "Customize dashboard" }),
  );
  fireEvent.click(dialog.getByRole("checkbox", { name: "/data", exact: true }));
  fireEvent.change(
    dialog.getByRole("combobox", { name: "Network interface" }),
    { target: { value: "eth1" } },
  );
  fireEvent.click(dialog.getByRole("button", { name: "Done" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(
    screen.getByRole("meter", { name: "/ disk fullness" }),
  ).toHaveAttribute("aria-valuenow", "99");
  expect(
    screen.getByRole("meter", { name: "/data disk fullness" }),
  ).toHaveAttribute("aria-valuenow", "10");
  expect(screen.getByRole("article", { name: "Download" })).toHaveTextContent(
    "4 MB/s",
  );
  expect(screen.getByRole("article", { name: "Upload" })).toHaveTextContent(
    "3 MB/s",
  );
});
it("opens tile details without leaving the dashboard and retains CPU history there", () => {
  render(<Dashboard onUnauthorized={() => {}} />);
  expect(screen.getByRole("meter", { name: "CPU usage" })).toHaveAttribute(
    "aria-valuenow",
    "25",
  );
  expect(
    screen.queryByRole("group", { name: "CPU history" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "CPU details" }));
  const dialog = within(screen.getByRole("dialog", { name: "CPU details" }));
  expect(dialog.getByRole("group", { name: "CPU history" })).toBeVisible();
  fireEvent.click(dialog.getByRole("button", { name: "Close" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
it("shows last-success timestamps for stale cards, details and filesystems", () => {
  const value = fixture();
  const at = "2026-10-08T10:15:00.000Z";
  for (const reading of Object.values(value.state.current.readings)) {
    reading.state = "stale";
    reading.lastSuccessAt = at;
  }
  vi.mocked(useMonitoring).mockReturnValue(value);
  render(<Dashboard onUnauthorized={() => {}} />);
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
    const card = screen.getByRole("article", { name });
    expect(within(card).getAllByText(/Last reading:/).length).toBeGreaterThan(
      0,
    );
    expect(card.querySelector("time")).toHaveAttribute("datetime", at);
  }
  fireEvent.click(screen.getByRole("button", { name: "Download details" }));
  const detail = screen
    .getByRole("article", { name: "Download" })
    .querySelector(".host-metric-details")!;
  expect(detail.querySelectorAll(".metric-detail-row time")).toHaveLength(2);
});
it("shows all eight host metrics with their sources", () => {
  render(<Dashboard onUnauthorized={() => {}} />);
  for (const name of [
    "CPU usage",
    "RAM usage",
    "Temperature",
    "GPU usage",
    "Download",
    "Upload",
    "Disk I/O",
    "/ · Disk fullness",
  ])
    expect(screen.getByRole("heading", { name, exact: true })).toBeVisible();
  expect(screen.getByText("CPU package")).toBeVisible();
  expect(
    screen.getByRole("button", { name: /temperature details/i }),
  ).toHaveAttribute("aria-expanded", "false");
  expect(screen.queryByText(/application shortcuts/i)).not.toBeInTheDocument();
});
it("uses the same selected interface for upload and download", () => {
  render(<Dashboard onUnauthorized={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  fireEvent.change(
    screen.getByRole("combobox", { name: "Network interface" }),
    { target: { value: "eth1" } },
  );
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(screen.getByRole("article", { name: "Download" })).toHaveTextContent(
    "4 MB/s",
  );
  expect(screen.getByRole("article", { name: "Upload" })).toHaveTextContent(
    "3 MB/s",
  );
});
it("does not hide a full filesystem in an average", () => {
  render(<Dashboard onUnauthorized={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  fireEvent.click(screen.getByRole("checkbox", { name: "/data", exact: true }));
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(
    within(
      screen.getByRole("article", { name: "/ · Disk fullness" }),
    ).getByText("99%"),
  ).toBeVisible();
  expect(
    within(
      screen.getByRole("article", { name: "/data · Disk fullness" }),
    ).getByText("10%"),
  ).toBeVisible();
});
it("lets users switch network interfaces without opening details", () => {
  render(<Dashboard onUnauthorized={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  const selector = screen.getByRole("combobox", { name: "Network interface" });
  expect(selector).toBeVisible();
  fireEvent.change(selector, { target: { value: "eth1" } });
  expect(
    screen.getByRole("combobox", { name: "Network interface" }),
  ).toHaveValue("eth1");
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(
    screen.getByRole("button", { name: "Download details" }),
  ).toHaveAttribute("aria-expanded", "false");
});
it("marks unavailable and stale values instead of zero", () => {
  const value = fixture();
  value.state.current.readings["cpu.busy"].state = "stale";
  value.state.current.readings["gpu.gpu_busy"].state = "unavailable";
  value.state.current.readings["gpu.gpu_busy"].value = null;
  vi.mocked(useMonitoring).mockReturnValue(value);
  render(<Dashboard onUnauthorized={() => {}} />);
  expect(
    within(screen.getByRole("article", { name: "CPU usage" })).getByText(
      "Updates delayed",
    ),
  ).toBeVisible();
  expect(
    within(screen.getByRole("article", { name: "GPU usage" })).getByText(
      "Unavailable",
    ),
  ).toBeVisible();
});
it("announces fallback when a selected device disappears", () => {
  localStorage.setItem(
    "porty.dashboard.v1",
    JSON.stringify({ network: "removed" }),
  );
  render(<Dashboard onUnauthorized={() => {}} />);
  expect(screen.getByRole("status")).toHaveTextContent(/no longer available/i);
});
it("keeps selections functional when browser storage is denied", () => {
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("Denied");
  });
  render(<Dashboard onUnauthorized={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  fireEvent.change(
    screen.getByRole("combobox", { name: "Network interface" }),
    {
      target: { value: "eth1" },
    },
  );
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(screen.getByRole("article", { name: "Upload" })).toHaveTextContent(
    "3 MB/s",
  );
});
