import { MetricAvailability } from "./MetricAvailability";
import { MetricCard } from "./MetricCard";
import { MetricChart } from "./MetricChart";
import {
  chartSeries,
  currentReading,
  formatMetric,
} from "./metricPresentation";
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
      chart={
        <MetricChart
          unit="percent"
          series={[
            chartSeries(state, device.id, "filesystem_percent", device.name),
          ]}
        />
      }
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
          <dl class="disk-space-summary" aria-label="Storage space">
            <div>
              <dt>Used</dt>
              <dd>{bytes("filesystem_used")}</dd>
            </div>
            <div>
              <dt>Free</dt>
              <dd>{bytes("filesystem_available")}</dd>
            </div>
            <div>
              <dt>Total</dt>
              <dd>{bytes("filesystem_total")}</dd>
            </div>
          </dl>
          <details class="disk-technical-details">
            <summary>Technical details</summary>
            <dl>
              {device.driver && (
                <div>
                  <dt>Filesystem type</dt>
                  <dd>{device.driver}</dd>
                </div>
              )}
              <div>
                <dt>Mount paths</dt>
                <dd>
                  {[
                    ...new Set(
                      device.mountPaths?.length
                        ? device.mountPaths
                        : [device.name],
                    ),
                  ].map((path) => (
                    <code key={path}>{path}</code>
                  ))}
                </dd>
              </div>
              {reserved > 0 && (
                <div>
                  <dt>Reserved space</dt>
                  <dd>{formatMetric(reserved, "bytes")}</dd>
                </div>
              )}
            </dl>
            {reserved > 0 && (
              <p class="muted">
                Reserved space is set aside by the filesystem and is not
                included in Used or Free.
              </p>
            )}
          </details>
        </>
      }
    />
  );
}
