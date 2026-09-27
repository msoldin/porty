import { api } from "../../lib/http";
import type { Alert, AlertPage, AlertView, AlertDetail } from "./types";
export function listAlerts(
  stackId: string | undefined,
  view: AlertView,
  offset: number,
): Promise<AlertPage> {
  const query = new URLSearchParams({
    view,
    offset: String(offset),
    limit: "25",
  });
  if (stackId) query.set("stackId", stackId);
  return api(`/alerts?${query}`);
}
export function getAlert(id: string, offset = 0): Promise<AlertDetail> {
  return api(`/alerts/${encodeURIComponent(id)}?limit=25&offset=${offset}`);
}
export function acknowledgeAlert(
  id: string,
  expectedRevision: number,
): Promise<Alert> {
  return api(`/alerts/${encodeURIComponent(id)}/acknowledge`, "POST", {
    expectedRevision,
  });
}
export function resolveAlert(
  id: string,
  expectedRevision: number,
  note: string,
): Promise<Alert> {
  return api(`/alerts/${encodeURIComponent(id)}/resolve`, "POST", {
    expectedRevision,
    note,
  });
}
