import type { MonitoringState, Sample, Snapshot } from "./types";
export function mergeMonitoring(
  previous: MonitoringState | undefined,
  response: Snapshot,
): MonitoringState {
  const reset = response.reset || previous?.generation !== response.generation;
  const inventory = response.inventory ??
    (!reset ? previous?.inventory : undefined) ?? {
      revision: "",
      devices: [],
      series: [],
    };
  const retained = new Map<string, Sample>();
  const cutoff = Math.max(
    Date.parse(response.windowStart),
    Date.parse(response.serverTime) - 300000,
  );
  for (const sample of [
    ...(!reset ? (previous?.samples ?? []) : []),
    ...(response.samples ?? []),
  ]) {
    if (Date.parse(sample.capturedAt) >= cutoff)
      retained.set(sample.sequence, sample);
  }
  const samples = Array.from(retained.values())
    .sort((a, b) =>
      BigInt(a.sequence) < BigInt(b.sequence)
        ? -1
        : BigInt(a.sequence) > BigInt(b.sequence)
          ? 1
          : 0,
    )
    .slice(-151);
  return { ...response, inventory, samples, coverage: response.coverage ?? [] };
}
