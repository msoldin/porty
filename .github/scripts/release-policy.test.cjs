const { test } = require("node:test");
const assert = require("node:assert/strict");
const { mkdtempSync, rmSync } = require("node:fs");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const { execFileSync } = require("node:child_process");
const {
  parseReleaseTag,
  shouldMakeLatest,
  resolveTag,
} = require("./release-policy.cjs");

test("accepts stable and prerelease versions usable as image tags", () => {
  assert.deepEqual(parseReleaseTag("v0.1.0"), {
    tag: "v0.1.0",
    version: "0.1.0",
    prerelease: false,
  });
  assert.equal(parseReleaseTag("v0.2.0-rc.1").prerelease, true);
  assert.equal(parseReleaseTag("v1.0.0-beta.0").prerelease, true);
});
test("rejects malformed versions and shell or Docker-invalid values", () => {
  for (const tag of [
    "0.1.0",
    "v01.1.0",
    "v0.1",
    "v0.1.0+build",
    "v0.1.0-rc.01",
    "v0.1.0\n",
    "v0.1.0-",
    "v0.1.0-a..b",
    "v0.1.0;touch /tmp/bad",
    "v0.1.0-" + "x".repeat(128),
  ])
    assert.throws(() => parseReleaseTag(tag), undefined, tag);
});
test("keeps latest on the newest published stable release", () => {
  const releases = [
    { tag_name: "v0.9.0", draft: false, prerelease: false },
    { tag_name: "v1.0.0", draft: true, prerelease: false },
    { tag_name: "v2.0.0-rc.1", draft: false, prerelease: true },
  ];
  assert.equal(shouldMakeLatest("v0.10.0", releases), true);
  assert.equal(shouldMakeLatest("v0.8.1", releases), false);
  assert.equal(shouldMakeLatest("v3.0.0-rc.1", releases), false);
  assert.equal(shouldMakeLatest("v0.9.0", releases), false);
  assert.equal(
    shouldMakeLatest("v9007199254740993.0.0", [
      { tag_name: "v9007199254740992.0.0" },
    ]),
    true,
  );
});
test("resolves the exact selected tag and rejects commits outside main history", () => {
  const cwd = mkdtempSync(join(tmpdir(), "porty-tags-"));
  const git = (...args) =>
    execFileSync("git", args, {
      cwd,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    }).trim();
  try {
    git("init", "-b", "main");
    git("config", "user.name", "Release test");
    git("config", "user.email", "release@example.invalid");
    git("commit", "--allow-empty", "-m", "first");
    const first = git("rev-parse", "HEAD");
    git("tag", "-a", "v0.1.0", "-m", "first");
    git("tag", "v0.1.1");
    git("update-ref", "refs/remotes/origin/main", first);
    assert.equal(resolveTag("v0.1.0", cwd).commit, first);
    assert.equal(resolveTag("v0.1.1", cwd).commit, first);
    git("checkout", "-b", "unmerged");
    git("commit", "--allow-empty", "-m", "unmerged");
    git("tag", "v0.2.0");
    assert.throws(() => resolveTag("v0.2.0", cwd));
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
});
