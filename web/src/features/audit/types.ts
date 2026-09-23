export type AuditEvent = {
  id: string;
  action: string;
  targetType: string;
  targetId?: string;
  outcome: string;
  requestId: string;
  occurredAt: string;
};
