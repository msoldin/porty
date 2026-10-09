import { formatMetric } from "./metricPresentation";

export function MetricGauge({
  label,
  value,
  capacity = false,
}: {
  label: string;
  value?: number | null;
  capacity?: boolean;
}) {
  const valid = value != null && Number.isFinite(value);
  const percent = valid ? Math.max(0, Math.min(100, value)) : 0;
  const formatted = formatMetric(valid ? value : null, "percent");
  const warning =
    capacity && valid
      ? percent >= 90
        ? "Almost full"
        : percent >= 70
          ? "Filling up"
          : ""
      : "";
  const arc = "M 14 104 A 88 88 0 0 1 190 104";
  return (
    <div
      class="metric-gauge"
      role={valid ? "meter" : undefined}
      aria-label={label}
      aria-valuemin={valid ? 0 : undefined}
      aria-valuemax={valid ? 100 : undefined}
      aria-valuenow={valid ? percent : undefined}
      aria-valuetext={
        valid
          ? formatted +
            (capacity ? " used" : "") +
            (warning ? " · " + warning : "")
          : undefined
      }
    >
      <svg viewBox="0 0 204 116" aria-hidden="true">
        <path d={arc} pathLength="100" class="gauge-track" />
        {valid &&
          (capacity ? (
            <>
              <path
                d={arc}
                pathLength="100"
                class="gauge-safe"
                stroke-dasharray="70 30"
              />
              <path
                d={arc}
                pathLength="100"
                class="gauge-warning"
                stroke-dasharray="20 80"
                stroke-dashoffset="-70"
              />
              <path
                d={arc}
                pathLength="100"
                class="gauge-danger"
                stroke-dasharray="10 90"
                stroke-dashoffset="-90"
              />
              <path
                class="gauge-needle"
                d="M 0 11 Q -2 12 -4 7 L -6 -5 Q 0 -10 6 -5 L 4 7 Q 2 12 0 11 Z"
                transform={`translate(102 104) rotate(${percent * 1.8 - 90}) translate(0 -88)`}
              />
            </>
          ) : (
            <path
              d={arc}
              pathLength="100"
              class="gauge-activity"
              stroke-dasharray={`${percent} 100`}
            />
          ))}
      </svg>
      <span class="gauge-value">{formatted}</span>
      {warning && (
        <span class={"gauge-warning-label" + (percent >= 90 ? " is-full" : "")}>
          {warning}
        </span>
      )}
    </div>
  );
}
