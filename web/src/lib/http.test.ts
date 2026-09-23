import { afterEach, expect, it, vi } from "vitest";
import { api, APIError, setCSRF } from "./http";

afterEach(() => {
  vi.unstubAllGlobals();
  document.cookie = "porty_csrf=; Max-Age=0; Path=/";
  setCSRF("");
});

it("refreshes a reloaded session using the CSRF cookie", async () => {
  document.cookie = "porty_csrf=cookie-csrf; Path=/";
  let sessions = 0;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url === "/api/v1/session/refresh") {
      expect(init?.headers).toEqual({ "X-CSRF-Token": "cookie-csrf" });
      return Response.json({ username: "admin", csrfToken: "next-csrf" });
    }
    sessions++;
    return sessions === 1
      ? Response.json(
          { error: { code: "AuthenticationFailed" } },
          { status: 401 },
        )
      : Response.json({ username: "admin", csrfToken: "next-csrf" });
  });
  vi.stubGlobal("fetch", fetcher);
  await expect(api("/session")).resolves.toEqual({
    username: "admin",
    csrfToken: "next-csrf",
  });
  expect(sessions).toBe(2);
  expect(fetcher).toHaveBeenCalledTimes(3);
});

it("shares one refresh across concurrent 401 responses and retries once", async () => {
  document.cookie = "porty_csrf=cookie-csrf; Path=/";
  let refreshes = 0;
  let resolves: (() => void) | undefined;
  const gate = new Promise<void>((resolve) => {
    resolves = resolve;
  });
  const fetcher = vi.fn(async (url: string) => {
    if (url === "/api/v1/session/refresh") {
      refreshes++;
      await gate;
      return Response.json({ username: "admin", csrfToken: "next-csrf" });
    }
    const count = fetcher.mock.calls.filter(([path]) => path === url).length;
    return count === 1
      ? Response.json(
          { error: { code: "AuthenticationFailed" } },
          { status: 401 },
        )
      : Response.json({ ok: true });
  });
  vi.stubGlobal("fetch", fetcher);
  const pending = Promise.all([api("/stacks"), api("/audit")]);
  await vi.waitFor(() => expect(refreshes).toBe(1));
  resolves?.();
  await expect(pending).resolves.toEqual([{ ok: true }, { ok: true }]);
  expect(refreshes).toBe(1);
});

it("uses a completed refresh for a late 401 response", async () => {
  document.cookie = "porty_csrf=cookie-csrf; Path=/";
  let releaseLate: (() => void) | undefined;
  const late = new Promise<void>((resolve) => {
    releaseLate = resolve;
  });
  let refreshes = 0;
  let firstCalls = 0;
  let lateCalls = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url === "/api/v1/session/refresh") {
        refreshes++;
        return Response.json({ username: "admin", csrfToken: "new-csrf" });
      }
      if (url === "/api/v1/audit") {
        lateCalls++;
        if (lateCalls === 1) {
          await late;
          return Response.json({}, { status: 401 });
        }
        return Response.json({ ok: true });
      }
      firstCalls++;
      return firstCalls === 1
        ? Response.json({}, { status: 401 })
        : Response.json({ ok: true });
    }),
  );
  const delayed = api("/audit");
  await expect(api("/stacks")).resolves.toEqual({ ok: true });
  releaseLate?.();
  await expect(delayed).resolves.toEqual({ ok: true });
  expect(refreshes).toBe(1);
});

it("does not retry when refresh has expired", async () => {
  document.cookie = "porty_csrf=cookie-csrf; Path=/";
  const fetcher = vi.fn(async (url: string) =>
    Response.json({ error: { code: "AuthenticationFailed" } }, { status: 401 }),
  );
  vi.stubGlobal("fetch", fetcher);
  await expect(api("/stacks")).rejects.toBeInstanceOf(APIError);
  expect(fetcher).toHaveBeenCalledTimes(2);
});

it("does not refresh or retry a rejected sign-in or setup request", async () => {
  document.cookie = "porty_csrf=cookie-csrf; Path=/";
  const fetcher = vi.fn(async () =>
    Response.json({ error: { code: "AuthenticationFailed" } }, { status: 401 }),
  );
  vi.stubGlobal("fetch", fetcher);
  await expect(
    api("/session", "POST", { username: "admin", password: "wrong" }),
  ).rejects.toBeInstanceOf(APIError);
  await expect(api("/setup/status")).rejects.toBeInstanceOf(APIError);
  expect(fetcher.mock.calls).toHaveLength(2);
});
