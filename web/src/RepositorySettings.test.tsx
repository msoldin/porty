import { fireEvent, render, screen, waitFor } from "@testing-library/preact";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RepositorySettings } from "./RepositorySettings";
import type { RemoteInspection, RepositorySetupStatus } from "./api";

const localStatus: RepositorySetupStatus = {
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
};

const managedStatus: RepositorySetupStatus = {
  ...localStatus,
  managedRemote: {
    name: "origin",
    url: "https://example.com/team/repo.git",
    authType: "https",
    managed: true,
  },
};

let requests: Array<{ path: string; method: string; body?: unknown }>;
let inspection: RemoteInspection;
let configureFailure = false;

beforeEach(() => {
  requests = [];
  configureFailure = false;
  inspection = {
    remoteUrl: "https://example.com/team/new.git",
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
      if (path === "/repository/remote" && method === "PUT") {
        if (configureFailure)
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
          ...managedStatus,
          branch: (body as { branch: string }).branch,
          managedRemote: {
            ...managedStatus.managedRemote,
            url: inspection.remoteUrl,
          },
        });
      }
      if (path === "/repository/remote" && method === "DELETE")
        return Response.json(localStatus);
      throw new Error(`Unexpected request: ${method} ${path}`);
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("repository remote settings", () => {
  it("offers Add remote for a local-only repository", () => {
    render(<RepositorySettings status={localStatus} onChange={vi.fn()} />);

    expect(
      screen.getByRole("button", { name: "Add remote" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/local commits and stack files remain usable/i),
    ).toBeInTheDocument();
  });

  it("offers managed remote replacement, authentication update, and removal without a stored secret", () => {
    render(<RepositorySettings status={managedStatus} onChange={vi.fn()} />);

    expect(
      screen.getByText("https://example.com/team/repo.git"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Replace remote" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Update authentication" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Remove remote" }),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Update authentication" }),
    );
    fireEvent.click(
      screen.getByRole("radio", { name: "HTTPS username and secret" }),
    );
    expect(screen.getByLabelText("HTTPS secret")).toHaveValue("");
  });

  it("requires explicit confirmation before replacing an unmanaged origin", () => {
    const status: RepositorySetupStatus = {
      ...localStatus,
      existingRemote: {
        name: "origin",
        url: "https://example.com/legacy/repo.git",
        authType: "none",
        managed: false,
      },
    };
    render(<RepositorySettings status={status} onChange={vi.fn()} />);
    fireEvent.click(
      screen.getByRole("button", { name: "Replace existing origin" }),
    );
    fireEvent.input(screen.getByLabelText("Remote URL"), {
      target: { value: "https://example.com/team/new.git" },
    });

    const confirmation = screen.getByRole("checkbox", {
      name: /replace the existing unmanaged origin/i,
    });
    expect(confirmation).not.toBeChecked();
    expect(
      screen.getByRole("button", { name: "Inspect remote" }),
    ).toBeDisabled();
    fireEvent.click(confirmation);
    expect(
      screen.getByRole("button", { name: "Inspect remote" }),
    ).toBeEnabled();
  });

  it("requires inspection and branch selection before adding a remote", async () => {
    const onChange = vi.fn();
    render(<RepositorySettings status={localStatus} onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Add remote" }));

    expect(screen.getByRole("button", { name: "Add remote" })).toBeDisabled();
    fireEvent.input(screen.getByLabelText("Remote URL"), {
      target: { value: "https://example.com/team/new.git" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Inspect remote" }));

    expect(await screen.findByLabelText("Branch")).toHaveValue("release");
    fireEvent.change(screen.getByLabelText("Branch"), {
      target: { value: "main" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add remote" }));
    await waitFor(() => expect(onChange).toHaveBeenCalledOnce());
    expect(requests.at(-1)).toEqual({
      path: "/repository/remote",
      method: "PUT",
      body: {
        remote: {
          url: "https://example.com/team/new.git",
          authentication: { type: "none" },
        },
        branch: "main",
        replaceExisting: false,
      },
    });
  });

  it("clears a newly entered secret after a failed authentication update", async () => {
    configureFailure = true;
    render(<RepositorySettings status={managedStatus} onChange={vi.fn()} />);
    fireEvent.click(
      screen.getByRole("button", { name: "Update authentication" }),
    );
    fireEvent.click(
      screen.getByRole("radio", { name: "HTTPS username and secret" }),
    );
    fireEvent.input(screen.getByLabelText("HTTPS username"), {
      target: { value: "octocat" },
    });
    fireEvent.input(screen.getByLabelText("HTTPS secret"), {
      target: { value: "replacement-secret" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Update authentication" }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Remote unavailable",
    );
    expect(screen.getByLabelText("HTTPS username")).toHaveValue("octocat");
    expect(screen.getByLabelText("HTTPS secret")).toHaveValue("");
    expect(document.body).not.toHaveTextContent("replacement-secret");
  });

  it("removes only the managed remote while keeping the repository ready", async () => {
    const onChange = vi.fn();
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<RepositorySettings status={managedStatus} onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Remove remote" }));

    await waitFor(() => expect(onChange).toHaveBeenCalledWith(localStatus));
    expect(requests.at(-1)).toMatchObject({
      path: "/repository/remote",
      method: "DELETE",
    });
    expect(localStatus.state).toBe("ready");
    expect(localStatus.managedRemote).toBeUndefined();
  });
});
