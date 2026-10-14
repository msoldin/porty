const { createHash } = require("node:crypto");
const { parseReleaseTag, shouldMakeLatest } = require("./release-policy.cjs");

const sha256 = (bytes) => createHash("sha256").update(bytes).digest("hex");
const digestPattern = /^sha256:[a-f0-9]{64}$/;
const sourceMarker = (commit) => `<!-- porty-source:${commit} -->`;

function validateManifest(manifest, owner, repo) {
  const { version } = parseReleaseTag(manifest.tag);
  const archive = `porty_${version}_linux_amd64.tar.gz`;
  if (
    manifest.schema !== 1 ||
    !/^[a-f0-9]{40}$/.test(manifest.commit) ||
    !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/.test(manifest.buildDate) ||
    !/^\d+$/.test(manifest.workflowRunId) ||
    manifest.image !==
      `ghcr.io/${owner.toLowerCase()}/${repo.toLowerCase()}:${manifest.tag}` ||
    !digestPattern.test(manifest.imageId) ||
    !/^[a-f0-9]{64}$/.test(manifest.imageArchiveSha256) ||
    Object.keys(manifest.assets || {})
      .sort()
      .join(",") !== ["checksums.txt", archive].sort().join(",") ||
    Object.values(manifest.assets).some(
      (value) => !/^[a-f0-9]{64}$/.test(value),
    )
  ) {
    throw Error("Invalid release manifest");
  }
  return archive;
}

function expectedFiles({ manifest, files, owner, repo }) {
  const archive = validateManifest(manifest, owner, repo);
  const expected = {};
  for (const [name, hash] of Object.entries(manifest.assets)) {
    if (!Buffer.isBuffer(files[name]) || sha256(files[name]) !== hash)
      throw Error(`Local checksum mismatch: ${name}`);
    expected[name] = files[name];
  }
  if (
    files["checksums.txt"].toString().trim() !==
    `${manifest.assets[archive]}  ${archive}`
  )
    throw Error("Invalid checksum list");
  expected["release-manifest.json"] = Buffer.from(
    JSON.stringify(manifest, null, 2) + "\n",
  );
  return expected;
}

async function inspectRelease({ github, owner, repo, tag, commit }) {
  parseReleaseTag(tag);
  const releases = await github.paginate(github.rest.repos.listReleases, {
    owner,
    repo,
    per_page: 100,
  });
  const matches = releases.filter((release) => release.tag_name === tag);
  if (matches.length > 1) throw Error("Multiple releases for the same tag");
  if (!matches.length) return { status: "absent", releases, imageDigest: null };
  const release = matches[0];
  const sources = [
    ...(release.body || "").matchAll(/<!-- porty-source:([a-f0-9]{40}) -->/g),
  ];
  if (sources.length !== 1 || sources[0][1] !== commit)
    throw Error("Release source identity conflict");
  const digests = [
    ...release.body.matchAll(/<!-- porty-image:(sha256:[a-f0-9]{64}) -->/g),
  ];
  if (digests.length > 1) throw Error("Multiple image digest records");
  const assets = await github.paginate(github.rest.repos.listReleaseAssets, {
    owner,
    repo,
    release_id: release.id,
    per_page: 100,
  });
  return {
    status: release.draft ? "draft" : "published",
    release,
    releases,
    assets,
    imageDigest: digests[0]?.[1] || null,
  };
}

async function readAsset({ github, owner, repo }, asset) {
  if (asset.state !== "uploaded" || asset.size > 256 * 1024 * 1024)
    throw Error(`Incomplete or oversized asset: ${asset.name}`);
  const { data } = await github.rest.repos.getReleaseAsset({
    owner,
    repo,
    asset_id: asset.id,
    headers: { accept: "application/octet-stream" },
  });
  const bytes = Buffer.from(data);
  if (bytes.length !== asset.size)
    throw Error(`Asset size mismatch: ${asset.name}`);
  return bytes;
}

async function checkAssets(args, state, expected, requireComplete) {
  if (state.release) {
    const builds = [
      ...state.release.body.matchAll(/<!-- porty-build:([a-f0-9]{64}) -->/g),
    ];
    if (
      builds.length !== 1 ||
      builds[0][1] !== sha256(expected["release-manifest.json"])
    )
      throw Error(
        "Release build provenance conflict; restore the original bundle or use a new version",
      );
  }
  const present = new Set();
  for (const asset of state.assets || []) {
    if (!Object.hasOwn(expected, asset.name) || present.has(asset.name))
      throw Error(`Unexpected asset: ${asset.name}`);
    if (!expected[asset.name].equals(await readAsset(args, asset)))
      throw Error(`Remote checksum conflict: ${asset.name}`);
    present.add(asset.name);
  }
  if (requireComplete && present.size !== Object.keys(expected).length)
    throw Error("Release assets are incomplete");
  return present;
}

