const { execFileSync } = require("node:child_process");

function parseReleaseTag(tag) {
  const match =
    typeof tag === "string" &&
    /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/.exec(
      tag,
    );
  if (
    !match ||
    match[0] !== tag ||
    tag.length > 128 ||
    (match[4] || "")
      .split(".")
      .some((part) => /^\d+$/.test(part) && part.length > 1 && part[0] === "0")
  ) {
    throw new Error(
      "Expected vMAJOR.MINOR.PATCH with optional SemVer prerelease, no build metadata, at most 128 characters",
    );
  }
  return { tag, version: tag.slice(1), prerelease: Boolean(match[4]) };
}

function shouldMakeLatest(tag, releases) {
  const current = parseReleaseTag(tag);
  if (current.prerelease) return false;
  const numbers = current.version.split(".").map(BigInt);
  return releases.every((release) => {
    if (release.draft || release.prerelease) return true;
    let other;
    try {
      other = parseReleaseTag(release.tag_name);
    } catch {
      return true;
    }
    if (other.prerelease) return true;
    const parts = other.version.split(".").map(BigInt);
    for (let i = 0; i < 3; i++)
      if (numbers[i] !== parts[i]) return numbers[i] > parts[i];
    return false;
  });
}

function resolveTag(tag, cwd = process.cwd()) {
  const parsed = parseReleaseTag(tag);
  const git = (...args) =>
    execFileSync("git", args, {
      cwd,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    }).trim();
  const commit = git("rev-parse", "--verify", `refs/tags/${tag}^{commit}`);
  git("merge-base", "--is-ancestor", commit, "refs/remotes/origin/main");
  const date = new Date(git("show", "-s", "--format=%cI", commit))
    .toISOString()
    .replace(".000Z", "Z");
  return { ...parsed, commit, date };
}

module.exports = { parseReleaseTag, shouldMakeLatest, resolveTag };
if (require.main === module)
  process.stdout.write(JSON.stringify(resolveTag(process.argv[2])) + "\n");
