const { parseReleaseTag } = require("./release-policy.cjs");

// Only a registry 404 means absent. Authentication, rate limits and network
// failures must stop publication rather than authorize an overwrite.
async function registryDigest({
  owner,
  repo,
  tag,
  actor,
  token,
  fetch = globalThis.fetch,
}) {
  parseReleaseTag(tag);
  if (!/^[a-zA-Z0-9_.-]+$/.test(owner) || !/^[a-zA-Z0-9_.-]+$/.test(repo))
    throw Error("Invalid image repository");
  const repository = `${owner}/${repo}`.toLowerCase();
  const url = new URL("https://ghcr.io/token");
  url.searchParams.set("service", "ghcr.io");
  url.searchParams.set("scope", `repository:${repository}:pull,push`);
  const auth = await fetch(url, {
    headers: {
      Authorization: `Basic ${Buffer.from(`${actor}:${token}`).toString("base64")}`,
    },
    signal: AbortSignal.timeout(30000),
  });
  if (!auth.ok) throw Error(`Registry authentication failed (${auth.status})`);
  const access = await auth.json();
  if (!access.token) throw Error("Registry authentication returned no token");
  const response = await fetch(
    `https://ghcr.io/v2/${repository}/manifests/${encodeURIComponent(tag)}`,
    {
      method: "HEAD",
      headers: {
        Authorization: `Bearer ${access.token}`,
        Accept:
          "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json",
      },
      signal: AbortSignal.timeout(30000),
    },
  );
  if (response.status === 404) return null;
  if (!response.ok) throw Error(`Registry request failed (${response.status})`);
  const digest = response.headers.get("docker-content-digest");
  if (!/^sha256:[a-f0-9]{64}$/.test(digest))
    throw Error("Registry returned an invalid digest");
  return digest;
}

module.exports = { registryDigest };
