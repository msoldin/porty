export const containerRoute = (stackId: string, containerId: string): string =>
  `/stacks/${encodeURIComponent(stackId)}/containers/${encodeURIComponent(containerId)}`;
