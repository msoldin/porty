import { api, setCSRF } from "../../lib/http";
import type { Session } from "./types";

export function getSession(): Promise<Session> {
  return api<Session>("/session");
}

export function getSetupStatus(): Promise<{
  registered: boolean;
  csrfToken: string;
}> {
  return api("/setup/status");
}

export async function signIn(
  registered: boolean,
  username: FormDataEntryValue | null,
  password: FormDataEntryValue | null,
): Promise<Session> {
  const session = await api<Session>(
    registered ? "/session" : "/setup/register",
    "POST",
    { username, password },
  );
  setCSRF(session.csrfToken);
  return session;
}

export function signOut(): Promise<void> {
  return api("/session", "DELETE");
}

export function changePassword(
  current: FormDataEntryValue | null,
  next: FormDataEntryValue | null,
): Promise<void> {
  return api("/session/password", "PUT", {
    CurrentPassword: current,
    NewPassword: next,
  });
}
