#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"
mode=${1:-snapshot}
case "$mode" in
  release)
    : "${PORTY_RELEASE_TAG:?}" "${PORTY_RELEASE_COMMIT:?}" "${PORTY_RELEASE_DATE:?}"
    test -z "$(git status --porcelain)" || { echo 'Release source checkout must be clean' >&2; exit 1; }
    node -e 'const p=require("./.github/scripts/release-policy.cjs").resolveTag(process.env.PORTY_RELEASE_TAG); for(const [a,b] of [[p.commit,process.env.PORTY_RELEASE_COMMIT],[p.date,process.env.PORTY_RELEASE_DATE]]) if(a!==b) throw Error("Release identity mismatch")'
    test "$(git rev-parse HEAD)" = "$PORTY_RELEASE_COMMIT"
    ;;
  snapshot)
    PORTY_RELEASE_COMMIT=$(git rev-parse HEAD)
    PORTY_RELEASE_TAG="v0.0.0-dev.$(git rev-parse --short=7 HEAD)"
    PORTY_RELEASE_DATE=$(node -e 'const {execFileSync}=require("node:child_process"); console.log(new Date(execFileSync("git",["show","-s","--format=%cI","HEAD"],{encoding:"utf8"}).trim()).toISOString().replace(".000Z","Z"))')
    ;;
  *) echo 'Usage: deploy/build-release.sh snapshot|release' >&2; exit 2 ;;
esac
PORTY_RELEASE_VERSION=${PORTY_RELEASE_TAG#v}
export PORTY_RELEASE_TAG PORTY_RELEASE_COMMIT PORTY_RELEASE_DATE PORTY_RELEASE_VERSION
(cd web && bun ci && bun run build)
if [ "$mode" = release ]; then
  test -z "$(git status --porcelain -- . ':!web/dist')" || { echo 'Build modified release source outside web/dist' >&2; exit 1; }
fi
docker build --platform linux/amd64 -f deploy/release-builder.Dockerfile -t porty:release-builder .
gitcommon=$(git rev-parse --path-format=absolute --git-common-dir)
set -- --rm --platform linux/amd64 --mount "type=bind,src=$root,dst=$root" --workdir "$root" \
  --mount "type=volume,src=porty-release-go-mod,dst=/go/pkg/mod" \
  --mount "type=volume,src=porty-release-go-build,dst=/root/.cache/go-build" \
  -e PORTY_RELEASE_TAG -e PORTY_RELEASE_VERSION -e PORTY_RELEASE_COMMIT -e PORTY_RELEASE_DATE \
  -e "GORELEASER_CURRENT_TAG=$PORTY_RELEASE_TAG" -e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory -e "GIT_CONFIG_VALUE_0=$root"
# Keep Git metadata read-only, including when a linked worktree points outside root.
set -- "$@" --mount "type=bind,src=$gitcommon,dst=$gitcommon,readonly"
docker run "$@" porty:release-builder check
if [ "$mode" = snapshot ]; then
  docker run "$@" porty:release-builder release --snapshot --clean --skip=publish,validate
else
  docker run "$@" porty:release-builder release --clean --skip=publish,validate
fi
# The builder runs as root for its caches; return only generated outputs to the caller.
docker run "$@" --entrypoint chown porty:release-builder -R "$(id -u):$(id -g)" "$root/dist"
docker build --platform linux/amd64 --build-arg "PORTY_VERSION=$PORTY_RELEASE_TAG" \
  --build-arg "PORTY_COMMIT=$PORTY_RELEASE_COMMIT" --build-arg "PORTY_BUILD_DATE=$PORTY_RELEASE_DATE" \
  --tag porty:release-test .
./deploy/release_test.sh "$root/dist" porty:release-test
docker save --output dist/porty-image.tar porty:release-test
echo 'PASS: verified artifacts are in dist/; nothing was published'
