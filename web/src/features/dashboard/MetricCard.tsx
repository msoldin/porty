import { cloneElement, type ComponentChildren, type VNode } from "preact";
import type { MetricChartProps } from "./MetricChart";
import { useId, useRef, useState } from "preact/hooks";
import { Dialog } from "../../components/Dialog";
import type { State } from "./types";
import { ReadingStatus } from "./ReadingStatus";
import { MetricGauge } from "./MetricGauge";

function MetricValue({ value }: { value: ComponentChildren }) {
  const parts =
    typeof value === "string" ? value.match(/^(-?[\d.,]+)\s+(.+)$/) : null;
  return parts ? (
    <>
      {parts[1]} <small>{parts[2]}</small>
    </>
  ) : (
    <>{value}</>
  );
}

export function MetricCard({
  label,
  value,
  source,
  state,
  lastSuccessAt,
  chart,
  children,
  details,
  detailsLabel,
  detailLabel = label + " details",
  note,
  gauge,
  availability,
}: {
  label: string;
  value?: ComponentChildren;
  source?: ComponentChildren;
  state: State;
  lastSuccessAt?: string;
  chart?: VNode<MetricChartProps> | false;
  children?: ComponentChildren;
  details?: ComponentChildren;
  detailsLabel?: string;
  detailLabel?: string;
  note?: string;
  availability?: ComponentChildren;
  gauge?: { value?: number | null; capacity?: boolean; label?: string };
}) {
  const id = useId();
  const closeButton = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  return (
    <article
      class={"host-metric" + (gauge ? " is-gauge" : " is-chart")}
      aria-labelledby={id}
      data-state={state}
    >
      <div class={"host-metric-main" + (chart && !gauge ? " has-chart" : "")}>
        <button
          type="button"
          class="host-metric-open"
          aria-label={detailLabel}
          aria-haspopup="dialog"
          aria-expanded={open}
          onClick={() => setOpen(true)}
        />
        {gauge && (
          <MetricGauge
            label={gauge.label ?? label}
            value={gauge.value}
            capacity={gauge.capacity}
          />
        )}
        <h2 id={id} title={label}>
          {label}
        </h2>
        {!gauge && value !== undefined && (
          <div class="host-metric-value">
            <MetricValue value={value} />
          </div>
        )}
        {source && (
          <div
            class="host-metric-source"
            title={typeof source === "string" ? source : undefined}
          >
            {source}
          </div>
        )}
        <ReadingStatus state={state} lastSuccessAt={lastSuccessAt} />
        {children}
        {!gauge && chart}
      </div>
      <Dialog
        open={open}
        title={detailLabel}
        onClose={() => setOpen(false)}
        initialFocusRef={closeButton}
      >
        <button
          ref={closeButton}
          type="button"
          class="metric-dialog-close"
          aria-label="Close details"
          onClick={() => setOpen(false)}
        >
          <span aria-hidden="true">×</span>
        </button>
        <div class="host-metric-details">
          <div class="metric-detail-summary">
            <div>
              {source && <p>{source}</p>}
              <ReadingStatus state={state} lastSuccessAt={lastSuccessAt} />
            </div>
            {value != null && value !== "—" && (
              <div class="metric-detail-current">
                <MetricValue value={value} />
              </div>
            )}
          </div>
          {note && <p class="muted">{note}</p>}
          {availability}
          {chart && (
            <section
              class="metric-detail-history"
              aria-label={label + " history chart"}
            >
              <div class="metric-history-heading">
                <h3>History</h3>
                <span>Last 5 minutes</span>
              </div>
              {cloneElement(chart, { detailed: true })}
              <p class="metric-history-hint">
                <span class="chart-hint-pointer">
                  Hover to inspect · Use arrow keys when focused
                </span>
                <span class="chart-hint-touch">
                  Tap the chart to inspect a reading.
                </span>
              </p>
            </section>
          )}
          {detailsLabel ? (
            <details class="metric-extra-details">
              <summary>{detailsLabel}</summary>
              {details}
            </details>
          ) : (
            details
          )}
        </div>
        <div class="dialog-actions">
          <button type="button" onClick={() => setOpen(false)}>
            Close
          </button>
        </div>
      </Dialog>
    </article>
  );
}
