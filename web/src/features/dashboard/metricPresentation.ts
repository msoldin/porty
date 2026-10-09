import type {
  Device,
  MetricKind,
  MonitoringState,
  Reading,
  Unit,
} from "./types";
export type ChartSeries = {
  label: string;
  points: { time: number; value: number | null }[];
};
const number = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
export function formatMetric(
  value: number | null | undefined,
  unit: Unit,
  binary = false,
): string {
  if (value == null || !Number.isFinite(value)) return "—";
  if (unit === "percent") return number.format(value) + "%";
  if (unit === "celsius") return number.format(value) + " °C";
  const base = binary ? 1024 : 1000;
  const units = binary
    ? ["B", "KiB", "MiB", "GiB", "TiB", "PiB"]
    : ["B", "kB", "MB", "GB", "TB", "PB"];
  const index =
    value > 0
      ? Math.min(units.length - 1, Math.floor(Math.log(value) / Math.log(base)))
      : 0;
  return (
    number.format(value / Math.pow(base, Math.max(0, index))) +
    " " +
    units[Math.max(0, index)] +
    (unit === "bytes_per_second" ? "/s" : "")
  );
}
export function selectDevice(
  devices: Device[],
  saved?: string,
  requireDefault = false,
): Device | undefined {
  return (
    devices.find((device) => device.id === saved) ??
    devices.find((device) => device.default) ??
    (requireDefault ? undefined : devices[0])
  );
}
export function currentReading(
  state: MonitoringState | undefined,
  device: string | undefined,
  metric: MetricKind,
  now?: number,
  stale = false,
): Reading | undefined {
  const series = state?.inventory.series.find(
    (series) => series.deviceId === device && series.metric === metric,
  );
  const reading = series ? state?.current.readings[series.id] : undefined;
  if (
    reading?.state === "available" &&
    (stale ||
      (now ?? Date.parse(state!.serverTime)) -
        Date.parse(reading.lastSuccessAt ?? reading.sampledAt) >=
        6000)
  )
    return { ...reading, state: "stale", reason: "collection_delayed" };
  return reading;
}
export function chartSeries(
  state: MonitoringState | undefined,
  device: string | undefined,
  metric: MetricKind,
  label: string,
): ChartSeries {
  const series = state?.inventory.series.find(
    (series) => series.deviceId === device && series.metric === metric,
  );
  const observations = new Set<string>();
  return {
    label,
    points: (state?.samples ?? []).slice(-151).map((sample) => {
      const reading = series ? sample.readings[series.id] : undefined;
      const fresh =
        reading?.state === "available" && !observations.has(reading.sampledAt);
      if (fresh) observations.add(reading.sampledAt);
      return {
        time: Date.parse(fresh ? reading.sampledAt : sample.capturedAt),
        value: fresh ? reading.value : null,
      };
    }),
  };
}
export function reasonLabel(reason?: string): string {
  const labels: Record<string, string> = {
    collecting: "Waiting for the next sample",
    unsupported: "This environment does not provide this reading.",
    build_unsupported:
      "This Porty build does not include support for these readings.",
    no_device: "No compatible device is exposed to this environment.",
    permission_denied: "Porty does not have permission to read this metric.",
    source_missing: "This reading is not available in the current environment.",
    read_failed:
      "Porty could not read this metric. It will try again automatically.",
    invalid_data:
      "The device returned an invalid reading. Porty will try again automatically.",
    collection_delayed:
      "Updates are delayed. The last successful reading is shown.",
    counter_reset:
      "Waiting for a new sample after the device restarted or its counter reset.",
    limit_exceeded:
      "Some devices are not shown because the monitoring limit has been reached.",
    disabled: "Host monitoring is turned off.",
    partial_coverage: "Some readings are not available.",
    host_mount_unavailable:
      "This disk is not accessible from the environment running Porty.",
  };
  return reason
    ? (labels[reason] ??
        "This reading is not available in the current environment.")
    : "";
}
export function uptime(
  bootTime: string | undefined,
  now: number | undefined,
): string {
  if (!bootTime || !now) return "";
  const minutes = Math.max(0, Math.floor((now - Date.parse(bootTime)) / 60000));
  if (!Number.isFinite(minutes)) return "";
  return minutes >= 1440
    ? "Up " + Math.floor(minutes / 1440) + " days"
    : minutes >= 60
      ? "Up " + Math.floor(minutes / 60) + " hours"
      : "Up " + minutes + " minutes";
}
