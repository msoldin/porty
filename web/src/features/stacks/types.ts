export type Stack = {
  id: string;
  directoryName: string;
  composeProjectName: string;
  archivedAt?: string;
  createdAt: string;
  updatedAt: string;
  lastDeploymentAt?: string;
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
  image: string;
  networks: string[];
  ports: ContainerPort[];
};

export type ContainerPort = {
  host: string;
  targetPort: number;
  publishedPort: number;
  protocol: string;
};

export type ContainerAction = "start" | "stop" | "restart";

export type ContainerLogs = { output: string; truncated: boolean };

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
