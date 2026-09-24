import type { Repository } from "../repository/types";

export type StatusTone = "neutral" | "success" | "warning" | "danger" | "blue";
export type StatusPresentation = { label: string; tone: StatusTone };

export function stackRuntimePresentation(value?: string): StatusPresentation {
  switch (value) {
    case "running":
      return { label: "RUNNING", tone: "success" };
    case "stopped":
      return { label: "STOPPED", tone: "danger" };
    case "partial":
      return { label: "PARTIALLY RUNNING", tone: "warning" };
    case "unhealthy":
      return { label: "UNHEALTHY", tone: "danger" };
    default:
      return { label: "UNKNOWN", tone: "neutral" };
  }
}

export function containerStatePresentation(
  state?: string,
  health?: string,
): StatusPresentation & { health?: string; healthTone?: StatusTone } {
  const base: StatusPresentation =
    state === "running"
      ? { label: "RUNNING", tone: "success" }
      : state === "created" || state === "exited"
        ? { label: "STOPPED", tone: "danger" }
        : { label: "UNKNOWN", tone: "neutral" };
  if (!health) return base;
  return {
    ...base,
    health: health[0].toUpperCase() + health.slice(1).toLowerCase(),
    healthTone:
      health.toLowerCase() === "unhealthy"
        ? "danger"
        : health.toLowerCase() === "healthy"
          ? "success"
          : "neutral",
  };
}

export function remoteTone(
  repo?: Pick<Repository, "ahead" | "behind"> | null,
): StatusTone {
  if (!repo) return "neutral";
  if (repo.behind) return "danger";
  return repo.ahead ? "warning" : "success";
}

export function deploymentTone(freshness?: string): StatusTone {
  switch (freshness) {
    case "current":
      return "success";
    case "changes_pending":
      return "warning";
    case "deploying":
      return "blue";
    default:
      return "neutral";
  }
}
