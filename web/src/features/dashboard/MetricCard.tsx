import type { ComponentChildren } from "preact";
import { useId, useState } from "preact/hooks";
import { Icon } from "../../components/Icon";
import type { State } from "./types";
export function MetricCard({
  label,
  value,
  source,
  state,
  chart,
  children,
  details,
  detailLabel = label + " details",
  note,
}: {
  label: string;
  value?: ComponentChildren;
  source?: ComponentChildren;
  state: State;
  chart?: ComponentChildren;
  children?: ComponentChildren;
  details?: ComponentChildren;
  detailLabel?: string;
  note?: string;
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const status =
    state === "stale"
      ? "Stale"
      : state === "unavailable"
        ? "Unavailable"
        : state === "collecting"
          ? "Collecting"
          : "";
  return (
    <article class="host-metric" aria-labelledby={id} data-state={state}>
      <div class={"host-metric-main" + (chart ? " has-chart" : "")}>
        <h2 id={id}>{label}</h2>
        {value !== undefined && <div class="host-metric-value">{value}</div>}
        {source && <div class="host-metric-source">{source}</div>}
        {status && (
          <span class={"host-metric-state is-" + state}>{status}</span>
        )}
        {note && <div class="host-metric-note">{note}</div>}
        {children}
        {chart}
      </div>
      <button
        type="button"
        class="host-metric-toggle"
        aria-expanded={open}
        aria-controls={id + "-details"}
        onClick={() => setOpen(!open)}
      >
        {detailLabel}
        <Icon name="Chevron" />
      </button>
      {open && (
        <div class="host-metric-details" id={id + "-details"}>
          {details}
        </div>
      )}
    </article>
  );
}
