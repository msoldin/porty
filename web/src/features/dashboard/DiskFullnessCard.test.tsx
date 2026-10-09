import { fireEvent, render, screen, within } from "@testing-library/preact";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Dashboard } from "./Dashboard";
import { useMonitoring } from "./useMonitoring";
import type { MonitoringState } from "./types";
vi.mock("./useMonitoring", () => ({ useMonitoring: vi.fn() }));
function DiskView({
  state,
  loading,
  stale,
}: {
  state: MonitoringState;
  loading: boolean;
  stale: boolean;
}) {
  vi.mocked(useMonitoring).mockReturnValue({
    state,
    loading,
    stale,
    error: "",
    retry: vi.fn(),
    serverNow: Date.parse(state.current.capturedAt),
  });
  return <Dashboard onUnauthorized={() => {}} />;
}
import { mergeMonitoring } from "./monitoringState";
import { snapshotFixture } from "./testFixtures";
import type { MetricKind } from "./types";

function fixture() {
  const response = snapshotFixture();
  for (const [id, name, percent] of [
    ["root", "/", 60],
    ["data", "/data", 10],
    ["backup", "/backup", 99],
    ["archive", "/archive", 20],
  ] as const) {
    response.inventory!.devices.push({
      id,
      name,
      kind: "filesystem",
      default: id === "root",
      driver: "ext4",
      mountPaths: [name],
    });
    for (const [metric, value] of [
      ["filesystem_percent", percent],
      ["filesystem_total", 1000000000],
      ["filesystem_used", percent * 10000000],
      ["filesystem_available", (100 - percent) * 10000000],
    ] as [MetricKind, number][]) {
      const key = id + "." + metric;
      response.inventory!.series.push({
        id: key,
        deviceId: id,
        metric,
        unit: metric === "filesystem_percent" ? "percent" : "bytes",
      });
      response.current.readings[key] = {
        value,
        state: "available",
        sampledAt: response.serverTime,
        lastSuccessAt: response.serverTime,
      };
    }
  }
  return mergeMonitoring(undefined, response);
}
beforeEach(() => localStorage.clear());
afterEach(() => vi.restoreAllMocks());

it("starts with the system disk and keeps the other filesystems out of the card", () => {
  render(<DiskView state={fixture()} loading={false} stale={false} />);
  expect(screen.getAllByRole("meter", { name: /disk fullness$/ })).toHaveLength(
    1,
  );
  expect(
    screen.getByRole("meter", { name: "/ disk fullness" }),
  ).toHaveAttribute("aria-valuenow", "60");
  expect(screen.getByText("400 MB free of 1 GB")).toBeVisible();
  expect(
    screen.getByRole("button", { name: "Customize dashboard" }),
  ).toBeEnabled();
});

