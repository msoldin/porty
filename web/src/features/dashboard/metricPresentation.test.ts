import { expect, it } from "vitest";
import {
  formatMetric,
  currentReading,
  selectDevice,
  chartSeries,
} from "./metricPresentation";
import { mergeMonitoring } from "./monitoringState";
import { snapshotFixture } from "./testFixtures";
it("uses decimal rates and disk capacity but binary memory", () => {
  expect(formatMetric(1500000, "bytes_per_second")).toBe("1.5 MB/s");
  expect(formatMetric(1073741824, "bytes", true)).toBe("1 GiB");
  expect(formatMetric(1000000000, "bytes")).toBe("1 GB");
  expect(formatMetric(null, "percent")).toBe("—");
});
it("keeps last known values stale using server-relative time", () => {
  const state = mergeMonitoring(undefined, snapshotFixture());
  expect(
    currentReading(
      state,
      "cpu",
      "cpu_busy",
      Date.parse(state.serverTime) + 6000,
    )?.state,
  ).toBe("stale");
});
it("never chooses a GPU temperature as the default CPU sensor", () => {
  const devices = [
    { id: "gpu-temp", kind: "sensor" as const, name: "GPU", default: false },
  ];
  expect(selectDevice(devices, undefined, true)).toBeUndefined();
  expect(selectDevice(devices, "removed", true)).toBeUndefined();
  expect(selectDevice(devices, "gpu-temp", true)?.name).toBe("GPU");
});
it("keeps missing and stale history as chart gaps", () => {
  const fixture = snapshotFixture();
  fixture.samples = [
    fixture.current,
    {
      ...fixture.current,
      sequence: "2",
      readings: {
        "cpu.busy": { ...fixture.current.readings["cpu.busy"], state: "stale" },
      },
    },
  ];
  const series = chartSeries(
    mergeMonitoring(undefined, fixture),
    "cpu",
    "cpu_busy",
    "CPU",
  );
  expect(series.points.map((point) => point.value)).toEqual([25, null]);
});
