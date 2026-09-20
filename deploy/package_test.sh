#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
unit="$root/deploy/systemd/porty.service"

test -f "$unit"
test -f "$root/Dockerfile"
test -f "$root/deploy/porty.example.yaml"
status=0
verification=$(systemd-analyze verify "$unit" 2>&1) || status=$?
unexpected=$(printf '%s\n' "$verification" | sed '\|Command /usr/local/bin/porty is not executable: No such file or directory|d' | sed '/^[[:space:]]*$/d')
if [ "$status" -ne 0 ] && [ -n "$unexpected" ]; then
  printf '%s\n' "$unexpected" >&2
  exit "$status"
fi

if [ "${PORTY_LIVE_DOCKER_CHECK:-}" = "1" ]; then
  docker build --tag porty:verify "$root"
else
  echo "SKIP live OCI build: set PORTY_LIVE_DOCKER_CHECK=1 on a Docker-enabled host"
fi
