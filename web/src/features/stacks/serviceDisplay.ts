import type { ContainerPort } from "./types";

export function formatContainerPort(port: ContainerPort): string {
  const target = port.targetPort + "/" + port.protocol;
  if (!port.publishedPort) return target;
  const host =
    port.host.includes(":") && !port.host.startsWith("[")
      ? "[" + port.host + "]"
      : port.host;
  return (host ? host + ":" : "") + port.publishedPort + " → " + target;
}
