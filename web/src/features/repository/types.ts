export type Repository = {
  configured: boolean;
  branch: string;
  dirty: boolean;
  ahead: number;
  behind: number;
  paths: string[] | null;
};

export type Commit = {
  sha: string;
  subject: string;
  author: string;
  time: string;
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
