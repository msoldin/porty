import { api } from "../../lib/http";
import { stackPath } from "./api";
import type {
  OnDemandAction,
  OnDemandGroup,
  OnDemandUpdate,
} from "./onDemandTypes";
export function listOnDemand(id: string): Promise<OnDemandGroup[]> {
  return api(`${stackPath(id)}/on-demand`);
}
export function saveOnDemand(
  id: string,
  groupId: string,
  update: OnDemandUpdate,
): Promise<OnDemandGroup> {
  return api(
    `${stackPath(id)}/on-demand${groupId ? `/${encodeURIComponent(groupId)}` : ""}`,
    groupId ? "PUT" : "POST",
    update,
  );
}
export function changeOnDemand(
  id: string,
  groupId: string,
  action: OnDemandAction,
  expectedRevision: number,
): Promise<OnDemandGroup | undefined> {
  return api(
    `${stackPath(id)}/on-demand/${encodeURIComponent(groupId)}${action === "delete" ? "" : `/${action}`}`,
    action === "delete" ? "DELETE" : "POST",
    { expectedRevision },
  );
}
