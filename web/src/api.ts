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
export type Repository = {
  configured: boolean;
  branch: string;
  dirty: boolean;
  ahead: number;
  behind: number;
  paths: string[] | null;
};
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
export type Commit = {
  sha: string;
  subject: string;
  author: string;
  time: string;
};
export type Deployment = {
  id: string;
  status: string;
  gitCommit?: string;
  dirty: boolean;
  startedAt: string;
  errorCode?: string;
};
export type Session = { username: string; csrfToken: string };
export type AuditEvent = {
  id: string;
  action: string;
  targetType: string;
  targetId?: string;
  outcome: string;
  requestId: string;
  occurredAt: string;
};
export type RepositorySetupState = "unregistered" | "registered" | "ready";
export type RepositorySetupMode = "init" | "remote" | "adopt";
export type RepositoryPathState = "empty" | "worktree" | "occupied" | "invalid";
export type RepositoryAuthType = "none" | "https" | "ssh";
export type GitIdentity = { name: string; email: string };
export type RemoteAuthenticationInput =
  | { type: "none" }
  | { type: "https"; username: string; secret: string }
  | { type: "ssh" };
export type RepositoryRemoteInput = {
  url: string;
  authentication: RemoteAuthenticationInput;
};
export type RepositoryRemoteSummary = {
  name: string;
  url: string;
  authType: RepositoryAuthType;
  managed: boolean;
};
export type RepositorySetupStatus = {
  state: RepositorySetupState;
  required: boolean;
  pathState: RepositoryPathState;
  modes: Array<{
    mode: RepositorySetupMode;
    available: boolean;
    reason?: string;
  }>;
  branch?: string;
  author: GitIdentity;
  defaultAuthor: GitIdentity;
  existingRemote?: RepositoryRemoteSummary;
  managedRemote?: RepositoryRemoteSummary;
  ssh: {
    identityAvailable: boolean;
    knownHostsAvailable: boolean;
    usable: boolean;
  };
};
export type RemoteInspectionRequest = { remote: RepositoryRemoteInput };
export type RemoteInspection = {
  remoteUrl: string;
  defaultBranch?: string;
  branches: string[];
  empty: boolean;
  suggestedBranch: string;
};
export type RepositorySetupRequest = {
  mode: RepositorySetupMode;
  branch: string;
  author: GitIdentity;
  remote?: RepositoryRemoteInput;
  manageExistingRemote?: boolean;
};
export type RepositoryRemoteRequest = {
  remote: RepositoryRemoteInput;
  branch: string;
  replaceExisting: boolean;
};

export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}
let csrfToken = "";
export function setCSRF(token: string) {
  csrfToken = token;
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
  headers: Record<string, string> = {},
): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    method,
    credentials: "same-origin",
    headers: {
      ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
      ...(method !== "GET" ? { "X-CSRF-Token": csrfToken } : {}),
      ...headers,
    },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => null);
    throw new APIError(
      response.status,
      data?.error?.code || "RequestFailed",
      data?.error?.message || `Request failed (${response.status})`,
    );
  }
  return response.status === 204 ? (undefined as T) : response.json();
}
export const stackPath = (id: string) => `/stacks/${encodeURIComponent(id)}`;
export const message = (error: unknown) =>
  error instanceof Error ? error.message : "Request failed";

export function getRepositorySetupStatus(): Promise<RepositorySetupStatus> {
  return api("/repository/setup/status");
}

export function inspectRepositoryRemote(
  request: RemoteInspectionRequest,
): Promise<RemoteInspection> {
  return api("/repository/setup/inspect-remote", "POST", request);
}

export function setupRepository(
  request: RepositorySetupRequest,
): Promise<RepositorySetupStatus> {
  return api("/repository/setup", "POST", request);
}

export function configureRepositoryRemote(
  request: RepositoryRemoteRequest,
): Promise<RepositorySetupStatus> {
  return api("/repository/remote", "PUT", request);
}

export function removeRepositoryRemote(): Promise<RepositorySetupStatus> {
  return api("/repository/remote", "DELETE");
}
