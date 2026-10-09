import type { State } from "./types";

export function ReadingStatus({
  state,
  lastSuccessAt,
}: {
  state: State;
  lastSuccessAt?: string;
}) {
  if (state === "available") return null;
  const at = lastSuccessAt ? new Date(lastSuccessAt) : undefined;
  return (
    <span class={"host-metric-state is-" + state}>
      <span>
        {state === "stale"
          ? "Stale"
          : state === "collecting"
            ? "Collecting"
            : "Unavailable"}
      </span>
      {state === "stale" && at && Number.isFinite(at.getTime()) && (
        <small class="metric-last-success">
          Last reading:{" "}
          <time dateTime={lastSuccessAt}>{at.toLocaleString()}</time>
        </small>
      )}
    </span>
  );
}
