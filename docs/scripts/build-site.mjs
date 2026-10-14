import { execFileSync } from "node:child_process";
import {
  cpSync,
  mkdtempSync,
  mkdirSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const {
  resolveTag,
  parseReleaseTag,
} = require("../../.github/scripts/release-policy.cjs");
const docs = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const root = resolve(docs, "..");
const output = join(docs, ".vitepress/dist");
const temporary = mkdtempSync(join(tmpdir(), "porty-docs-"));
const stableTag = process.env.PORTY_DOCS_STABLE_TAG || "";

try {
  let stableSource = docs;
  let commit = "";
  if (stableTag) {
    if (parseReleaseTag(stableTag).prerelease)
      throw Error("Default documentation requires a stable release");
    ({ commit } = resolveTag(stableTag, root));
    const archive = join(temporary, "source.tar");
    execFileSync(
      "git",
      ["archive", "--format=tar", `--output=${archive}`, commit, "docs"],
      { cwd: root },
    );
    execFileSync("tar", ["-xf", archive, "-C", temporary]);
    stableSource = join(temporary, "docs");
    // Markdown outside the checkout still needs the current renderer's locked
    // dependencies. Never install or execute tooling from the historical tag.
    symlinkSync(
      join(docs, "node_modules"),
      join(stableSource, "node_modules"),
      "dir",
    );
  }
  const editions = [
    {
      name: "default",
      source: stableSource,
      ref: stableTag || "main",
      base: "/porty/",
    },
    { name: "next", source: docs, ref: "main", base: "/porty/next/" },
  ];
  for (const edition of editions) {
    edition.output = join(temporary, edition.name + "-output");
    execFileSync(
      process.execPath,
      [join(docs, "node_modules/vitepress/bin/vitepress.js"), "build", docs],
      {
        cwd: docs,
        stdio: "inherit",
        env: {
          ...process.env,
          PORTY_DOCS_SOURCE: edition.source,
          PORTY_DOCS_REF: edition.ref,
          PORTY_DOCS_BASE: edition.base,
          PORTY_DOCS_OUT: edition.output,
          PORTY_DOCS_STABLE_TAG: stableTag,
        },
      },
    );
  }
  // Replace the upload directory only after both builds succeed.
  rmSync(output, { recursive: true, force: true });
  mkdirSync(output, { recursive: true });
  cpSync(editions[0].output, output, { recursive: true });
  cpSync(editions[1].output, join(output, "next"), { recursive: true });
  writeFileSync(
    join(output, "editions.json"),
    JSON.stringify(
      {
        stableTag,
        commit,
        editions: editions.map(({ base, ref, source }) => ({
          base,
          ref,
          guides: readdirSync(source).filter((name) => name.endsWith(".md")),
        })),
      },
      null,
      2,
    ),
  );
} finally {
  rmSync(temporary, { recursive: true, force: true });
}
