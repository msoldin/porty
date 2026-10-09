#!/bin/sh
# Disposable smoke check; never mounts the host root or Docker socket.
set -eu
if [ "${1:-}" != "--image" ] || [ -z "${2:-}" ]; then
  echo "Usage: $0 --image <built-image> [--nested-mounts]" >&2
  exit 2
fi
image=$2
cid=
fixture=
nested=
cleanup() {
  if [ -n "$cid" ]; then docker rm -fv "$cid" >/dev/null; fi
  if [ -n "$nested" ]; then umount "$nested"; fi
  if [ -n "$fixture" ]; then rm -rf "$fixture"; fi
}
trap cleanup EXIT HUP INT TERM
docker run --rm --entrypoint /bin/sh "$image" -ec '
  test "$(id -u)" != 0
  test "$(stat -c %a /var/lib/porty)" = 700
  test "$(stat -c %a /home/porty/.docker/config.json)" = 600
  test "$PORTY_MONITORING_MODE" = host
  ldd /usr/local/bin/porty | grep -q libc.so
  ! /sbin/ldconfig -p | grep -q libnvidia-ml
'
cid=$(docker run -d -p 127.0.0.1::8080 "$image")
address=$(docker port "$cid" 8080/tcp)
python3 - "$address" <<'PY'
import http.cookiejar, json, sys, time, urllib.request
origin = "http://" + sys.argv[1]
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
def request(path, data=None, csrf=None):
    headers = {"Origin": origin}
    if csrf: headers["X-CSRF-Token"] = csrf
    if data is not None: headers["Content-Type"] = "application/json"
    return client.open(urllib.request.Request(origin + path, data=json.dumps(data).encode() if data is not None else None, headers=headers), timeout=3)
for attempt in range(60):
    try:
        with request("/healthz") as response: assert response.status == 200
        break
    except Exception:
        if attempt == 59: raise
        time.sleep(.5)
with request("/api/v1/setup/status") as response: setup = json.load(response)
with request("/api/v1/setup/register", {"username":"packaging-test","password":"isolated-test-password-only"},setup["csrfToken"]): pass
time.sleep(2.2)
with request("/api/v1/monitoring") as response:
    assert response.headers["Cache-Control"] == "no-store"
    snapshot = json.load(response)
assert snapshot["coverage"], snapshot
assert not any(r["state"] == "available" for r in snapshot["current"]["readings"].values()), snapshot
print("PASS OCI: non-root, restrictive modes, glibc, no NVML, no container fallback")
PY
if [ "${3:-}" != "--nested-mounts" ]; then
  echo "SKIP recursive read-only mount check: pass --nested-mounts on a disposable local Linux builder with mount permission"
  exit 0
fi
fixture=$(mktemp -d /tmp/porty-monitoring.XXXXXX)
chmod 0755 "$fixture"
mkdir "$fixture/nested"
mount -t tmpfs -o size=1m,mode=0755 tmpfs "$fixture/nested"
nested=$fixture/nested
printf 'fixture\n' > "$nested/marker"
docker run --rm --user 0 --entrypoint /bin/sh \
  --mount "type=bind,src=$fixture,dst=/fixture,readonly,bind-recursive=readonly,bind-propagation=rprivate" \
  "$image" -ec '
    test -f /fixture/nested/marker
    ! touch /fixture/write-probe
    ! touch /fixture/nested/write-probe
    awk '\''$5 ~ /^\/fixture($|\/)/ { n++; if ($6 !~ /(^|,)ro(,|$)/) exit 1 } END { if (n < 2) exit 1 }'\'' /proc/self/mountinfo
  '
echo "PASS recursive read-only parent and nested mount"

