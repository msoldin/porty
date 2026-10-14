const { test } = require("node:test");
const assert = require("node:assert/strict");
const { createHash } = require("node:crypto");
const {
  inspectRelease,
  prepareDraft,
  recordImageDigest,
  finalizeRelease,
  imageAction,
  verifyPublished,
} = require("./publish-release.cjs");
const hash = (b) => createHash("sha256").update(b).digest("hex");
const commit = "a".repeat(40),
  digest = "sha256:" + "b".repeat(64);
function fixture() {
  const archive = Buffer.from("tested archive"),
    checksums = Buffer.from(
      hash(archive) + "  porty_0.1.0_linux_amd64.tar.gz\n",
    );
  const manifest = {
    schema: 1,
    tag: "v0.1.0",
    commit,
    buildDate: "2026-10-14T12:00:00Z",
    workflowRunId: "123",
    image: "ghcr.io/msoldin/porty:v0.1.0",
    imageId: "sha256:" + "c".repeat(64),
    imageArchiveSha256: "d".repeat(64),
    assets: {
      "porty_0.1.0_linux_amd64.tar.gz": hash(archive),
      "checksums.txt": hash(checksums),
    },
  };
  const files = {
    "porty_0.1.0_linux_amd64.tar.gz": archive,
    "checksums.txt": checksums,
  };
  let next = 1;
  const state = { releases: [], assets: [], writes: [], failAt: 0 };
  function write(op) {
    state.writes.push(op);
    if (state.failAt === state.writes.length)
      throw Error("injected interruption");
  }
  const repos = {
    listReleases: async () => ({ data: state.releases }),
    listReleaseAssets: async () => ({ data: state.assets }),
    getReleaseAsset: async ({ asset_id }) => ({
      data: state.assets.find((a) => a.id === asset_id).bytes,
    }),
    createRelease: async (args) => {
      write("create");
      const r = { ...args, id: next++, body: args.body, assets: [] };
      state.releases.push(r);
      return { data: r };
    },
    uploadReleaseAsset: async (args) => {
      write("upload:" + args.name);
      const a = {
        id: next++,
        name: args.name,
        size: args.data.length,
        state: "uploaded",
        bytes: Buffer.from(args.data),
      };
      state.assets.push(a);
      return { data: a };
    },
    updateRelease: async (args) => {
      write("update");
      const r = state.releases.find((r) => r.id === args.release_id);
      // GitHub can detach a draft's tag when PATCH omits tag_name.
      if (r.draft && !Object.hasOwn(args, "tag_name"))
        r.tag_name = "untagged-fixture";
      Object.assign(r, args);
      return { data: r };
    },
  };
  const github = {
    rest: { repos },
    paginate: async (fn, args) => (await fn(args)).data,
  };
  const args = {
    github,
    owner: "msoldin",
    repo: "porty",
    manifest,
    files,
    notes: "Release notes",
  };
  return { state, args, manifest };
}
test("preserves the version tag when recording the digest and publishing the draft", async () => {
  const { state, args } = fixture();
  const releaseId = await prepareDraft(args);
  const assetIds = state.assets.map((asset) => asset.id);
  await recordImageDigest({ ...args, releaseId, imageDigest: digest });
  assert.equal(state.releases[0].tag_name, "v0.1.0");
  assert.equal(state.releases[0].draft, true);
  await finalizeRelease({ ...args, releaseId, imageDigest: digest });
  assert.equal(state.releases[0].tag_name, "v0.1.0");
  assert.equal(state.releases[0].draft, false);
  assert.equal(state.releases[0].id, releaseId);
  assert.deepEqual(
    state.assets.map((asset) => asset.id),
    assetIds,
  );
});
test("uploads verified artifacts once and publishes only after recording the image digest", async () => {
  const { state, args, manifest } = fixture();
  const releaseId = await prepareDraft(args);
  assert.equal(state.releases[0].draft, true);
  assert.equal(state.assets.length, 3);
  const writes = state.writes.length;
  assert.equal(await prepareDraft(args), releaseId);
  assert.equal(state.writes.length, writes);
  await assert.rejects(
    finalizeRelease({ ...args, releaseId, imageDigest: digest }),
    /digest/,
  );
  await recordImageDigest({ ...args, releaseId, imageDigest: digest });
  await finalizeRelease({ ...args, releaseId, imageDigest: digest });
  assert.equal(state.releases[0].draft, false);
  assert.equal(state.releases[0].make_latest, "true");
  const done = state.writes.length;
  await prepareDraft(args);
  await finalizeRelease({ ...args, releaseId, imageDigest: digest });
  assert.equal(state.writes.length, done);
  assert.equal(
    (await inspectRelease({ ...args, tag: manifest.tag, commit })).status,
    "published",
  );
});

