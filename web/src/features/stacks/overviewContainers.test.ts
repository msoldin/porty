import { expect, it, vi } from "vitest";
import { readOverviewContainers } from "./overviewContainers";
import type { Container } from "./types";

const container = (id: string): Container => ({
  id,
  name: id,
  service: "web",
  state: "running",
  health: "healthy",
  image: "web:1",
  networks: [],
  ports: [],
});

it("reads at most four stacks at once and keeps stack order after out-of-order replies", async () => {
  const finish = new Map<string, (items: Container[]) => void>();
  let active = 0;
  let peak = 0;
  const read = vi.fn(
    (id: string) =>
      new Promise<Container[]>((resolve) => {
        active += 1;
        peak = Math.max(peak, active);
        finish.set(id, (items) => {
          active -= 1;
          resolve(items);
        });
      }),
  );
  const result = readOverviewContainers(["a", "b", "c", "d", "e", "f"], read);
  expect(read).toHaveBeenCalledTimes(4);
  finish.get("d")!([container("d1")]);
  await Promise.resolve();
  expect(read).toHaveBeenCalledTimes(5);
  finish.get("b")!([container("b1")]);
  await Promise.resolve();
  expect(read).toHaveBeenCalledTimes(6);
  for (const id of ["f", "e", "c", "a"]) finish.get(id)!([container(id + "1")]);
  expect((await result).rows.map((row) => row.stackId)).toEqual([
    "a",
    "b",
    "c",
    "d",
    "e",
    "f",
  ]);
  expect(peak).toBe(4);
});

it("keeps replicas from successful stacks when another stack read fails", async () => {
  const read = vi.fn(async (id: string) => {
    if (id === "broken") throw new Error("Docker unavailable");
    return id === "one" ? [container("replica-1"), container("replica-2")] : [];
  });
  const result = await readOverviewContainers(["one", "broken", "empty"], read);
  expect(result.rows.map((row) => [row.stackId, row.container.id])).toEqual([
    ["one", "replica-1"],
    ["one", "replica-2"],
  ]);
  expect(result.errors).toEqual([
    { stackId: "broken", message: "Docker unavailable" },
  ]);
});
