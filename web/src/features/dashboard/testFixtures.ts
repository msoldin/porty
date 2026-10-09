import type { Snapshot } from "./types";
export function snapshotFixture(overrides: Partial<Snapshot> = {}): Snapshot {
  const now = "2026-10-09T12:00:00.000Z";
  return {
    generation: "generation-a",
    cursor: "cursor-a",
    reset: true,
    serverTime: now,
    windowStart: "2026-10-09T11:55:00.000Z",
    host: { name: "porty-host", os: "Linux" },
    inventory: {
      revision: "inventory-a",
      devices: [{ id: "cpu", kind: "host", name: "All CPUs", default: true }],
      series: [
        {
          id: "cpu.busy",
          deviceId: "cpu",
          metric: "cpu_busy",
          unit: "percent",
        },
      ],
    },
    current: {
      sequence: "1",
      capturedAt: now,
      readings: {
        "cpu.busy": {
          value: 25,
          state: "available",
          sampledAt: now,
          lastSuccessAt: now,
        },
      },
    },
    samples: [],
    coverage: [],
    ...overrides,
  };
}
