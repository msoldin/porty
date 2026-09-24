import { expect, it } from "vitest";
import { formatContainerPort } from "./serviceDisplay";

it("formats IPv4, IPv6, and unpublished runtime ports without inventing hosts", () => {
  expect(
    formatContainerPort({
      host: "127.0.0.1",
      publishedPort: 8080,
      targetPort: 80,
      protocol: "tcp",
    }),
  ).toBe("127.0.0.1:8080 → 80/tcp");
  expect(
    formatContainerPort({
      host: "::1",
      publishedPort: 8443,
      targetPort: 443,
      protocol: "tcp",
    }),
  ).toBe("[::1]:8443 → 443/tcp");
  expect(
    formatContainerPort({
      host: "",
      publishedPort: 0,
      targetPort: 9000,
      protocol: "udp",
    }),
  ).toBe("9000/udp");
});
