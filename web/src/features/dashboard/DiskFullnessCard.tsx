import { MetricAvailability } from "./MetricAvailability";
import { MetricCard } from "./MetricCard";
import { currentReading, formatMetric } from "./metricPresentation";
import type { Device, MetricKind, MonitoringState } from "./types";

export function DiskFullnessCard({
  device,
  state,
  stale,
  now,
}: {
  device: Device;
  state?: MonitoringState;
  stale: boolean;
  now?: number;
}) {
  const read = (metric: MetricKind) =>
    currentReading(state, device.id, metric, now, stale);
  const bytes = (metric: MetricKind) =>
    formatMetric(read(metric)?.value, "bytes");
  const percent = read("filesystem_percent");
  const total = read("filesystem_total")?.value;
  const used = read("filesystem_used")?.value;
  const available = read("filesystem_available")?.value;
  const reserved =
    total != null && used != null && available != null
      ? total - used - available
      : 0;
  return (
    <MetricCard
      label={device.name + " · Disk fullness"}
      availability={
        <MetricAvailability
          coverage={state?.coverage}
          sources={["filesystems", device.id]}
          reading={percent}
        />
      }
      value={formatMetric(percent?.value, "percent")}
      gauge={{
        value: percent?.value,
        capacity: true,
        label: device.name + " disk fullness",
      }}
      source={
        bytes("filesystem_available") + " free of " + bytes("filesystem_total")
      }
      state={percent?.state ?? "unavailable"}
      lastSuccessAt={percent?.lastSuccessAt}
      detailLabel={device.name + " filesystem details"}
      details={
        <>
          <p>
            {[device.driver, ...(device.mountPaths ?? [device.name])]
              .filter(Boolean)
              .join(" · ")}
          </p>
          <p>
            {bytes("filesystem_used")} used of {bytes("filesystem_total")}
          </p>
          <p>{bytes("filesystem_available")} available</p>
          <p class="muted">Capacity guide: 70% filling up · 90% almost full.</p>
          {reserved > 0 && (
            <p class="muted">
              {formatMetric(reserved, "bytes")} reserved; excluded from used and
              available capacity.
            </p>
          )}
        </>
      }
    />
  );
}
