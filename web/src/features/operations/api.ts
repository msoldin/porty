import { api } from "../../lib/http";
import type { Operation } from "./types";

export function listOperations(): Promise<Operation[] | null> {
  return api("/operations?limit=50");
}
