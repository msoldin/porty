import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import {
  cpSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const docs = resolve(dirname(fileURLToPath(import.meta.url)), "..");
test("builds stable content from its tag and next content from the checkout without overwriting on failure", () => {
  const fixture = mkdtempSync(join(tmpdir(), "porty-docs-fixture-"));
  const git = (...args) =>
    execFileSync("git", args, {
      cwd: fixture,
      stdio: "pipe",
      encoding: "utf8",
    }).trim();
  try {
    cpSync(docs, join(fixture, "docs"), {
      recursive: true,
      filter: (path) =>
        !/\/(node_modules|dist|cache|superpowers|test-results|playwright-report)(\/|$)/.test(
          path,
        ),
    });
    mkdirSync(join(fixture, ".github/scripts"), { recursive: true });
    cpSync(
      resolve(docs, "../.github/scripts/release-policy.cjs"),
      join(fixture, ".github/scripts/release-policy.cjs"),
    );
    git("init", "-b", "main");
    git("config", "user.name", "Docs fixture");
    git("config", "user.email", "docs@example.invalid");
    const home = join(fixture, "docs/README.md");
    const content = readFileSync(home, "utf8");
    writeFileSync(home, content + "\nStablefixtureword.\n");
    git("add", ".");
    git("commit", "-m", "Stable docs");
    git("tag", "v0.1.0");
    writeFileSync(home, content + "\nNextfixtureword.\n");
    git("add", ".");
    git("commit", "-m", "Next docs");
    git("update-ref", "refs/remotes/origin/main", "HEAD");
    symlinkSync(
      join(docs, "node_modules"),
      join(fixture, "docs/node_modules"),
      "dir",
    );
    const build = (tag) =>
      execFileSync(process.execPath, ["scripts/build-site.mjs"], {
        cwd: join(fixture, "docs"),
        env: { ...process.env, PORTY_DOCS_STABLE_TAG: tag },
        stdio: "pipe",
        timeout: 120000,
      });
    build("v0.1.0");
    const output = join(fixture, "docs/.vitepress/dist");
    const stable = readFileSync(join(output, "index.html"), "utf8");
    const next = readFileSync(join(output, "next/index.html"), "utf8");
    assert.match(stable, /Stablefixtureword/);
    assert.doesNotMatch(stable, /Nextfixtureword/);
    assert.match(next, /Nextfixtureword/);
    assert.doesNotMatch(next, /Stablefixtureword/);
    assert.match(stable, /Stable documentation — v0\.1\.0/);
    assert.match(next, /Development documentation — main branch/);
    assert.match(stable, /blob\/v0\.1\.0\/README\.md/);
    assert.match(next, /\/porty\/next\/assets\//);
    const metadata = JSON.parse(readFileSync(join(output, "editions.json")));
    assert.equal(metadata.commit, git("rev-parse", "v0.1.0"));
    assert.throws(() => build("v8.0.0"));
    assert.equal(readFileSync(join(output, "index.html"), "utf8"), stable);
    git("tag", "v0.2.0-rc.1");
    assert.throws(() => build("v0.2.0-rc.1"));
    if (process.env.PORTY_DOCS_FIXTURE_OUTPUT)
      cpSync(output, process.env.PORTY_DOCS_FIXTURE_OUTPUT, {
        recursive: true,
      });
    build("");
    assert.match(
      readFileSync(join(output, "index.html"), "utf8"),
      /No stable release yet/,
    );
    assert.match(
      readFileSync(join(output, "index.html"), "utf8"),
      /Nextfixtureword/,
    );
  } catch (error) {
    if (error.stdout) process.stderr.write(error.stdout);
    if (error.stderr) process.stderr.write(error.stderr);
    throw error;
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});
