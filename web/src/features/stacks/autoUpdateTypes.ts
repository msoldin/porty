export type AutoUpdatePolicy = {
  stackId: string;
  enabled: boolean;
  expression: string;
  revision: number;
  nextRunAt: string;
  pausedReason?: string;
};
export type AutoUpdateStatus = {
  policy: AutoUpdatePolicy;
  available: boolean;
  availabilityReason?: string;
  eligible: boolean;
  eligibilityReason?: string;
  excluded: Record<string, string>;
  lastRun?: {
    id: string;
    outcome?: string;
    reason?: string;
    scheduledAt: string;
    operationId?: string;
  };
};
export type PolicyUpdate = {
  enabled: boolean;
  expression: string;
  expectedRevision: number;
};