async function prepareDraft(args) {
  const { github, owner, repo, manifest, notes } = args;
  const expected = expectedFiles(args);
  const state = await inspectRelease({
    ...args,
    tag: manifest.tag,
    commit: manifest.commit,
  });
  const present = await checkAssets(
    args,
    state,
    expected,
    state.status === "published",
  );
  if (state.status === "published") return state.release.id;
  const release =
    state.release ||
    (
      await github.rest.repos.createRelease({
        owner,
        repo,
        tag_name: manifest.tag,
        target_commitish: manifest.commit,
        name: manifest.tag,
        draft: true,
        prerelease: parseReleaseTag(manifest.tag).prerelease,
        make_latest: "false",
        body: `${notes || ""}\n\n${sourceMarker(manifest.commit)}\n<!-- porty-build:${sha256(expected["release-manifest.json"])} -->`,
      })
    ).data;
  for (const [name, data] of Object.entries(expected)) {
    if (!present.has(name))
      await github.rest.repos.uploadReleaseAsset({
        owner,
        repo,
        release_id: release.id,
        name,
        data,
        headers: {
          "content-type": "application/octet-stream",
          "content-length": data.length,
        },
      });
  }
  return release.id;
}

function imageAction(recorded, observed) {
  if (recorded && !digestPattern.test(recorded))
    throw Error("Invalid recorded digest");
  if (observed && !digestPattern.test(observed))
    throw Error("Invalid registry digest");
  if (!recorded && observed)
    throw Error("Existing image has no recorded provenance; use a new version");
  if (recorded && !observed)
    throw Error("Recorded image is missing from registry");
  if (recorded !== observed)
    throw Error("Registry digest conflicts with recorded digest");
  return recorded ? "reuse" : "push";
}

async function recordImageDigest(args) {
  const { github, owner, repo, manifest, releaseId, imageDigest } = args;
  if (!digestPattern.test(imageDigest)) throw Error("Invalid image digest");
  const state = await inspectRelease({
    ...args,
    tag: manifest.tag,
    commit: manifest.commit,
  });
  if (state.release?.id !== releaseId) throw Error("Release identity conflict");
  if (state.imageDigest === imageDigest) return;
  if (state.imageDigest || state.status === "published")
    throw Error("Image digest conflict");
  await github.rest.repos.updateRelease({
    owner,
    repo,
    release_id: releaseId,
    body: `${state.release.body}\n\nContainer: \`${manifest.image.split(":")[0]}@${imageDigest}\`\n\n<!-- porty-image:${imageDigest} -->`,
  });
}

async function finalizeRelease(args) {
  const { github, owner, repo, manifest, releaseId, imageDigest } = args;
  const state = await inspectRelease({
    ...args,
    tag: manifest.tag,
    commit: manifest.commit,
  });
  if (state.release?.id !== releaseId) throw Error("Release identity conflict");
  await checkAssets(args, state, expectedFiles(args), true);
  if (!imageDigest || state.imageDigest !== imageDigest)
    throw Error("Image digest must be recorded before publication");
  if (state.status === "published") return;
  const releases = await github.paginate(github.rest.repos.listReleases, {
    owner,
    repo,
    per_page: 100,
  });
  await github.rest.repos.updateRelease({
    owner,
    repo,
    release_id: releaseId,
    draft: false,
    prerelease: parseReleaseTag(manifest.tag).prerelease,
    make_latest: String(shouldMakeLatest(manifest.tag, releases)),
  });
}

// Published reruns verify Release assets directly, even after CI artifacts expire.
async function verifyPublished(args) {
  const state = await inspectRelease(args);
  if (state.status !== "published") throw Error("Expected a published release");
  const asset = state.assets.find((a) => a.name === "release-manifest.json");
  if (!asset || asset.size > 16384) throw Error("Missing release manifest");
  const manifest = JSON.parse(await readAsset(args, asset));
  validateManifest(manifest, args.owner, args.repo);
  if (manifest.tag !== args.tag || manifest.commit !== args.commit)
    throw Error("Manifest source conflict");
  const files = {};
  for (const name of Object.keys(manifest.assets)) {
    const found = state.assets.find((a) => a.name === name);
    if (!found) throw Error("Published release is incomplete");
    files[name] = await readAsset(args, found);
  }
  await checkAssets(
    args,
    state,
    expectedFiles({ ...args, manifest, files }),
    true,
  );
  if (!state.imageDigest) throw Error("Published image digest is missing");
  return { ...state, manifest };
}

module.exports = {
  inspectRelease,
  prepareDraft,
  recordImageDigest,
  finalizeRelease,
  imageAction,
  verifyPublished,
  validateManifest,
  expectedFiles,
  sha256,
};
