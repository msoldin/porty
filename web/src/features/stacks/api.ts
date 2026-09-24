import { api } from "../../lib/http";
import type { Operation } from "../operations/types";
import type {
  Deployment,
  FileContent,
  FileEntry,
  Stack,
  StackState,
} from "./types";

export const stackPath = (id: string): string =>
  `/stacks/${encodeURIComponent(id)}`;

export async function listStacksWithState(): Promise<Stack[]> {
  const items = await api<Stack[] | null>("/stacks");
  return Promise.all(
    (items || []).map(async (stack) => {
      try {
        return {
          ...stack,
          state: await api<StackState>(`${stackPath(stack.id)}/state`),
        };
      } catch {
        return stack;
      }
    }),
  );
}

export function createStack(name: FormDataEntryValue | null): Promise<Stack> {
  return api("/stacks", "POST", { name });
}

export function listDeployments(id: string): Promise<Deployment[] | null> {
  return api(`${stackPath(id)}/deployments?limit=50`);
}

export function runStackAction(id: string, kind: string): Promise<Operation> {
  return api(`${stackPath(id)}/actions/${kind}`, "POST");
}

export function listFiles(id: string): Promise<FileEntry[] | null> {
  return api(`${stackPath(id)}/tree`);
}

export function getStackFile(id: string, path: string): Promise<FileContent> {
  return api(`${stackPath(id)}/files?path=${encodeURIComponent(path)}`);
}

export async function getDiff(id: string): Promise<string> {
  return (await api<{ diff: string }>(`${stackPath(id)}/diff`)).diff;
}

export function saveStackFile(
  id: string,
  path: string,
  content: string,
  hash: string,
): Promise<{ hash: string }> {
  return api(
    `${stackPath(id)}/files?path=${encodeURIComponent(path)}`,
    "PUT",
    { content },
    { "If-Match": `"${hash}"` },
  );
}

export function deleteStackFile(id: string, path: string): Promise<void> {
  return api(
    `${stackPath(id)}/files?path=${encodeURIComponent(path)}`,
    "DELETE",
  );
}

export function moveStackFile(
  id: string,
  from: string | undefined,
  to: string,
): Promise<void> {
  return api(`${stackPath(id)}/files/move`, "POST", { from, to });
}

export function createStackFile(
  id: string,
  path: string,
  directory: boolean,
): Promise<void> {
  return api(`${stackPath(id)}/files`, "POST", {
    path,
    content: "",
    directory,
  });
}

export function commitStack(id: string, message: string): Promise<void> {
  return api(`${stackPath(id)}/commit`, "POST", { message });
}

export async function listEnvironmentKeys(id: string): Promise<string[]> {
  const value = await api<{ keys: string[] | null }>(
    `${stackPath(id)}/environment`,
  );
  return value.keys || [];
}

export async function getEnvironmentValue(
  id: string,
  key: string,
): Promise<string> {
  const response = await api<{ value: string }>(
    `${stackPath(id)}/environment/${encodeURIComponent(key)}`,
  );
  return response.value;
}

export function setEnvironmentValue(
  id: string,
  key: string,
  value: FormDataEntryValue | string | null,
): Promise<void> {
  return api(`${stackPath(id)}/environment/${encodeURIComponent(key)}`, "PUT", {
    value,
  });
}

export function deleteEnvironmentValue(id: string, key: string): Promise<void> {
  return api(
    `${stackPath(id)}/environment/${encodeURIComponent(key)}`,
    "DELETE",
  );
}

export function renameStack(id: string, name: string): Promise<void> {
  return api(stackPath(id), "PATCH", { name });
}

export function deleteStack(id: string): Promise<void> {
  return api(stackPath(id), "DELETE");
}
