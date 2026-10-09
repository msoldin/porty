import { Badge } from "../../components/Feedback";
import { formatContainerPort } from "./serviceDisplay";
import { containerStatePresentation } from "./statusPresentation";
import type { Container } from "./types";

export function ServiceCells({
  container,
  href,
  onOpen,
  onLogs,
}: {
  container: Container;
  href: string;
  onOpen: () => void;
  onLogs: () => void;
}) {
  const state = containerStatePresentation(
    container.state,
    container.health,
    container.onDemandSleeping,
  );
  return (
    <>
      <td>
        <div class="service-identity">
          <a
            href={`#${href}`}
            aria-label={`Open ${container.name}`}
            onClick={(event) => {
              event.preventDefault();
              onOpen();
            }}
          >
            <strong>{container.service || container.name}</strong>
            <small title={container.name}>{container.name}</small>
          </a>
          <a
            class="container-log-link"
            href={`#${href}/logs`}
            aria-label={`View logs for ${container.name}`}
            onClick={(event) => {
              event.preventDefault();
              onLogs();
            }}
          >
            View logs
          </a>
        </div>
      </td>
      <td>
        <div class="service-state">
          <Badge tone={state.tone} dot>
            {state.label}
          </Badge>
          {container.state === "exited" && container.onDemandSleeping && (
            <small>Stopped by on-demand automation</small>
          )}
          {state.health && (
            <small class={"service-health " + state.healthTone}>
              {state.health}
            </small>
          )}
        </div>
      </td>
      <td>
        <code class="service-image" title={container.image}>
          {container.image || "—"}
        </code>
      </td>
      <td>
        <span class="service-networks" title={container.networks.join(", ")}>
          {container.networks.length
            ? [...container.networks].sort().join(", ")
            : "—"}
        </span>
      </td>
      <td>
        {container.ports.length ? (
          <div class="service-ports">
            {container.ports.map((port, index) => (
              <span key={index} title={formatContainerPort(port)}>
                {formatContainerPort(port)}
              </span>
            ))}
          </div>
        ) : (
          "—"
        )}
      </td>
    </>
  );
}