it("remembers multiple chosen disks without adding unselected filesystems", () => {
  const view = render(
    <DiskView state={fixture()} loading={false} stale={false} />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  fireEvent.click(screen.getByRole("checkbox", { name: "/data", exact: true }));
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(screen.getAllByRole("meter", { name: /disk fullness$/ })).toHaveLength(
    2,
  );
  expect(
    screen.queryByRole("meter", { name: "/backup disk fullness" }),
  ).not.toBeInTheDocument();
  view.unmount();
  render(<DiskView state={fixture()} loading={false} stale={false} />);
  expect(
    screen.getByRole("meter", { name: "/data disk fullness" }),
  ).toBeVisible();
  expect(screen.getAllByRole("meter", { name: /disk fullness$/ })).toHaveLength(
    2,
  );
});

it("reports a missing selected disk without substituting a newly discovered disk", () => {
  const state = fixture();
  const view = render(<DiskView state={state} loading={false} stale={false} />);
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  fireEvent.click(screen.getByRole("checkbox", { name: "/data", exact: true }));
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  const removed = {
    ...state,
    inventory: {
      ...state.inventory,
      devices: state.inventory.devices.filter((d) => d.id !== "data"),
    },
  };
  view.rerender(<DiskView state={removed} loading={false} stale={false} />);
  expect(screen.getByRole("status")).toHaveTextContent(
    /selected disk.*unavailable/i,
  );
  expect(screen.getAllByRole("meter", { name: /disk fullness$/ })).toHaveLength(
    1,
  );
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  const checkbox = screen.getByRole("checkbox", { name: "/data", exact: true });
  expect(checkbox).toBeChecked();
  fireEvent.click(checkbox);
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
});

it("keeps an intentionally empty selection after reopening the dashboard", () => {
  const view = render(
    <DiskView state={fixture()} loading={false} stale={false} />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  fireEvent.click(screen.getByRole("checkbox", { name: "/", exact: true }));
  view.unmount();
  render(<DiskView state={fixture()} loading={false} stale={false} />);
  expect(
    screen.queryByRole("meter", { name: /disk fullness$/ }),
  ).not.toBeInTheDocument();
  expect(screen.getByText(/No disks selected/)).toBeVisible();
  expect(
    screen.getByRole("button", { name: "Customize dashboard" }),
  ).toBeEnabled();
});

it("keeps the picker usable when storage fails and restores focus on Escape", () => {
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw Error("Denied");
  });
  render(<DiskView state={fixture()} loading={false} stale={false} />);
  const button = screen.getByRole("button", { name: "Customize dashboard" });
  button.focus();
  fireEvent.click(button);
  fireEvent.click(screen.getByRole("checkbox", { name: "/data", exact: true }));
  fireEvent(
    screen.getByRole("dialog"),
    new Event("cancel", { cancelable: true }),
  );
  expect(button).toHaveFocus();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.getAllByRole("meter", { name: /disk fullness$/ })).toHaveLength(
    2,
  );
});

it("shows clear storage totals and keeps technical disk information collapsed", () => {
  render(<DiskView state={fixture()} loading={false} stale={false} />);
  fireEvent.click(screen.getByRole("button", { name: "Customize dashboard" }));
  fireEvent.click(
    screen.getByRole("checkbox", { name: "/backup", exact: true }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(
    screen.getByRole("meter", { name: "/backup disk fullness" }),
  ).toHaveAttribute("aria-valuenow", "99");
  fireEvent.click(
    screen.getByRole("button", { name: "/backup filesystem details" }),
  );
  const card = within(
    screen.getByRole("article", {
      name: "/backup · Disk fullness",
      hidden: true,
    }),
  );
  expect(card.getByText("Used").parentElement).toHaveTextContent("990 MB");
  expect(card.getByText("Free").parentElement).toHaveTextContent("10 MB");
  expect(card.getByText("Total").parentElement).toHaveTextContent("1 GB");
  expect(card.getByText("Technical details")).toBeVisible();
  expect(card.getByText("ext4")).not.toBeVisible();
  expect(card.queryByText(/Capacity guide/)).not.toBeInTheDocument();
  expect(card.queryByText("/data")).not.toBeInTheDocument();
});

it("offers compact selectable mount paths with copy controls", () => {
  const state = fixture();
  const disk = state.inventory.devices.find((device) => device.id === "root")!;
  disk.mountPaths = ["/", "/mnt/wsl/" + "long-path-".repeat(20)];
  render(<DiskView state={state} loading={false} stale={false} />);
  fireEvent.click(screen.getByRole("button", { name: "/ filesystem details" }));
  const dialog = within(screen.getByRole("dialog"));
  dialog.getByText("Technical details").parentElement!.setAttribute("open", "");
  expect(dialog.getByRole("textbox", { name: "Mount path 2" })).toHaveValue(
    disk.mountPaths[1],
  );
  expect(dialog.getByRole("textbox", { name: "Mount path 2" })).toHaveAttribute(
    "readonly",
  );
  expect(
    dialog.getByRole("button", { name: "Copy mount path 2" }),
  ).toBeVisible();
});
