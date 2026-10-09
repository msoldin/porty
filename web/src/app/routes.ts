export function parseStackRoute(
  route: string,
): { stackId: string; containerId?: string; containerSection?: "logs" } | null {
  const match =
    /^\/stacks\/([^/]+)(?:\/containers\/([^/]+)(?:\/(logs))?)?$/.exec(route);
  if (!match) return null;
  try {
    const stackId = decodeURIComponent(match[1]);
    const containerId = match[2] ? decodeURIComponent(match[2]) : undefined;
    if (!stackId || stackId.includes("/") || (!containerId && match[2]))
      return null;
    if (containerId?.includes("/")) return null;
    return {
      stackId,
      ...(containerId ? { containerId } : {}),
      ...(match[3] ? { containerSection: "logs" as const } : {}),
    };
  } catch {
    return null;
  }
}
