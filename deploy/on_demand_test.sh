#!/bin/sh
# Opt-in local Docker lifecycle gate. Creates only labeled disposable fixtures.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
staging=$(mktemp -d)
image="porty-on-demand-test:$$"
cleanup() {
    docker image rm "$image" >/dev/null 2>&1 || true
    rm -rf "$staging"
}
trap cleanup EXIT HUP INT TERM
CGO_ENABLED=0 go test -c -o "$staging/control.test" ./internal/control
printf 'FROM alpine:3.22\nRUN mkdir -p /root/.docker && printf "{}" > /root/.docker/config.json\nCOPY --chmod=0555 control.test /control.test\nENTRYPOINT ["/control.test"]\n' > "$staging/Dockerfile"
docker build --quiet -t "$image" "$staging"
docker run --rm --network host --cap-drop ALL \
    --label com.docker.compose.project=porty-on-demand-test \
    --label io.porty.test=on-demand-controller \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -e PORTY_ON_DEMAND_DOCKER_TEST=1 -e PORTY_ON_DEMAND_TEST_IMAGE="$image" \
    -e PORTY_ON_DEMAND_PERF="${PORTY_ON_DEMAND_PERF:-}" \
    -e PORTY_ON_DEMAND_IDLE_TEST="${PORTY_ON_DEMAND_IDLE_TEST:-}" \
    "$image" -test.run '^TestOnDemandDockerLifecycle$' -test.v -test.timeout 100s