test("an empty draft still binds retries to the original tested build", async () => {
  const { state, args } = fixture();
  state.failAt = 2;
  await assert.rejects(prepareDraft(args), /interruption/);
  assert.equal(state.assets.length, 0);
  state.failAt = 0;
  args.manifest.imageArchiveSha256 = "e".repeat(64);
  const before = state.writes.length;
  await assert.rejects(prepareDraft(args), /build provenance/);
  assert.equal(state.writes.length, before);
});
test("rejects altered assets and source identity before any writes", async () => {
  for (const change of ["asset", "commit", "local", "extra"]) {
    const { state, args } = fixture();
    await prepareDraft(args);
    const before = state.writes.length;
    if (change === "asset") state.assets[0].bytes = Buffer.from("corrupt");
    if (change === "commit")
      state.releases[0].body = "<!-- porty-source:" + "f".repeat(40) + " -->";
    if (change === "local")
      args.files["checksums.txt"] = Buffer.from("corrupt");
    if (change === "extra")
      state.assets.push({ id: 99, name: "unexpected", size: 0 });
    await assert.rejects(prepareDraft(args));
    assert.equal(state.writes.length, before, change);
  }
});
test("resumes each interrupted upload without replacing existing assets", async () => {
  for (let failAt = 1; failAt <= 4; failAt++) {
    const { state, args } = fixture();
    state.failAt = failAt;
    await assert.rejects(prepareDraft(args), /interruption/);
    state.failAt = 0;
    const surviving = state.assets.map((a) => a.id);
    await prepareDraft(args);
    assert.equal(state.assets.length, 3);
    assert.ok(surviving.every((id) => state.assets.some((a) => a.id === id)));
  }
});
test("never pushes over an existing image and fails closed on missing provenance", () => {
  assert.equal(imageAction(null, null), "push");
  assert.equal(imageAction(digest, digest), "reuse");
  assert.throws(() => imageAction(null, digest), /provenance/);
  assert.throws(
    () => imageAction(digest, "sha256:" + "e".repeat(64)),
    /digest/,
  );
  assert.throws(() => imageAction(digest, null), /missing/);
});
test("does not make an older stable release latest and leaves drafts recoverable", async () => {
  const { state, args } = fixture();
  const releaseId = await prepareDraft(args);
  state.releases.push({
    id: 99,
    tag_name: "v0.2.0",
    draft: false,
    prerelease: false,
  });
  assert.equal(state.releases[0].draft, true);
  await recordImageDigest({ ...args, releaseId, imageDigest: digest });
  await finalizeRelease({ ...args, releaseId, imageDigest: digest });
  assert.equal(state.releases[0].make_latest, "false");
});

test("published reruns verify remote artifacts without CI files and make no writes", async () => {
  const { state, args } = fixture();
  const releaseId = await prepareDraft(args);
  await recordImageDigest({ ...args, releaseId, imageDigest: digest });
  await finalizeRelease({ ...args, releaseId, imageDigest: digest });
  const before = state.writes.length;
  const remote = { ...args, files: undefined, tag: args.manifest.tag, commit };
  assert.equal((await verifyPublished(remote)).manifest.commit, commit);
  assert.equal(state.writes.length, before);
  state.assets[0].bytes = Buffer.from("changed");
  await assert.rejects(verifyPublished(remote));
  assert.equal(state.writes.length, before);
});

test("prerelease publication never claims latest stable", async () => {
  const { state, args } = fixture();
  const old = "porty_0.1.0_linux_amd64.tar.gz",
    name = "porty_0.1.0-rc.1_linux_amd64.tar.gz";
  args.manifest.tag = "v0.1.0-rc.1";
  args.manifest.image = "ghcr.io/msoldin/porty:v0.1.0-rc.1";
  args.files[name] = args.files[old];
  delete args.files[old];
  args.files["checksums.txt"] = Buffer.from(
    hash(args.files[name]) + "  " + name + "\n",
  );
  args.manifest.assets = Object.fromEntries(
    Object.entries(args.files).map(([n, b]) => [n, hash(b)]),
  );
  const releaseId = await prepareDraft(args);
  await recordImageDigest({ ...args, releaseId, imageDigest: digest });
  await finalizeRelease({ ...args, releaseId, imageDigest: digest });
  assert.equal(state.releases[0].prerelease, true);
  assert.equal(state.releases[0].make_latest, "false");
});

test("uses paginated releases so an older draft is found without creating a duplicate", async () => {
  const { state, args } = fixture();
  const releaseId = await prepareDraft(args);
  const before = state.writes.length;
  const list = args.github.rest.repos.listReleases;
  args.github.rest.repos.listReleases = async () => ({ data: [] });
  const paginate = args.github.paginate;
  args.github.paginate = async (fn, options) =>
    fn === args.github.rest.repos.listReleases
      ? (await list()).data
      : paginate(fn, options);
  assert.equal(await prepareDraft(args), releaseId);
  assert.equal(state.writes.length, before);
});

test("handles interruption after each successful remote mutation without replacement", async () => {
  for (const method of [
    "createRelease",
    "uploadReleaseAsset",
    "updateRelease",
  ]) {
    const { state, args } = fixture();
    const original = args.github.rest.repos[method];
    let interrupted = false;
    args.github.rest.repos[method] = async (options) => {
      const result = await original(options);
      if (!interrupted) {
        interrupted = true;
        throw Error("response lost after write");
      }
      return result;
    };
    if (method === "updateRelease") {
      const releaseId = await prepareDraft(args);
      await assert.rejects(
        recordImageDigest({ ...args, releaseId, imageDigest: digest }),
        /response lost/,
      );
      const before = state.writes.length;
      await recordImageDigest({ ...args, releaseId, imageDigest: digest });
      assert.equal(state.writes.length, before);
    } else {
      await assert.rejects(prepareDraft(args), /response lost/);
      const ids = state.assets.map((a) => a.id);
      await prepareDraft(args);
      assert.equal(state.releases.length, 1);
      assert.equal(state.assets.length, 3);
      assert.ok(ids.every((id) => state.assets.some((a) => a.id === id)));
    }
  }
});
