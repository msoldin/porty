import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/preact";
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import { EnvironmentRow } from "../features/stacks/EnvironmentRow";

const stack = {
  id: "s1",
  directoryName: "paperless",
  composeProjectName: "porty-paperless",
  createdAt: "",
  updatedAt: "",
};
let writes: { path: string; init?: RequestInit }[];
let stale = false;
let holdSave: (() => void) | undefined;
let delaySave = false;
let createFails = false;
let authenticated = true;
let registered = true;
let repositoryReady = true;
let repositoryRequired: boolean | undefined;
let hasManagedRemote = true;
let sockets = 0;
let repositoryStatus: Record<string, unknown>;
let environmentReads: string[];
let environmentValue = "saved-secret";
let environmentSecret = false;
let environmentKeys: string[];
let environmentReadResponse: (() => Promise<Response>) | undefined;
let environmentUpdateFails = false;
beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
  location.hash = "";
  writes = [];
  stale = false;
  delaySave = false;
  createFails = false;
  holdSave = undefined;
  authenticated = true;
  registered = true;
  repositoryReady = true;
  repositoryRequired = undefined;
  hasManagedRemote = true;
  sockets = 0;
  repositoryStatus = {
    configured: true,
    branch: "main",
    dirty: true,
    ahead: 1,
    behind: 0,
    paths: ["paperless/docker-compose.yml", "other/config.yml"],
  };
  environmentReads = [];
  environmentValue = "saved-secret";
  environmentSecret = false;
  environmentKeys = ["DATABASE_PASSWORD"];
  environmentReadResponse = undefined;
  environmentUpdateFails = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = String(url).replace("/api/v1", "");
      const method = init?.method || "GET";
      if (method !== "GET") writes.push({ path, init });
      if (path === "/session" && method === "GET" && !authenticated)
        return Response.json(
          { error: { code: "Unauthorized", message: "Sign in required" } },
          { status: 401 },
        );
      if (path === "/setup/status")
        return Response.json({ registered, csrfToken: "register-csrf" });
      if (path === "/stacks" && method === "POST" && createFails)
        return Response.json(
          {
            error: { code: "CreateFailed", message: "Could not create stack" },
          },
          { status: 500 },
        );
      if (
        (path === "/session" || path === "/setup/register") &&
        method === "POST"
      ) {
        authenticated = true;
        return Response.json({ username: "admin", csrfToken: "csrf" });
      }
      if (path === "/repository/remote" && method === "DELETE") {
        hasManagedRemote = false;
        return Response.json({
          state: "ready",
          required: false,
          pathState: "worktree",
          modes: [],
          branch: "main",
          author: { name: "Porty", email: "porty@localhost" },
          defaultAuthor: { name: "Porty", email: "porty@localhost" },
          ssh: {
            identityAvailable: false,
            knownHostsAvailable: false,
            usable: false,
          },
        });
      }
      if (path.includes("/files") && method === "PUT") {
        if (delaySave)
          await new Promise<void>((resolve) => {
            holdSave = resolve;
          });
        return new Response(
          JSON.stringify(
            stale
              ? {
                  error: {
                    code: "StaleWrite",
                    message: "File changed on disk",
                  },
                }
              : { hash: "new-hash" },
          ),
          { status: stale ? 412 : 200 },
        );
      }
      if (path.endsWith("/commit"))
        return Response.json({ sha: "abc123" }, { status: 201 });
      if (path.includes("/environment/") && method === "GET") {
        environmentReads.push(path);
        return environmentReadResponse
          ? environmentReadResponse()
          : Response.json({
              value: path.endsWith("/API_TOKEN")
                ? "api-secret"
                : environmentValue,
              secret: environmentSecret,
            });
      }
      if (path.includes("/environment/") && method === "PUT")
        return environmentUpdateFails
          ? Response.json(
              { error: { message: "Update failed" } },
              { status: 500 },
            )
          : new Response(null, { status: 204 });
      if (path.includes("/environment/") && method === "DELETE")
        return new Response(null, { status: 204 });
      const data: Record<string, unknown> = {
        "/session": { username: "admin", csrfToken: "csrf" },
        "/repository/setup/status": {
          state: repositoryReady ? "ready" : "registered",
          required: repositoryRequired ?? !repositoryReady,
          pathState: "empty",
          modes: [
            { mode: "init", available: true },
            { mode: "remote", available: true },
            {
              mode: "adopt",
              available: false,
              reason: "No worktree is mounted.",
            },
          ],
          author: { name: "", email: "" },
          defaultAuthor: { name: "Porty", email: "porty@localhost" },
          managedRemote: hasManagedRemote
            ? {
                name: "origin",
                url: "https://example.com/team/repo.git",
                authType: "none",
                managed: true,
              }
            : undefined,
          ssh: {
            identityAvailable: false,
            knownHostsAvailable: false,
            usable: false,
          },
        },
        "/stacks": [stack],
        "/repository/status": repositoryStatus,
        "/repository/history?limit=50": [
          {
            sha: "abcdef123",
            subject: "Initial stack",
            author: "admin",
            time: "2026-09-19",
          },
        ],
        "/operations?limit=50": [],
        "/stacks/s1/tree": [
          {
            path: "docker-compose.yml",
            size: 22,
            isDirectory: false,
            editable: true,
          },
          { path: "config.yml", size: 3, isDirectory: false, editable: true },
        ],
        "/stacks/s1/files?path=docker-compose.yml": {
          path: "docker-compose.yml",
          content: "services:\n  web: {}",
          hash: "original-hash",
          size: 19,
        },
        "/stacks/s1/files?path=config.yml": {
          path: "config.yml",
          content: "x: y",
          hash: "config-hash",
          size: 4,
        },
        "/stacks/s1/diff": {
          diff: "diff --git a/paperless/docker-compose.yml b/paperless/docker-compose.yml\n-old\n+new",
        },
        "/stacks/s1/environment": { keys: environmentKeys },
        "/stacks/s1/deployments?limit=50": [],
      };
      if (!(path in data))
        throw new Error(`Unexpected request: ${method} ${path}`);
      return Response.json(data[path]);
    }),
  );
  vi.stubGlobal(
    "WebSocket",
    class {
      static OPEN = 1;
      readyState = 1;
      constructor() {
        sockets += 1;
      }
      onopen: (() => void) | null = null;
      onmessage: ((event: { data: string }) => void) | null = null;
      onclose = null;
      onerror = null;
      send() {}
      close() {}
    },
  );
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function requestsFor(path: string) {
  return vi
    .mocked(fetch)
    .mock.calls.filter(([url]) => String(url).replace("/api/v1", "") === path);
}

