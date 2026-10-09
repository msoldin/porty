import type { ComponentChildren } from "preact";
import { useId, useState } from "preact/hooks";
import { Dialog } from "../../components/Dialog";
import type { State } from "./types";
import { ReadingStatus } from "./ReadingStatus";
import { MetricGauge } from "./MetricGauge";

export function MetricCard({
  label,
  value,
  source,
  state,
  lastSuccessAt,
  chart,
  children,
  details,
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
  chart?: ComponentChildren;
  children?: ComponentChildren;
  details?: ComponentChildren;
  detailLabel?: string;
  note?: string;
  availability?: ComponentChildren;
  gauge?: { value?: number | null; capacity?: boolean; label?: string };
}) {
  const id = useId();
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
          <div class="host-metric-value">{value}</div>
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
      <Dialog open={open} title={detailLabel} onClose={() => setOpen(false)}>
        <div class="host-metric-details">
          <div class="metric-detail-current">{value}</div>
          {source && <p>{source}</p>}
          <ReadingStatus state={state} lastSuccessAt={lastSuccessAt} />
          {note && <p class="muted">{note}</p>}
          {availability}
          {chart && <div class="metric-detail-history">{chart}</div>}
          {details}
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
