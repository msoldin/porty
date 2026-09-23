import { api } from "../../lib/http";
import type { AuditEvent } from "./types";

export function listAudit(): Promise<AuditEvent[] | null> {
  return api("/audit?limit=50");
}
