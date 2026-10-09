import { expect, it } from "vitest";
import { mergeMonitoring } from "./monitoringState";
import { snapshotFixture } from "./testFixtures";
it("resets history when Porty restarts", () => {
  const original = snapshotFixture();
  original.samples = [original.current];
  const previous = mergeMonitoring(undefined, original);
  const restarted = snapshotFixture({
    generation: "generation-b",
    samples: [],
  });
  const result = mergeMonitoring(previous, restarted);
  expect(result.generation).toBe("generation-b");
  expect(result.samples).toHaveLength(0);
});
it("preserves exact sequence order and retains only 151 samples", () => {
  const response = snapshotFixture();
  response.samples = Array.from({ length: 160 }, (_, i) => ({
    ...response.current,
    sequence: (9007199254740993n + BigInt(i)).toString(),
  })).reverse();
  const result = mergeMonitoring(undefined, response);
  expect(result.samples).toHaveLength(151);
  expect(result.samples[0].sequence).toBe("9007199254741002");
});
it("preserves unchanged inventory and prunes expired samples without filling gaps", () => {
  const first = snapshotFixture();
  first.samples = [
    { ...first.current, sequence: "1", capturedAt: "2026-10-09T11:54:00Z" },
    {
      ...first.current,
      sequence: "2",
      readings: {
        "cpu.busy": {
          value: null,
          state: "collecting",
          sampledAt: first.serverTime,
        },
      },
    },
  ];
  const previous = mergeMonitoring(undefined, first);
  const next = mergeMonitoring(
    previous,
    snapshotFixture({ reset: false, inventory: undefined }),
  );
  expect(next.inventory).toEqual(first.inventory);
  expect(next.samples).toHaveLength(1);
  expect(next.samples[0].readings["cpu.busy"].value).toBeNull();
});
