export interface OnDemandPolicy {
  name: string;
  enabled: boolean;
  members: string[];
  wakeThreshold: number;
  wakeWindowMs: number;
  idleSeconds: number;
  minRuntimeSeconds: number;
  startupSeconds: number;
  stopGraceSeconds: number;
}
export interface OnDemandGroup extends OnDemandPolicy {
  id: string;
  stackId: string;
  revision: number;
  phase: "running" | "sleeping" | "starting" | "stopping" | "unknown";
  holdReason?: string;
  pausedReason?: string;
  observationReason?: string;
}
export interface OnDemandUpdate extends OnDemandPolicy {
  expectedRevision: number;
}
export type OnDemandAction = "hold" | "resume" | "delete";
export const defaultOnDemandPolicy: OnDemandPolicy = {
  name: "",
  enabled: true,
  members: [],
  wakeThreshold: 1,
  wakeWindowMs: 1000,
  idleSeconds: 600,
  minRuntimeSeconds: 120,
  startupSeconds: 300,
  stopGraceSeconds: 120,
};
export function groupPolicy(g: OnDemandPolicy): OnDemandPolicy {
  const {
    name,
    enabled,
    members,
    wakeThreshold,
    wakeWindowMs,
    idleSeconds,
    minRuntimeSeconds,
    startupSeconds,
    stopGraceSeconds,
  } = g;
  return {
    name,
    enabled,
    members,
    wakeThreshold,
    wakeWindowMs,
    idleSeconds,
    minRuntimeSeconds,
    startupSeconds,
    stopGraceSeconds,
  };
}
