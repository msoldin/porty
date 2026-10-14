const { parseReleaseTag, shouldMakeLatest } = require("./release-policy.cjs");

function selectStableTag(releases) {
  let selected;
  for (const release of releases) {
    if (release.draft || release.prerelease) continue;
    let version;
    try {
      version = parseReleaseTag(release.tag_name);
    } catch {
      continue;
    }
    if (version.prerelease) continue;
    if (!selected || shouldMakeLatest(version.tag, [selected]))
      selected = release;
  }
  return selected?.tag_name || "";
}

module.exports = { selectStableTag };
