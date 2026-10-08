import type { Repository } from "../repository/types";

export type StatusTone = "neutral" | "success" | "warning" | "danger" | "blue";
export type StatusPresentation = { label: string; tone: StatusTone };

export function stackRuntimePresentation(value?: string): StatusPresentation {
  switch (value) {
    case "running":
      return { label: "RUNNING", tone: "success" };
    case "sleeping":
      return { label: "SLEEPING", tone: "blue" };
    case "on_demand":
      return { label: "RUNNING · ON DEMAND", tone: "blue" };
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
  onDemandSleeping = false,
): StatusPresentation & { health?: string; healthTone?: StatusTone } {
  if (state === "exited" && onDemandSleeping)
    return { label: "SLEEPING", tone: "blue" };
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

export function deploymentLabel(freshness?: string): string {
  switch (freshness) {
    case "current":
      return "Current";
    case "changes_pending":
      return "Changes pending";
    case "deploying":
      return "Deploying";
    case "never_deployed":
      return "Never deployed";
    case "unverifiable":
      return "Unverifiable";
    default:
      return "Unverified";
  }
}
