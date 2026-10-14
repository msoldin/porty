const { readFileSync, writeFileSync, createReadStream } = require("node:fs");
const { join } = require("node:path");
const { createHash } = require("node:crypto");
const { parseReleaseTag } = require("./release-policy.cjs");
const {
  validateManifest,
  expectedFiles,
  sha256,
} = require("./publish-release.cjs");

async function hashFile(path) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  return hash.digest("hex");
}

async function createBundle(dir, identity) {
  const { tag, commit, date, runId, owner, repo, imageId } = identity;
  const archive = `porty_${parseReleaseTag(tag).version}_linux_amd64.tar.gz`;
  const manifest = {
    schema: 1,
    tag,
    commit,
    buildDate: date,
    workflowRunId: runId,
    image: `ghcr.io/${owner.toLowerCase()}/${repo.toLowerCase()}:${tag}`,
    imageId,
    imageArchiveSha256: await hashFile(join(dir, "porty-image.tar")),
    assets: Object.fromEntries(
      [archive, "checksums.txt"].map((name) => [
        name,
        sha256(readFileSync(join(dir, name))),
      ]),
    ),
  };
  validateManifest(manifest, owner, repo);
  writeFileSync(
    join(dir, "release-manifest.json"),
    JSON.stringify(manifest, null, 2) + "\n",
    { flag: "wx" },
  );
  return loadBundle(dir, identity);
}

async function loadBundle(dir, identity) {
  const manifest = JSON.parse(readFileSync(join(dir, "release-manifest.json")));
  validateManifest(manifest, identity.owner, identity.repo);
  if (
    manifest.tag !== identity.tag ||
    manifest.commit !== identity.commit ||
    manifest.buildDate !== identity.date ||
    manifest.workflowRunId !== identity.runId
  )
    throw Error("Build bundle identity mismatch");
  const files = Object.fromEntries(
    Object.keys(manifest.assets).map((name) => [
      name,
      readFileSync(join(dir, name)),
    ]),
  );
  expectedFiles({ manifest, files, ...identity });
  if (
    (await hashFile(join(dir, "porty-image.tar"))) !==
    manifest.imageArchiveSha256
  )
    throw Error("Saved image checksum mismatch");
  return { manifest, files };
}

function recoveryAction(status, artifactAvailable) {
  if (status === "published") return "verify";
  if (artifactAvailable) return "restore";
  if (status === "draft")
    throw Error(
      "The original build bundle is missing or expired; use a new version",
    );
  return "build";
}

module.exports = { createBundle, loadBundle, recoveryAction };
