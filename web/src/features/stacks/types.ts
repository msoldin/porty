export type Stack = {
  id: string;
  directoryName: string;
  composeProjectName: string;
  archivedAt?: string;
  createdAt: string;
  updatedAt: string;
  state?: StackState;
};

export type StackState = { runtime: string; freshness: string };

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
