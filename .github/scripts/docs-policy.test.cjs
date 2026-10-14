const { test } = require("node:test");
const assert = require("node:assert/strict");
const { selectStableTag } = require("./docs-policy.cjs");

test("selects the newest published stable version, independent of API order", () => {
  const releases = [
    { tag_name: "v0.9.0" },
    { tag_name: "v0.11.0-rc.1", prerelease: true },
    { tag_name: "v1.0.0", draft: true },
    { tag_name: "v0.10.0" },
    { tag_name: "v0.8.1" },
    { tag_name: "not-a-version" },
    { tag_name: "v0.12.0", prerelease: true },
    { tag_name: "v9.0.0-beta" },
  ];
  assert.equal(selectStableTag(releases), "v0.10.0");
  assert.equal(selectStableTag(releases.reverse()), "v0.10.0");
});
test("uses development fallback only when there are no stable releases", () => {
  assert.equal(selectStableTag([]), "");
  assert.equal(
    selectStableTag([{ tag_name: "v0.1.0-rc.1", prerelease: true }]),
    "",
  );
});
