import { api } from "../../lib/http";
import { stackPath } from "./api";
import type { AutoUpdateStatus, PolicyUpdate } from "./autoUpdateTypes";
export function getAutoUpdate(id: string): Promise<AutoUpdateStatus> {
  return api(`${stackPath(id)}/auto-update`);
}
export function saveAutoUpdate(
  id: string,
  value: PolicyUpdate,
): Promise<AutoUpdateStatus> {
  return api(`${stackPath(id)}/auto-update`, "PUT", value);
}
export function resumeAutoUpdate(
  id: string,
  expectedRevision: number,
): Promise<AutoUpdateStatus> {
  return api(`${stackPath(id)}/auto-update/resume`, "POST", {
    expectedRevision,
  });
}
