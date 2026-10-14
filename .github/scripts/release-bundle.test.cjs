const { test } = require("node:test");
const assert = require("node:assert/strict");
const { mkdtempSync, writeFileSync, rmSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const {
  createBundle,
  loadBundle,
  recoveryAction,
} = require("./release-bundle.cjs");

test("binds the original archive and saved image to tag, commit and workflow run", async () => {
  const dir = mkdtempSync(join(tmpdir(), "porty-bundle-"));
  const identity = {
    tag: "v0.1.0",
    commit: "a".repeat(40),
    date: "2026-10-14T12:00:00Z",
    runId: "123",
    owner: "msoldin",
    repo: "porty",
    imageId: "sha256:" + "b".repeat(64),
  };
  try {
    writeFileSync(join(dir, "porty_0.1.0_linux_amd64.tar.gz"), "native");
    writeFileSync(join(dir, "porty-image.tar"), "image");
    const { createHash } = require("node:crypto");
    writeFileSync(
      join(dir, "checksums.txt"),
      createHash("sha256").update("native").digest("hex") +
        "  porty_0.1.0_linux_amd64.tar.gz\n",
    );
    await createBundle(dir, identity);
    const original = await loadBundle(dir, identity);
    assert.equal(original.manifest.workflowRunId, "123");
    await assert.rejects(
      loadBundle(dir, { ...identity, commit: "c".repeat(40) }),
      /identity/,
    );
    await assert.rejects(
      loadBundle(dir, { ...identity, runId: "124" }),
      /identity/,
    );
    writeFileSync(join(dir, "porty-image.tar"), "changed");
    await assert.rejects(loadBundle(dir, identity), /image checksum/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
test("requires original artifacts to resume a draft and verifies published releases without them", () => {
  assert.equal(recoveryAction("absent", false), "build");
  assert.equal(recoveryAction("draft", true), "restore");
  assert.equal(recoveryAction("absent", true), "restore");
  assert.equal(recoveryAction("published", false), "verify");
  assert.throws(() => recoveryAction("draft", false), /original build bundle/);
});
