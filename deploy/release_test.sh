#!/bin/sh
# Smoke-test our archives and images using disposable containers, without a Docker socket mount.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
dist=${1:?Usage: release_test.sh DIST_DIR IMAGE_REF}
image=${2:?Usage: release_test.sh DIST_DIR IMAGE_REF}
test -f "$dist/checksums.txt"
(cd "$dist" && sha256sum -c checksums.txt)
python3 - "$root" "$dist" "$image" <<'PY'
import hashlib, html.parser, os, pathlib, subprocess, sys, tarfile, tempfile, time, urllib.request
root, dist, image = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3]
archives = list(dist.glob('porty_*_linux_amd64.tar.gz'))
assert len(archives) == 1, archives
class Assets(html.parser.HTMLParser):
    def __init__(self): super().__init__(); self.urls=[]
    def handle_starttag(self, tag, attrs):
        for key,value in attrs:
            if key in ('src','href') and value.startswith('/assets/'): self.urls.append(value)
with tempfile.TemporaryDirectory(prefix='porty-release-') as temp:
    os.chmod(temp,0o755)
    with tarfile.open(archives[0]) as tf:
        members=tf.getmembers()
        assert {m.name for m in members} == {'porty','porty.example.yaml','porty.service','INSTALL.txt'}, [m.name for m in members]
        assert all(m.isfile() for m in members), 'archive contains non-regular files'
        tf.extractall(temp,filter='data')
    binary=pathlib.Path(temp)/'porty'
    assert binary.stat().st_mode & 0o111, 'binary is not executable'
    common=['docker','run','--rm','--mount',f'type=bind,src={binary},dst=/usr/local/bin/porty,readonly',image]
    native=subprocess.check_output([*common,'--version'],text=True)
    container=subprocess.check_output(['docker','run','--rm',image,'--version'],text=True)
    assert native == container, (native,container)
    assert native.startswith('porty v') and 'unknown' not in native, native
    for mounts in [[],['--mount',f'type=bind,src={binary},dst=/usr/local/bin/porty,readonly']]:
        cid=subprocess.check_output(['docker','run','-d','-p','127.0.0.1::8080',*mounts,image],text=True).strip()
        try:
            address=subprocess.check_output(['docker','port',cid,'8080/tcp'],text=True).strip()
            origin='http://'+address
            for attempt in range(60):
                try:
                    with urllib.request.urlopen(origin+'/readyz',timeout=2) as response: assert response.status==200
                    break
                except Exception:
                    if attempt==59: raise
                    time.sleep(.25)
            expected=(root/'web/dist/index.html').read_bytes()
            with urllib.request.urlopen(origin+'/',timeout=3) as response: assert response.read()==expected, 'embedded UI is stale'
            assets=Assets();assets.feed(expected.decode())
            assert assets.urls, 'frontend contains no assets'
            for url in assets.urls:
                with urllib.request.urlopen(origin+url,timeout=3) as response:
                    assert response.read()==(root/'web/dist'/url.lstrip('/')).read_bytes(), url
        finally: subprocess.run(['docker','rm','-fv',cid],check=True,stdout=subprocess.DEVNULL)
print('PASS release: archive contract, checksums, Bookworm startup, matching versions, fresh embedded UI')
PY
"$root/deploy/monitoring_test.sh" --image "$image"
