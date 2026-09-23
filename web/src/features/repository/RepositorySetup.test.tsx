import { fireEvent, render, screen, waitFor } from "@testing-library/preact";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RepositorySetup } from "./RepositorySetup";
import { RemoteInspection, RepositorySetupStatus } from "./types";

const emptyStatus: RepositorySetupStatus = {
  state: "registered",
  required: true,
  pathState: "empty",
  modes: [
    { mode: "init", available: true },
    { mode: "remote", available: true },
    { mode: "adopt", available: false, reason: "No worktree is mounted." },
  ],
  author: { name: "", email: "" },
  defaultAuthor: { name: "Porty", email: "porty@localhost" },
  ssh: {
    identityAvailable: true,
    knownHostsAvailable: true,
    usable: true,
  },
};

let requests: Array<{ path: string; method: string; body?: unknown }>;
let inspection: RemoteInspection;
let setupFailure = false;

beforeEach(() => {
  requests = [];
  setupFailure = false;
  inspection = {
    remoteUrl: "https://example.com/team/repo.git",
    defaultBranch: "release",
    branches: ["main", "release"],
    empty: false,
    suggestedBranch: "release",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = String(url).replace("/api/v1", "");
      const method = init?.method || "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      requests.push({ path, method, body });
      if (path === "/repository/setup/inspect-remote")
        return Response.json(inspection);
      if (path === "/repository/setup") {
        if (setupFailure)
          return Response.json(
            {
              error: {
                code: "RemoteUnavailable",
                message: "Remote unavailable",
              },
            },
            { status: 502 },
          );
        return Response.json({
          ...emptyStatus,
          state: "ready",
          required: false,
        });
      }
      throw new Error(`Unexpected request: ${method} ${path}`);
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function choose(name: string) {
  fireEvent.click(screen.getByRole("button", { name }));
}

describe("repository first-start setup", () => {
  it("shows the three fixed-root choices without path or SSH upload controls", () => {
    render(<RepositorySetup status={emptyStatus} onReady={vi.fn()} />);

    expect(
      screen.getByRole("button", { name: "Create local repository" }),
    ).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Use remote repository" }),
    ).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Use mounted repository" }),
    ).toBeDisabled();
    expect(screen.getByText("No worktree is mounted.")).toBeInTheDocument();
    expect(screen.queryByLabelText(/path/i)).not.toBeInTheDocument();
    expect(document.querySelector('input[type="file"]')).toBeNull();
  });

  it("prefills editable author defaults and creates a local-only repository", async () => {
    const onReady = vi.fn();
    render(<RepositorySetup status={emptyStatus} onReady={onReady} />);
    choose("Create local repository");

    expect(screen.getByLabelText("Git author name")).toHaveValue("Porty");
    expect(screen.getByLabelText("Git author email")).toHaveValue(
      "porty@localhost",
    );
    fireEvent.input(screen.getByLabelText("Initial branch"), {
      target: { value: "stacks" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create repository" }));

    await waitFor(() => expect(onReady).toHaveBeenCalledOnce());
    const request = requests.find(
      (value) => value.path === "/repository/setup",
    );
    expect(request?.body).toEqual({
      mode: "init",
      branch: "stacks",
      author: { name: "Porty", email: "porty@localhost" },
      manageExistingRemote: false,
    });
    expect(request?.body).not.toHaveProperty("remote");
  });

  it("requires inspection and selects the advertised symbolic HEAD branch", async () => {
    render(<RepositorySetup status={emptyStatus} onReady={vi.fn()} />);
    choose("Use remote repository");

    expect(
      screen.getByRole("button", { name: "Import repository" }),
    ).toBeDisabled();
    fireEvent.input(screen.getByLabelText("Remote URL"), {
      target: { value: "https://example.com/team/repo.git" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Inspect remote" }));

    expect(await screen.findByLabelText("Branch")).toHaveValue("release");
    expect(screen.getByRole("option", { name: "main" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "release" })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Import repository" }),
    ).toBeEnabled();
  });

  it("uses an editable main branch for an empty remote", async () => {
    inspection = {
      remoteUrl: "https://example.com/team/empty.git",
      branches: [],
      empty: true,
      suggestedBranch: "main",
    };
    render(<RepositorySetup status={emptyStatus} onReady={vi.fn()} />);
    choose("Use remote repository");
    fireEvent.input(screen.getByLabelText("Remote URL"), {
      target: { value: "https://example.com/team/empty.git" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Inspect remote" }));

    const branch = await screen.findByLabelText("Branch");
    expect(branch).toHaveValue("main");
    expect(branch).toHaveAttribute("type", "text");
    fireEvent.input(branch, { target: { value: "trunk" } });
    expect(branch).toHaveValue("trunk");
  });

  it("offers explicit authentication choices and reports fixed SSH availability", () => {
    render(<RepositorySetup status={emptyStatus} onReady={vi.fn()} />);
    choose("Use remote repository");

    expect(
      screen.getByRole("radio", { name: "No authentication" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("radio", { name: "HTTPS username and secret" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("radio", { name: "Mounted SSH files" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "Mounted SSH files" }));
    expect(
      screen.getByText(/SSH identity and known hosts are ready/i),
    ).toBeInTheDocument();
    expect(document.querySelector('input[type="file"]')).toBeNull();
  });

  it("preserves non-secret fields and clears the HTTPS secret after failure", async () => {
    setupFailure = true;
    render(<RepositorySetup status={emptyStatus} onReady={vi.fn()} />);
    choose("Use remote repository");
    fireEvent.click(
      screen.getByRole("radio", { name: "HTTPS username and secret" }),
    );
    fireEvent.input(screen.getByLabelText("Remote URL"), {
      target: { value: "https://example.com/team/repo.git" },
    });
    fireEvent.input(screen.getByLabelText("HTTPS username"), {
      target: { value: "octocat" },
    });
    fireEvent.input(screen.getByLabelText("HTTPS secret"), {
      target: { value: "top-secret" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Inspect remote" }));
    await screen.findByLabelText("Branch");
    fireEvent.click(screen.getByRole("button", { name: "Import repository" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Remote unavailable",
    );
    expect(screen.getByLabelText("Remote URL")).toHaveValue(
      "https://example.com/team/repo.git",
    );
    expect(screen.getByLabelText("HTTPS username")).toHaveValue("octocat");
    expect(screen.getByLabelText("HTTPS secret")).toHaveValue("");
    expect(
      screen.getByRole("button", { name: "Import repository" }),
    ).toBeDisabled();
    expect(document.body).not.toHaveTextContent("top-secret");
  });

  it("adopts detected repository facts with an explicit origin choice", async () => {
    const status: RepositorySetupStatus = {
      ...emptyStatus,
      pathState: "worktree",
      modes: [
        { mode: "init", available: false, reason: "A worktree exists." },
        { mode: "remote", available: false, reason: "A worktree exists." },
        { mode: "adopt", available: true },
      ],
      branch: "develop",
      author: { name: "Ada", email: "ada@example.com" },
      existingRemote: {
        name: "origin",
        url: "https://example.com/team/repo.git",
        authType: "none",
        managed: false,
      },
    };
    render(<RepositorySetup status={status} onReady={vi.fn()} />);
    choose("Use mounted repository");

    expect(screen.getByText("develop")).toBeInTheDocument();
    expect(screen.getByDisplayValue("Ada")).toBeInTheDocument();
    expect(
      screen.getByRole("radio", { name: "Keep local-only" }),
    ).toBeChecked();
    fireEvent.click(
      screen.getByRole("radio", { name: "Manage origin with Porty" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Use mounted repository" }),
    );

    await waitFor(() =>
      expect(
        requests.some((request) => request.path === "/repository/setup"),
      ).toBe(true),
    );
    expect(requests.at(-1)?.body).toEqual({
      mode: "adopt",
      branch: "develop",
      author: { name: "Ada", email: "ada@example.com" },
      manageExistingRemote: true,
    });
  });
});
