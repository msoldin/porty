export type Operation = {
  trigger?: string;
  serviceUpdates?: {
    service: string;
    beforeImageId: string;
    targetImageId: string;
    actualImageId: string;
    outcome: string;
  }[];
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
