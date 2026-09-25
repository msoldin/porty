import type { ComponentChildren } from "preact";
import { Icon } from "../components/Icon";

const nav = [
  { name: "Overview", icon: "Stacks", path: "/" },
  { name: "Repository", path: "/repository" },
  { name: "Operations", path: "/operations" },
  { name: "Audit", path: "/audit" },
  { name: "Settings", path: "/settings" },
];

export function WorkspaceShell({
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
  return (
    <div class={`app-shell ${operationOpen ? "with-drawer" : ""}`}>
      <aside class="sidebar">
        <a
          class="brand"
          href="#/"
          onClick={(event) => {
            event.preventDefault();
            navigate("/");
          }}
        >
          <Icon name="Stacks" />
          <span>Porty</span>
        </a>
        <p class="tagline">
          Docker Compose
          <br />
          made simple
        </p>
        <nav aria-label="Main navigation">
          {nav.map((item) => (
            <a
              href={`#${item.path}`}
              class={
                route === item.path || (item.path === "/" && stackSelected)
                  ? "active"
                  : ""
              }
              onClick={(event) => {
                event.preventDefault();
                navigate(item.path);
              }}
            >
              <Icon name={item.icon || item.name} />
              {item.name}
            </a>
          ))}
        </nav>
        <div class="server-info">
          <span class="muted">Server</span>
          <strong>{location.hostname}</strong>
          <span class="connection">{connection}</span>
          <hr />
          <span>{username}</span>
          <button class="text-button" onClick={onSignOut}>
            Sign out
          </button>
        </div>
      </aside>
      <div class={`workspace ${stackSelected ? "stack-workspace" : ""}`}>
        {children}
      </div>
      {drawer}
    </div>
  );
}
