import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/preact";
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";

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
beforeEach(() => {
  location.hash = "";
  writes = [];
  stale = false;
  delaySave = false;
  holdSave = undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = String(url).replace("/api/v1", "");
      const method = init?.method || "GET";
      if (method !== "GET") writes.push({ path, init });
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
      if (path.includes("/environment/") && method === "PUT")
        return new Response(null, { status: 204 });
      const data: Record<string, unknown> = {
        "/session": { username: "admin", csrfToken: "csrf" },
        "/stacks": [stack],
        "/repository/status": {
          branch: "main",
          dirty: true,
          ahead: 1,
          behind: 0,
          paths: ["paperless/docker-compose.yml", "other/config.yml"],
        },
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
        "/stacks/s1/environment": { keys: ["DATABASE_PASSWORD"] },
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
  it("keeps working-tree and remote states independent and does not invent runtime health", async () => {
    render(<App />);
    const link = await screen.findByRole("link", { name: "paperless" });
    const row = within(link.closest("tr")!);
    expect(row.getByText("Modified")).toBeInTheDocument();
    expect(row.getByText("Ahead 1")).toBeInTheDocument();
    expect(row.getByText("Unknown")).toBeInTheDocument();
    expect(screen.queryByText("Running")).not.toBeInTheDocument();
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
    await waitFor(() => expect(screen.getByLabelText("File contents")).toHaveAttribute("contenteditable", "true"));
  });
  it("shows environment keys with blank password inputs and clears replacements after save", async () => {
    render(<App />);
    fireEvent.click(await screen.findByRole("link", { name: "paperless" }));
    fireEvent.click(screen.getByRole("tab", { name: "Settings" }));
    const input = await screen.findByLabelText(
      "New value for DATABASE_PASSWORD",
    );
    expect(input).toHaveAttribute("type", "password");
    expect(input).toHaveValue("");
    fireEvent.input(input, { target: { value: "replacement-secret" } });
    fireEvent.click(
      screen.getByRole("button", { name: "Update DATABASE_PASSWORD" }),
    );
    await waitFor(() => expect(input).toHaveValue(""));
    expect(writes[0].path).toBe("/stacks/s1/environment/DATABASE_PASSWORD");
    expect(document.body).not.toHaveTextContent("replacement-secret");
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
