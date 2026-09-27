export type Alert = {
  id: string;
  key: { stackId: string; problem: string; target: string };
  stackName: string;
  revision: number;
  episode: number;
  count: number;
  summary: string;
  operationId?: string;
  firstAt: string;
  latestAt: string;
  acknowledgedAt?: string;
  acknowledgedBy?: string;
  resolvedAt?: string;
  resolvedBy?: string;
  resolution?: string;
  canResolveManually: boolean;
};
export type AlertPage = {
  items: Alert[];
  unacknowledgedCount: number;
  total: number;
};
export type AlertView = "attention" | "open" | "unacknowledged" | "all";
export type AlertEvent = {
  id: string;
  alertId: string;
  episode: number;
  kind: string;
  actorId?: string;
  at: string;
  note?: string;
  operationId?: string;
};
export type AlertDetail = { alert: Alert; history: AlertEvent[] };
