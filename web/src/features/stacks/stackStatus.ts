import type { Repository } from "../repository/types";
import type { Stack } from "./types";

export function remoteState(repo?: Repository | null) {
  if (!repo) return "Unknown";
  if (repo.ahead && repo.behind)
    return `Diverged +${repo.ahead} / −${repo.behind}`;
  return repo.ahead
    ? `Ahead ${repo.ahead}`
    : repo.behind
      ? `Behind ${repo.behind}`
      : "Current";
}
export function isModified(stack: Stack, repo?: Repository | null) {
  return (repo?.paths || []).some((path) =>
    path.startsWith(`${stack.directoryName}/`),
  );
}
