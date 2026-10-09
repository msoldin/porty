import { expect, it } from "vitest";
import {
  formatMetric,
  currentReading,
  selectDevice,
  chartSeries,
} from "./metricPresentation";
import { mergeMonitoring } from "./monitoringState";
import { snapshotFixture } from "./testFixtures";
it("leaves gaps for repeated observations but keeps unchanged fresh values", () => {
  const fixture = snapshotFixture();
  const start = Date.parse(fixture.serverTime);
  fixture.samples = [0, 2000, 4000, 6000].map((delta, i) => ({
    ...fixture.current,
    sequence: String(i + 1),
    capturedAt: new Date(start + delta).toISOString(),
    readings: {
      "cpu.busy": {
        ...fixture.current.readings["cpu.busy"],
        sampledAt: new Date(start + (i === 3 ? 5500 : 0)).toISOString(),
      },
    },
  }));
  fixture.serverTime = fixture.samples[3].capturedAt;
  const points = chartSeries(
    mergeMonitoring(undefined, fixture),
    "cpu",
    "cpu_busy",
    "CPU",
  ).points;
  expect(points.map((point) => point.value)).toEqual([25, null, null, 25]);
  expect(points[3].time).toBe(start + 5500);
});
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
