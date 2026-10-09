export const containerRoute = (
  stackId: string,
  containerId: string,
  section?: "logs",
): string =>
  `/stacks/${encodeURIComponent(stackId)}/containers/${encodeURIComponent(containerId)}${section === "logs" ? "/logs" : ""}`;
