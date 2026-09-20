import { expect, it } from "vitest";
import { reduceStream } from "./stream";

it("marks explicit stream gaps without claiming output is complete", () => {
  const next = reduceStream(
    { sequence: 5, gap: false, output: "before\n" },
    { type: "gap", sequence: 0, payload: { since: 5 } },
  );
  expect(next.gap).toBe(true);
  expect(next.sequence).toBe(0);
  expect(next.output).toBe("before\n");
});
it("ignores replay duplicates and caps operation output", () => {
  const prior = { sequence: 5, gap: false, output: "before\n" };
  expect(
    reduceStream(prior, {
      type: "operation",
      sequence: 4,
      payload: { output: "old" },
    }),
  ).toEqual(prior);
  const next = reduceStream(prior, {
    type: "operation",
    sequence: 8,
    payload: { output: "x".repeat(100000) },
  });
  expect(next.sequence).toBe(8);
  expect(next.output).toBe("x".repeat(65536));
});
