import { reasonLabel } from "./metricPresentation";
import type { Coverage, Reading } from "./types";

function coverageMessage(item: Coverage): string {
  if (item.source === "sensors" && item.reason === "no_device")
    return "No temperature sensors are exposed to this environment.";
  const names: Record<string, string> = {
    cpu: "CPU cores",
    network: "network interfaces",
    disks: "disks",
    filesystems: "disks",
    sensors: "temperature sensors",
    gpu: "graphics devices",
    nvidia: "graphics devices",
  };
  const message =
    item.reason === "partial_coverage"
      ? names[item.source]
        ? `Some ${names[item.source]} aren’t included in these readings.`
        : "Some readings aren’t available."
      : reasonLabel(item.reason) ||
        (item.partial ? "Some readings aren’t available." : "");
  return (
    message +
    (item.omitted > 0
      ? ` At least ${item.omitted} ${item.omitted === 1 ? "device isn’t" : "devices aren’t"} shown.`
      : "")
  );
}

export function MetricAvailability({
  coverage = [],
  sources,
  reading,
}: {
  coverage?: Coverage[];
  sources: (string | undefined)[];
  reading?: Reading;
}) {
  const relevant = coverage.filter((item) => sources.includes(item.source));
  const messages = relevant.map(coverageMessage).filter(Boolean);
  if (
    reading?.reason &&
    !relevant.some((item) => item.reason === reading.reason)
  )
    messages.push(reasonLabel(reading.reason));
  if (!messages.length) return null;
  return (
    <div class="metric-availability">
      {Array.from(new Set(messages)).map((message) => (
        <p key={message}>{message}</p>
      ))}
    </div>
  );
}
