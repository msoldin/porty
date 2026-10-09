import { api } from "../../lib/http";
import type { Operation } from "../operations/types";
import { stackPath } from "./api";
export type DeploymentReview = {
  stackId: string;
  sourceRevision: string;
  uncommittedChanges: boolean;
};
export function getDeploymentReview(id: string): Promise<DeploymentReview> {
  return api(`${stackPath(id)}/deployment-review`);
}
export function deployReviewedStack(
  id: string,
  revision: string,
): Promise<Operation> {
  return api(`${stackPath(id)}/actions/deploy`, "POST", undefined, {
    "If-Match": `"${revision}"`,
  });
}
