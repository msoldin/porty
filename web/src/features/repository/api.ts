import { api } from "../../lib/http";
import type { Operation } from "../operations/types";
import type {
  Commit,
  Repository,
  RepositorySetupStatus,
  RemoteInspectionRequest,
  RemoteInspection,
  RepositorySetupRequest,
  RepositoryRemoteRequest,
} from "./types";

export function getRepositoryStatus(): Promise<Repository> {
  return api("/repository/status");
}

export function listCommits(): Promise<Commit[] | null> {
  return api("/repository/history?limit=50");
}

export function runRepositoryAction(action: string): Promise<Operation> {
  return api(`/repository/actions/${action}`, "POST");
}

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
