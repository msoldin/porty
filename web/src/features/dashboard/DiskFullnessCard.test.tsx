import { fireEvent, render, screen, within } from "@testing-library/preact";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { DiskFullnessCard } from "./DiskFullnessCard";
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
  render(<DiskFullnessCard state={fixture()} loading={false} stale={false} />);
  expect(screen.getAllByRole("progressbar")).toHaveLength(1);
  expect(
    screen.getByRole("progressbar", { name: "/ disk fullness" }),
  ).toHaveAttribute("aria-valuenow", "60");
  expect(screen.getByText("400 MB free of 1 GB")).toBeVisible();
  expect(
    screen.getByRole("button", { name: /Choose disks/ }),
  ).toHaveTextContent("Disks: 1 of 4");
});

it("remembers multiple chosen disks without adding unselected filesystems", () => {
  const view = render(
    <DiskFullnessCard state={fixture()} loading={false} stale={false} />,
  );
  fireEvent.click(screen.getByRole("button", { name: /Choose disks/ }));
  fireEvent.click(screen.getByRole("checkbox", { name: "/data", exact: true }));
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(screen.getAllByRole("progressbar")).toHaveLength(2);
  expect(
    screen.queryByRole("progressbar", { name: "/backup disk fullness" }),
  ).not.toBeInTheDocument();
  view.unmount();
  render(<DiskFullnessCard state={fixture()} loading={false} stale={false} />);
  expect(
    screen.getByRole("progressbar", { name: "/data disk fullness" }),
  ).toBeVisible();
  expect(
    screen.getByRole("button", { name: /Choose disks/ }),
  ).toHaveTextContent("Disks: 2 of 4");
});

it("reports a missing selected disk without substituting a newly discovered disk", () => {
  const state = fixture();
  const view = render(
    <DiskFullnessCard state={state} loading={false} stale={false} />,
  );
  fireEvent.click(screen.getByRole("button", { name: /Choose disks/ }));
  fireEvent.click(screen.getByRole("checkbox", { name: "/data", exact: true }));
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  const removed = {
    ...state,
    inventory: {
      ...state.inventory,
      devices: state.inventory.devices.filter((d) => d.id !== "data"),
    },
  };
  view.rerender(
    <DiskFullnessCard state={removed} loading={false} stale={false} />,
  );
  expect(screen.getByRole("status")).toHaveTextContent(
    /selected disk.*unavailable/i,
  );
  expect(screen.getAllByRole("progressbar")).toHaveLength(1);
  fireEvent.click(screen.getByRole("button", { name: /Choose disks/ }));
  const checkbox = screen.getByRole("checkbox", { name: "/data", exact: true });
  expect(checkbox).toBeChecked();
  fireEvent.click(checkbox);
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
});

it("keeps an intentionally empty selection after reopening the dashboard", () => {
  const view = render(
    <DiskFullnessCard state={fixture()} loading={false} stale={false} />,
  );
  fireEvent.click(screen.getByRole("button", { name: /Choose disks/ }));
  fireEvent.click(screen.getByRole("checkbox", { name: "/", exact: true }));
  view.unmount();
  render(<DiskFullnessCard state={fixture()} loading={false} stale={false} />);
  expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  expect(screen.getByText(/No disks selected/)).toBeVisible();
  expect(screen.getByRole("button", { name: /Choose disks/ })).toBeEnabled();
});

it("keeps the picker usable when storage fails and restores focus on Escape", () => {
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw Error("Denied");
  });
  render(<DiskFullnessCard state={fixture()} loading={false} stale={false} />);
  const button = screen.getByRole("button", { name: /Choose disks/ });
  fireEvent.click(button);
  fireEvent.click(screen.getByRole("checkbox", { name: "/data", exact: true }));
  fireEvent.keyDown(screen.getByRole("group", { name: "Visible disks" }), {
    key: "Escape",
  });
  expect(button).toHaveFocus();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.getAllByRole("progressbar")).toHaveLength(2);
});

it("keeps individual capacities and technical details for the selected disks", () => {
  render(<DiskFullnessCard state={fixture()} loading={false} stale={false} />);
  fireEvent.click(screen.getByRole("button", { name: /Choose disks/ }));
  fireEvent.click(
    screen.getByRole("checkbox", { name: "/backup", exact: true }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(
    screen.getByRole("progressbar", { name: "/backup disk fullness" }),
  ).toHaveAttribute("aria-valuenow", "99");
  fireEvent.click(screen.getByRole("button", { name: "Filesystem details" }));
  const card = within(screen.getByRole("article", { name: "Disk fullness" }));
  expect(card.getByText("990 MB used of 1 GB")).toBeVisible();
  expect(card.queryByText("/data")).not.toBeInTheDocument();
});
