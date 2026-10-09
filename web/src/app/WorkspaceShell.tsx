import type { ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { Icon } from "../components/Icon";
import portySidebar from "../assets/porty-sidebar.png";

const nav = [
  { name: "Dashboard", path: "/" },
  { name: "Stacks", icon: "Stacks", path: "/stacks" },
  { name: "Repository", path: "/repository" },
  { name: "Operations", path: "/operations", group: "Monitor" },
  { name: "Alerts", path: "/alerts" },
  { name: "Audit log", icon: "Audit", path: "/audit" },
  { name: "Settings", path: "/settings", group: "Manage" },
];

export function WorkspaceShell({
  unacknowledgedAlerts,
  username,
  route,
  stackSelected,
  operationOpen,
  connection,
  navigate,
  onSignOut,
  drawer,
  children,
}: {
  unacknowledgedAlerts: number;
  username: string;
  route: string;
  stackSelected: boolean;
  operationOpen: boolean;
  connection: string;
  navigate: (path: string) => void;
  onSignOut: () => void;
  drawer?: ComponentChildren;
  children: ComponentChildren;
}) {
  const [navigationOpen, setNavigationOpen] = useState(false);
  const navigationToggle = useRef<HTMLButtonElement>(null);
  useEffect(() => setNavigationOpen(false), [route]);
  const connectionLabel =
    connection === "Disabled"
      ? "Stack updates start after setup"
      : connection === "Connected"
        ? "Live updates connected"
        : connection === "Reconnecting"
          ? "Reconnecting · data may be stale"
          : "Updates unavailable · data may be stale";
  function selectPage(path: string) {
    navigate(path);
    if (navigationOpen) {
      setNavigationOpen(false);
      navigationToggle.current?.focus();
    }
  }
  return (
    <div class={`app-shell ${operationOpen ? "with-drawer" : ""}`}>
      <aside
        class={`workspace-sidebar ${navigationOpen ? "navigation-open" : ""}`}
      >
        <div class="sidebar-heading">
          <a
            class="brand"
            href="#/"
            onClick={(event) => {
              event.preventDefault();
              selectPage("/");
            }}
          >
            <img src={portySidebar} alt="" width="48" height="48" />
            <span>Porty</span>
          </a>
          <button
            class="navigation-toggle"
            ref={navigationToggle}
            type="button"
            aria-label="Navigation"
            aria-expanded={navigationOpen}
            aria-controls="workspace-navigation"
            onClick={() => setNavigationOpen(!navigationOpen)}
          >
            Navigation <Icon name="Chevron" />
          </button>
        </div>
        <div
          class="sidebar-content"
          id="workspace-navigation"
          onKeyDown={(event) => {
            if (event.key === "Escape" && navigationOpen) {
              setNavigationOpen(false);
              navigationToggle.current?.focus();
            }
          }}
        >
          <nav aria-label="Main navigation">
            {nav.map((item) => (
              <div key={item.path}>
                {item.group && <p class="nav-group">{item.group}</p>}
                <a
                  href={`#${item.path}`}
                  aria-current={
                    route === item.path ||
                    (item.path === "/stacks" && stackSelected)
                      ? "page"
                      : undefined
                  }
                  class={
                    route === item.path ||
                    (item.path === "/stacks" && stackSelected)
                      ? "active"
                      : ""
                  }
                  onClick={(event) => {
                    event.preventDefault();
                    selectPage(item.path);
                  }}
                >
                  <Icon name={item.icon || item.name} />
                  {item.name}
                  {item.path === "/alerts" && unacknowledgedAlerts > 0 && (
                    <span
                      class="alert-count"
                      aria-label={`${unacknowledgedAlerts} unacknowledged alerts`}
                    >
                      {unacknowledgedAlerts}
                    </span>
                  )}
                </a>
              </div>
            ))}
          </nav>
          <div class="server-info">
            <span class="muted">Server</span>
            <strong>{location.hostname}</strong>
            <span
              class={`connection ${connection === "Connected" ? "is-connected" : ""}`}
            >
              {connectionLabel}
            </span>
            <hr />
            <span>{username}</span>
            <button class="text-button" onClick={onSignOut}>
              Sign out
            </button>
          </div>
        </div>
      </aside>
      <div class={`workspace ${stackSelected ? "stack-workspace" : ""}`}>
        {children}
      </div>
      {drawer}
    </div>
  );
}
