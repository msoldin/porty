import type { Operation } from "./types";
export function operationPresentation(operation: Operation): {
  label: string;
  tone: "neutral" | "success" | "danger" | "blue";
  terminal: boolean;
} {
  switch (operation.status) {
    case "queued":
      return { label: "Queued", tone: "blue", terminal: false };
    case "running":
      return { label: "Running", tone: "blue", terminal: false };
    case "succeeded":
      return { label: "Succeeded", tone: "success", terminal: true };
    case "failed":
      return { label: "Failed", tone: "danger", terminal: true };
    case "cancelled":
      return { label: "Cancelled", tone: "neutral", terminal: true };
    default:
      return { label: "Unknown", tone: "neutral", terminal: false };
  }
}