async function openEditor() {
  render(<App />);
  fireEvent.click(await screen.findByRole("link", { name: "paperless" }));
  fireEvent.click(await screen.findByRole("tab", { name: "Editor" }));
  return screen.findByRole("textbox", { name: "File contents" });
}

async function edit(element: HTMLElement) {
  element.textContent = "services:\n  web:\n    image: nginx";
  fireEvent.input(element);
  await screen.findByText("Unsaved changes");
}

describe("Porty administration interface", () => {
  it("lets the administrator override the system appearance and restore it", async () => {
    render(<App />);
    fireEvent.click(await screen.findByRole("link", { name: "Settings" }));
    const theme = await screen.findByLabelText("Theme");
    expect(theme).toHaveValue("system");

    fireEvent.change(theme, { target: { value: "dark" } });
    expect(document.documentElement).toHaveAttribute("data-theme", "dark");
    expect(localStorage.getItem("porty-theme")).toBe("dark");

    fireEvent.change(theme, { target: { value: "system" } });
    expect(document.documentElement).not.toHaveAttribute("data-theme");
    expect(localStorage.getItem("porty-theme")).toBeNull();
  });
  it("disables remote-only actions for a ready local-only repository", async () => {
    hasManagedRemote = false;
    render(<App />);

    expect(
      await screen.findByRole("link", { name: "paperless" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Fetch" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Pull" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Push" })).toBeDisabled();
  });

  it("disables remote actions immediately after removing the managed remote", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<App />);
    expect(
      await screen.findByRole("link", { name: "paperless" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pull" })).toBeEnabled();

    fireEvent.click(screen.getByRole("link", { name: "Settings" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Remove remote" }),
    );

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Pull" })).toBeDisabled(),
    );
    expect(screen.getByRole("button", { name: "Fetch" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Push" })).toBeDisabled();
    expect(requestsFor("/repository/remote")).toHaveLength(1);
  });

  it("fails closed until the authoritative repository state is ready", async () => {
    repositoryReady = false;
    repositoryRequired = false;
    render(<App />);

    expect(
      await screen.findByRole("button", { name: "Create local repository" }),
    ).toBeInTheDocument();
    expect(requestsFor("/stacks")).toHaveLength(0);
    expect(sockets).toBe(0);
  });

  it.each([
    { registered: false, submit: "Create administrator" },
    { registered: true, submit: "Sign in" },
  ])(
    "shows repository setup after authentication when registered=$registered without starting workspace traffic",
    async ({ registered: isRegistered, submit }) => {
      authenticated = false;
      registered = isRegistered;
      repositoryReady = false;
      render(<App />);

      fireEvent.input(await screen.findByLabelText("Username"), {
        target: { value: "admin" },
      });
      fireEvent.input(screen.getByLabelText("Password"), {
        target: { value: "correct horse battery staple" },
      });
      fireEvent.click(screen.getByRole("button", { name: submit }));

      expect(
        await screen.findByRole("button", { name: "Create local repository" }),
      ).toBeInTheDocument();
      expect(requestsFor("/repository/setup/status")).toHaveLength(1);
      expect(requestsFor("/stacks")).toHaveLength(0);
      expect(sockets).toBe(0);
    },
  );
  it("keeps repository actions disabled when status is unconfigured", async () => {
    location.hash = "/repository";
    repositoryStatus = {
      configured: false,
      branch: "main",
      dirty: false,
      ahead: 0,
      behind: 0,
      paths: null,
    };

    render(<App />);

    expect(
      await screen.findByRole("heading", { name: "Repository" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Not configured")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pull" })).toBeDisabled();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
  it("keeps working-tree and remote states independent and does not invent runtime health", async () => {
    render(<App />);
    const link = await screen.findByRole("link", { name: "paperless" });
    const row = within(link.closest("tr")!);
    expect(row.getByText("Ahead 1")).toBeInTheDocument();
    expect(row.getByText("UNKNOWN")).toBeInTheDocument();
    fireEvent.change(screen.getByRole("combobox", { name: "Filter stacks" }), {
      target: { value: "modified" },
    });
    expect(screen.getByRole("link", { name: "paperless" })).toBeInTheDocument();
    expect(screen.queryByText("RUNNING")).not.toBeInTheDocument();
  });
  it("navigates from the dashboard to a stack and back", async () => {
    render(<App />);
    fireEvent.click(await screen.findByRole("link", { name: "paperless" }));
    expect(
      await screen.findByRole("heading", { name: "paperless" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("link", { name: "All stacks" }));
    expect(
      await screen.findByRole("heading", { name: "Stacks" }),
    ).toBeInTheDocument();
  });
  it("opens a stack from a direct hash link and handles malformed stack IDs", async () => {
    location.hash = "#/stacks/s1";
    const rendered = render(<App />);
    expect(
      await screen.findByRole("heading", { name: "paperless" }),
    ).toBeInTheDocument();
    rendered.unmount();
    location.hash = "#/stacks/%E0%A4%A";
    render(<App />);
    expect(
      await screen.findByRole("heading", { name: "Stack not found" }),
    ).toBeInTheDocument();
  });
  it("keeps an unsaved editor open when sign out is declined", async () => {
    await edit(await openEditor());
    vi.spyOn(window, "confirm").mockReturnValue(false);
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(
      screen.getByRole("textbox", { name: "File contents" }),
    ).toHaveTextContent("nginx");
    expect(
      writes.filter(
        (value) => value.path === "/session" && value.init?.method === "DELETE",
      ),
    ).toHaveLength(0);
  });
  it("blocks repository actions and browser unload while the editor is unsaved", async () => {
    await edit(await openEditor());
    fireEvent.click(screen.getByRole("button", { name: "Fetch" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Save or discard editor changes",
    );
    expect(
      writes.filter((value) => value.path === "/repository/actions/fetch"),
    ).toHaveLength(0);
    const unload = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(unload);
    expect(unload.defaultPrevented).toBe(true);
  });
  it("keeps the create form and shows the server error after a failed create", async () => {
    createFails = true;
    render(<App />);
    fireEvent.click(await screen.findByRole("button", { name: "New stack" }));
    fireEvent.input(screen.getByPlaceholderText("my-stack"), {
      target: { value: "broken" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create stack" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not create stack",
    );
    expect(screen.getByPlaceholderText("my-stack")).toHaveValue("broken");
  });
  it("preserves unsaved contents when navigation is declined", async () => {
    const editor = await openEditor();
    await edit(editor);
    vi.spyOn(window, "confirm").mockReturnValue(false);
    fireEvent.click(screen.getByRole("link", { name: "All stacks" }));
    expect(
      screen.getByRole("textbox", { name: "File contents" }),
    ).toHaveTextContent("nginx");
    expect(screen.getByText("Unsaved changes")).toBeInTheDocument();
  });
  it("sends the original ETag and preserves edits after a stale save", async () => {
    stale = true;
    await edit(await openEditor());
    fireEvent.click(screen.getByRole("button", { name: "Save file" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /changed on disk/i,
    );
    const save = writes.find((w) => w.init?.method === "PUT")!;
    expect(new Headers(save.init?.headers).get("If-Match")).toBe(
      '"original-hash"',
    );
    expect(new Headers(save.init?.headers).get("X-CSRF-Token")).toBe("csrf");
    expect(
      screen.getByRole("textbox", { name: "File contents" }),
    ).toHaveTextContent("nginx");
  });
  it("prevents editing while a save is pending so newer contents cannot be overwritten", async () => {
    delaySave = true;
    const editor = await openEditor();
    await edit(editor);
    fireEvent.click(screen.getByRole("button", { name: "Save file" }));
    await waitFor(() =>
      expect(editor).toHaveAttribute("contenteditable", "false"),
    );
    holdSave!();
    await screen.findByText("Saved");
    await waitFor(() =>
      expect(screen.getByLabelText("File contents")).toHaveAttribute(
        "contenteditable",
        "true",
      ),
    );
  });
  it("edits the saved value in its single field", async () => {
    render(<App />);
    fireEvent.click(await screen.findByRole("link", { name: "paperless" }));
    fireEvent.click(screen.getByRole("tab", { name: "Settings" }));
    expect(
      screen.getByText(/Saved values can be edited in place/),
    ).toBeInTheDocument();
    const input = await screen.findByLabelText("Value for DATABASE_PASSWORD");
    await waitFor(() => expect(input).toHaveValue("saved-secret"));
    expect(input).toHaveAttribute("type", "text");
    expect(
      screen.queryByLabelText("New value for DATABASE_PASSWORD"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Show DATABASE_PASSWORD" }),
    ).not.toBeInTheDocument();
    fireEvent.input(input, { target: { value: "replacement-secret" } });
    fireEvent.click(
      screen.getByRole("button", { name: "Update DATABASE_PASSWORD" }),
    );
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(input).toHaveValue("replacement-secret");
    expect(writes[0].path).toBe("/stacks/s1/environment/DATABASE_PASSWORD");
    expect(JSON.parse(String(writes[0].init?.body))).toEqual({
      value: "replacement-secret",
    });
  });
  it("shows ordinary saved values without eye buttons", async () => {
    environmentKeys = ["DATABASE_PASSWORD", "API_TOKEN"];
    render(<App />);
    fireEvent.click(await screen.findByRole("link", { name: "paperless" }));
    expect(environmentReads).toHaveLength(0);
    fireEvent.click(screen.getByRole("tab", { name: "Settings" }));
    const current = await screen.findByLabelText("Value for DATABASE_PASSWORD");
    await waitFor(() => expect(current).toHaveValue("saved-secret"));
    await waitFor(() =>
      expect(screen.getByLabelText("Value for API_TOKEN")).toHaveValue(
        "api-secret",
      ),
    );
    expect(environmentReads).toHaveLength(2);
    expect(current).toHaveAttribute("type", "text");
    expect(current).not.toHaveAttribute("readonly");
    expect(writes).toHaveLength(0);
    expect(
      screen.queryByRole("button", { name: /Show|Hide/ }),
    ).not.toBeInTheDocument();
  });
  it("masks only values marked secret and reveals them with the eye button", async () => {
    environmentSecret = true;
    render(<App />);
    fireEvent.click(await screen.findByRole("link", { name: "paperless" }));
    fireEvent.click(screen.getByRole("tab", { name: "Settings" }));
    const input = await screen.findByLabelText("Value for DATABASE_PASSWORD");
    await waitFor(() => expect(input).toHaveValue("saved-secret"));
    expect(input).toHaveAttribute("type", "password");
    expect(
      screen.queryByRole("checkbox", {
        name: "Secret value for DATABASE_PASSWORD",
      }),
    ).not.toBeInTheDocument();
    expect(
      screen.getAllByLabelText("Value for DATABASE_PASSWORD"),
    ).toHaveLength(1);
    const show = screen.getByRole("button", { name: "Show DATABASE_PASSWORD" });
    expect(show.querySelector("svg")).not.toBeNull();
    fireEvent.click(show);
    expect(input).toHaveAttribute("type", "text");
    fireEvent.click(
      screen.getByRole("button", { name: "Hide DATABASE_PASSWORD" }),
    );
    expect(input).toHaveAttribute("type", "password");
    expect(input).toHaveValue("saved-secret");
    expect(environmentReads).toHaveLength(1);
  });
  it("keeps the secret setting fixed when editing an existing value", async () => {
    render(<App />);
    fireEvent.click(await screen.findByRole("link", { name: "paperless" }));
    fireEvent.click(screen.getByRole("tab", { name: "Settings" }));
    const input = await screen.findByLabelText("Value for DATABASE_PASSWORD");
    await waitFor(() => expect(input).toHaveValue("saved-secret"));
    expect(
      screen.queryByRole("checkbox", {
        name: "Secret value for DATABASE_PASSWORD",
      }),
    ).not.toBeInTheDocument();
    expect(input).toHaveAttribute("type", "text");
    expect(
      screen.getAllByLabelText("Value for DATABASE_PASSWORD"),
    ).toHaveLength(1);
    fireEvent.input(input, { target: { value: "changed" } });
    fireEvent.click(
      screen.getByRole("button", { name: "Update DATABASE_PASSWORD" }),
    );
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(JSON.parse(String(writes[0].init?.body))).toEqual({
      value: "changed",
    });
  });
  it("marks a new environment value secret when added", async () => {
    render(<App />);
    fireEvent.click(await screen.findByRole("link", { name: "paperless" }));
    fireEvent.click(screen.getByRole("tab", { name: "Settings" }));
    fireEvent.input(screen.getByLabelText("New key"), {
      target: { value: "API_TOKEN" },
    });
    fireEvent.input(screen.getByLabelText("New value"), {
      target: { value: "new-secret" },
    });
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Secret value for new key" }),
    );
    expect(screen.getByLabelText("New value")).toHaveAttribute(
      "type",
      "password",
    );
    fireEvent.click(screen.getByRole("button", { name: "Add value" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(JSON.parse(String(writes[0].init?.body))).toEqual({
      value: "new-secret",
      secret: true,
    });
  });
  it("does not apply a pending value after leaving Settings", async () => {
    let resolveRead: ((response: Response) => void) | undefined;
    environmentReadResponse = () =>
      new Promise<Response>((resolve) => {
        resolveRead = resolve;
      });
    render(<App />);
    fireEvent.click(await screen.findByRole("link", { name: "paperless" }));
    fireEvent.click(screen.getByRole("tab", { name: "Settings" }));
    await waitFor(() => expect(resolveRead).toBeDefined());
    fireEvent.click(screen.getByRole("tab", { name: "Overview" }));
    resolveRead!(Response.json({ value: "late-secret", secret: false }));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(document.body).not.toHaveTextContent("late-secret");
  });
  it("ignores the old Stack value when its row changes Stack", async () => {
    let resolveRead: ((response: Response) => void) | undefined;
    environmentReadResponse = () =>
      new Promise<Response>((resolve) => {
        resolveRead = resolve;
      });
    const view = render(
      <EnvironmentRow
        name="DATABASE_PASSWORD"
        stackId="s1"
        reload={() => {}}
      />,
    );
    const current = screen.getByLabelText("Value for DATABASE_PASSWORD");
    await waitFor(() => expect(resolveRead).toBeDefined());
    const resolveOld = resolveRead!;
    view.rerender(
      <EnvironmentRow
        name="DATABASE_PASSWORD"
        stackId="s2"
        reload={() => {}}
      />,
    );
    await waitFor(() => expect(environmentReads).toHaveLength(2));
    const resolveNew = resolveRead!;
    resolveOld(Response.json({ value: "old-stack-secret", secret: true }));
    resolveNew(Response.json({ value: "new-stack-secret", secret: false }));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(current).toHaveValue("new-stack-secret");
    expect(document.body).not.toHaveTextContent("old-stack-secret");
  });
  it("keeps the edited value and error when an update fails", async () => {
    environmentUpdateFails = true;
    environmentSecret = true;
    render(<App />);
    fireEvent.click(await screen.findByRole("link", { name: "paperless" }));
    fireEvent.click(screen.getByRole("tab", { name: "Settings" }));
    const current = await screen.findByLabelText("Value for DATABASE_PASSWORD");
    await waitFor(() => expect(current).toHaveValue("saved-secret"));
    fireEvent.input(current, { target: { value: "replacement-secret" } });
    fireEvent.click(
      screen.getByRole("button", { name: "Update DATABASE_PASSWORD" }),
    );
    expect(await screen.findByText("Update failed")).toBeInTheDocument();
    expect(current).toHaveValue("replacement-secret");
    expect(current).toHaveAttribute("type", "password");
  });
  it("commits only the current stack through the stack-scoped endpoint", async () => {
    await openEditor();
    fireEvent.input(screen.getByLabelText("Commit message"), {
      target: { value: "Update paperless" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Commit stack" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0].path).toBe("/stacks/s1/commit");
    expect(JSON.parse(String(writes[0].init?.body))).toEqual({
      message: "Update paperless",
    });
  });
});
