import assert from "node:assert/strict";
import { test } from "node:test";
import { rewriteDocLink } from "./links.mjs";

test("stable repository links point at the selected version", () => {
  assert.equal(
    rewriteDocLink("../README.md#porty", "v0.1.0"),
    "https://github.com/msoldin/porty/blob/v0.1.0/README.md#porty",
  );
  assert.equal(
    rewriteDocLink("../deploy/porty.example.yaml", "v0.1.0"),
    "https://github.com/msoldin/porty/blob/v0.1.0/deploy/porty.example.yaml",
  );
});

test("keeps documentation home links usable after the site rewrite", () => {
  assert.equal(rewriteDocLink("README.md#daily-use"), "index.md#daily-use");
  assert.equal(
    rewriteDocLink("./README.md?view=1#daily-use"),
    "index.md?view=1#daily-use",
  );
});

test("links repository documents to GitHub without losing fragments", () => {
  assert.equal(
    rewriteDocLink("../AGENTS.md"),
    "https://github.com/msoldin/porty/blob/main/AGENTS.md",
  );
  assert.equal(
    rewriteDocLink("../README.md#porty"),
    "https://github.com/msoldin/porty/blob/main/README.md#porty",
  );
  assert.equal(
    rewriteDocLink("../deploy/porty.example.yaml"),
    "https://github.com/msoldin/porty/blob/main/deploy/porty.example.yaml",
  );
});

test("preserves guide, external, and anchor links", () => {
  for (const href of [
    "operator-guide.md#upgrade-and-rollback",
    "https://example.com/README.md",
    "#daily-use",
    "images/dashboard.png",
    "other/README.md",
  ]) {
    assert.equal(rewriteDocLink(href), href);
  }
});
