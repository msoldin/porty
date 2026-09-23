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
function cookieCSRF(): string {
  const cookie = document.cookie
    .split("; ")
    .find((part) => part.startsWith("porty_csrf="));
  return cookie ? decodeURIComponent(cookie.slice("porty_csrf=".length)) : "";
}
let refreshInFlight: Promise<void> | null = null;
let authGeneration = 0;
export function refreshSession(): Promise<void> {
  if (!refreshInFlight) {
    refreshInFlight = (async () => {
      const csrf = cookieCSRF();
      if (!csrf)
        throw new APIError(
          401,
          "AuthenticationFailed",
          "Authentication required",
        );
      const response = await fetch("/api/v1/session/refresh", {
        method: "POST",
        credentials: "same-origin",
        headers: { "X-CSRF-Token": csrf },
      });
      if (!response.ok)
        throw new APIError(
          response.status,
          "AuthenticationFailed",
          "Authentication required",
        );
      const session = (await response.json()) as { csrfToken: string };
      setCSRF(session.csrfToken);
      authGeneration++;
    })().finally(() => {
      refreshInFlight = null;
    });
  }
  return refreshInFlight;
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
  headers: Record<string, string> = {},
): Promise<T> {
  async function request(): Promise<Response> {
    return fetch(`/api/v1${path}`, {
      method,
      credentials: "same-origin",
      headers: {
        ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
        ...(method !== "GET"
          ? { "X-CSRF-Token": csrfToken || cookieCSRF() }
          : {}),
        ...headers,
      },
      ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    });
  }
  const sentGeneration = authGeneration;
  let response = await request();
  if (
    response.status === 401 &&
    path !== "/session/refresh" &&
    !(path === "/session" && method === "POST") &&
    !path.startsWith("/setup/")
  ) {
    if (authGeneration === sentGeneration) await refreshSession();
    response = await request();
  }
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

export function message(error: unknown): string {
  return error instanceof Error ? error.message : "Request failed";
}
