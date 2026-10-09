import type { ComponentChildren } from "preact";
import { useId, useState } from "preact/hooks";
import { Icon } from "../../components/Icon";
import type { State } from "./types";
import { ReadingStatus } from "./ReadingStatus";
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
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  return (
    <article class="host-metric" aria-labelledby={id} data-state={state}>
      <div class={"host-metric-main" + (chart ? " has-chart" : "")}>
        <h2 id={id}>{label}</h2>
        {value !== undefined && <div class="host-metric-value">{value}</div>}
        {source && <div class="host-metric-source">{source}</div>}
        <ReadingStatus state={state} lastSuccessAt={lastSuccessAt} />
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
