#!/bin/sh
# Requires local Docker. Owns and cleans up its test image and containers.
# PORTY_TRAFFIC_DOCKER_LOOPBACK=1 selects the Docker Desktop loopback fixture;
# otherwise publication is limited to the native Docker bridge gateway.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
staging=$(mktemp -d)
image="porty-traffic-test:$$"
cleanup() {
    docker image rm "$image" >/dev/null 2>&1 || true
    rm -rf "$staging"
}
trap cleanup EXIT HUP INT TERM
CGO_ENABLED=0 go test -c -o "$staging/traffic.test" ./internal/traffic
cp deploy/traffic-runtime.Dockerfile "$staging/Dockerfile"
docker build --quiet -t "$image" "$staging"
docker run --rm --network none --cap-drop ALL "$image" -test.v -test.timeout 30s
PORTY_TRAFFIC_DOCKER_TEST=1 PORTY_TRAFFIC_DOCKER_SAME_PORT=1 \
    PORTY_TRAFFIC_TEST_IMAGE="$image" go test ./internal/traffic \
    -run '^TestDockerPublishedUDPRetryFromSameSocket$' -count=1 -v
