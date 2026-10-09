import { useState } from "preact/hooks";
import { formatMetric, type ChartSeries } from "./metricPresentation";
import type { Unit } from "./types";

export type MetricChartProps = {
  series: ChartSeries[];
  unit: Unit;
  detailed?: boolean;
};

export function MetricChart({
  series,
  unit,
  detailed = false,
}: MetricChartProps) {
  const [selected, setSelected] = useState<number>();
  const points = series[0]?.points ?? [];
  const end = points.at(-1)?.time ?? 0,
    start = end - 300000;
  const values = series.flatMap((item) =>
    item.points.flatMap((point) => (point.value == null ? [] : [point.value])),
  );
  const low =
    unit === "celsius" && values.length
      ? Math.floor(Math.min(...values) - 1)
      : 0;
  const high = unit === "percent" ? 100 : Math.max(low + 1, ...values);
  const x = (time: number) =>
    Math.max(0, Math.min(300, ((time - start) / (end - start)) * 300));
  const height = detailed ? 180 : 64;
  const bottom = detailed ? height : 58;
  const top = detailed ? 0 : 6;
  const y = (value: number) =>
    bottom - ((value - low) / (high - low)) * (bottom - top);
  const ticks = [0, 0.25, 0.5, 0.75, 1];
  const timeLabel = (time: number) =>
    new Date(time).toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    });
  const paths = (item: ChartSeries) => {
    const segments: string[][] = [];
    let segment: string[] = [];
    let previous: number | undefined;
    for (const point of item.points) {
      if (
        point.value == null ||
        (previous !== undefined && point.time - previous > 6000)
      ) {
        if (segment.length) segments.push(segment);
        segment = [];
      }
      if (point.value != null)
        segment.push(
          x(point.time).toFixed(2) + "," + y(point.value).toFixed(2),
        );
      previous = point.time;
    }
    if (segment.length) segments.push(segment);
    return segments;
  };
  const label = series.map((item) => item.label).join(" / ");
  const index =
    selected === undefined ? undefined : Math.min(selected, points.length - 1);
  const hasSelectedReading =
    index !== undefined &&
    series.some((item) => item.points[index]?.value != null);
  function inspect(clientX: number, element: HTMLDivElement) {
    if (!points.length) return;
    const rect = (
      detailed ? element.querySelector("svg")! : element
    ).getBoundingClientRect();
    const time =
      start +
      Math.max(0, Math.min(1, (clientX - rect.left) / (rect.width || 1))) *
        (end - start);
    let nearest = 0;
    for (let i = 1; i < points.length; i++)
      if (
        Math.abs(points[i].time - time) < Math.abs(points[nearest].time - time)
      )
        nearest = i;
    setSelected(nearest);
  }
  return (
    <div
      class={"metric-chart" + (detailed ? " is-detailed" : "")}
      role="group"
      aria-label={label + " history"}
      tabIndex={0}
      onFocus={() => setSelected(Math.max(0, points.length - 1))}
      onBlur={() => setSelected(undefined)}
      onKeyDown={(event) => {
        if (
          !["ArrowLeft", "ArrowRight", "Home", "End", "Escape"].includes(
            event.key,
          )
        )
          return;
        if (event.key === "Escape") {
          setSelected(undefined);
          return;
        }
        event.preventDefault();
        setSelected(
          event.key === "Home"
            ? 0
            : event.key === "End"
              ? Math.max(0, points.length - 1)
              : Math.max(
                  0,
                  Math.min(
                    points.length - 1,
                    (index ?? points.length - 1) +
                      (event.key === "ArrowLeft" ? -1 : 1),
                  ),
                ),
        );
      }}
      onPointerMove={(event) => inspect(event.clientX, event.currentTarget)}
      onPointerLeave={(event) => {
        if (event.pointerType !== "touch") setSelected(undefined);
      }}
      onPointerDown={(event) => {
        event.currentTarget.focus();
        inspect(event.clientX, event.currentTarget);
      }}
    >
      {detailed && (
        <>
          <div class="chart-value-axis" aria-label="Value scale">
            {ticks.map((tick) => (
              <span key={tick}>
                {formatMetric(high - tick * (high - low), unit)}
              </span>
            ))}
          </div>
          <div class="chart-time-axis" aria-label="Time scale">
            {points.length > 0 &&
              [0, 0.5, 1].map((tick) => (
                <time key={tick}>
                  {timeLabel(start + tick * (end - start))}
                </time>
              ))}
          </div>
        </>
      )}
      <svg
        viewBox={`0 0 300 ${height}`}
        preserveAspectRatio="none"
        aria-hidden="true"
      >
        {detailed && (
          <g class="chart-grid">
            {ticks.map((tick) => (
              <g key={tick}>
                <line x1="0" x2="300" y1={tick * height} y2={tick * height} />
                <line x1={tick * 300} x2={tick * 300} y1="0" y2={height} />
              </g>
            ))}
          </g>
        )}
        {series.map((item, line) =>
          paths(item).map((segment, i) => (
            <g key={line + "-" + i} class={"chart-series chart-series-" + line}>
              {segment.length > 1 && (
                <polygon
                  points={
                    segment[0].split(",")[0] +
                    "," +
                    height +
                    " " +
                    segment.join(" ") +
                    " " +
                    segment.at(-1)!.split(",")[0] +
                    "," +
                    height
                  }
                  fill="currentColor"
                  opacity={detailed ? ".14" : ".07"}
                />
              )}
              {segment.length === 1 && detailed && (
                <circle
                  cx={segment[0].split(",")[0]}
                  cy={segment[0].split(",")[1]}
                  r="2"
                  fill="currentColor"
                />
              )}
              <polyline
                points={segment.join(" ")}
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-dasharray={line % 2 ? "5 3" : undefined}
                vector-effect="non-scaling-stroke"
              />
            </g>
          )),
        )}
        {detailed &&
          hasSelectedReading &&
          index !== undefined &&
          points[index] && (
            <line
              class="chart-cursor"
              x1={x(points[index].time)}
              x2={x(points[index].time)}
              y1="0"
              y2={height}
            />
          )}
      </svg>
      {detailed && values.length === 0 && (
        <span class="chart-empty">Waiting for history</span>
      )}
      {!detailed && unit === "celsius" && values.length > 0 && (
        <span class="chart-range">
          Range: {formatMetric(low, unit)}–{formatMetric(high, unit)}
        </span>
      )}
      {series.length > 1 && (
        <div class="chart-legend">
          {series.map((item, i) => (
            <span class={"chart-series-" + i} key={item.label}>
              <i class={i % 2 ? "dashed" : ""} />
              {item.label}
            </span>
          ))}
        </div>
      )}
      {hasSelectedReading && index !== undefined && points[index] && (
        <output class="chart-inspection" aria-live="off">
          <time>{new Date(points[index].time).toLocaleTimeString()}</time>
          {" · "}
          {series
            .map(
              (item) =>
                item.label +
                ": " +
                (item.points[index]?.value == null
                  ? "No reading"
                  : formatMetric(item.points[index].value, unit)),
            )
            .join(" · ")}
        </output>
      )}
    </div>
  );
}
