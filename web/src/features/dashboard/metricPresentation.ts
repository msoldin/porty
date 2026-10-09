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
  return {
    label,
    points: (state?.samples ?? []).slice(-151).map((sample) => {
      const reading = series ? sample.readings[series.id] : undefined;
      return {
        time: Date.parse(sample.capturedAt),
        value: reading?.state === "available" ? reading.value : null,
      };
    }),
  };
}
export function reasonLabel(reason?: string): string {
  const labels: Record<string, string> = {
    collecting: "Waiting for the next sample",
    unsupported: "Not supported by this source",
    build_unsupported: "Unavailable in this build",
    no_device: "No device detected",
    permission_denied: "Access denied",
    source_missing: "Source unavailable",
    read_failed: "Unable to read this source",
    invalid_data: "Invalid source data",
    collection_delayed: "Waiting for a fresh reading",
    counter_reset: "Counter reset; collecting",
    limit_exceeded: "Device limit reached",
    disabled: "Monitoring disabled",
    partial_coverage: "Partial coverage",
    host_mount_unavailable: "Host filesystem is not mounted here",
  };
  return reason ? (labels[reason] ?? "Source unavailable") : "";
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
