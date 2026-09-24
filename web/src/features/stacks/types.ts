export type Stack = {
  id: string;
  directoryName: string;
  composeProjectName: string;
  archivedAt?: string;
  createdAt: string;
  updatedAt: string;
  state?: StackState;
};

export type StackState = {
  runtime: string;
  freshness: string;
  hasDeployed: boolean;
};

export type Container = {
  id: string;
  name: string;
  service: string;
  state: string;
  health: string;
};

export type ContainerAction = "start" | "stop" | "restart";

export type FileEntry = {
  path: string;
  isDirectory: boolean;
  editable: boolean;
  size: number;
};

export type FileContent = {
  path: string;
  content: string;
  hash: string;
  size: number;
};

export type Deployment = {
  id: string;
  status: string;
  gitCommit?: string;
  dirty: boolean;
  startedAt: string;
  errorCode?: string;
};
