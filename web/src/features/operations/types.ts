export type Operation = {
  id: string;
  kind: string;
  scopeType: string;
  scopeId?: string;
  status: string;
  output?: string;
  outputTruncated: boolean;
  errorCode?: string;
  startedAt?: string;
  completedAt?: string;
};
