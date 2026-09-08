#!/usr/bin/env python3
"""Build offline release archives and SHA-256 sums using only the standard library."""
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
TARGETS = (("darwin", "arm64"), ("darwin", "amd64"), ("linux", "arm64"), ("linux", "amd64"))


def main():
    version = re.search(r'const version = "([0-9]+\.[0-9]+\.[0-9]+)"',
                        (ROOT / "cmd/ma/main.go").read_text())[1]
    installer = (ROOT / "install.sh").read_text()
    if f"VERSION='{version}'" not in installer:
        raise SystemExit("Update the pinned version in install.sh to match cmd/ma/main.go")
    output = ROOT / "dist" / f"v{version}"
    output.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, CGO_ENABLED="0", GOPROXY="off", GOSUMDB="off",
               GOTOOLCHAIN="local", GOWORK="off", GOFLAGS="-mod=readonly",
               GOMODCACHE=str(ROOT / ".cache/mod"), GOCACHE=str(ROOT / ".cache/go"))
    assets = []
    with tempfile.TemporaryDirectory(prefix="ma-release-", dir=output.parent) as staging:
        staging = Path(staging)
        for target_os, arch in TARGETS:
            binary = staging / "ma"
            print(f"Building {target_os}/{arch}", flush=True)
            subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w",
                            "-o", str(binary), "./cmd/ma"], cwd=ROOT, check=True,
                           env=dict(env, GOOS=target_os, GOARCH=arch, GOAMD64="v1", GOARM64="v8.0"))
            files = [("ma", binary, 0o755), ("README.md", ROOT / "README.md", 0o644),
                     ("THIRD_PARTY_NOTICES.txt", ROOT / "THIRD_PARTY_NOTICES.txt", 0o644)]
            if (ROOT / "LICENSE").exists():
                files.append(("LICENSE", ROOT / "LICENSE", 0o644))
            name = f"ma_{version}_{target_os}_{arch}.tar.gz"
            # Stable tar/gzip metadata, with no host usernames or absolute paths.
            with (staging / name).open("wb") as raw:
                with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
                    with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                        for member, path, mode in files:
                            data = path.read_bytes()
                            info = tarfile.TarInfo(member)
                            info.size, info.mode = len(data), mode
                            archive.addfile(info, io.BytesIO(data))
            os.replace(staging / name, output / name)
            assets.append(output / name)
    shutil.copyfile(ROOT / "install.sh", output / "install.sh")
    assets.append(output / "install.sh")
    checksums = "".join(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n"
                        for path in sorted(assets))
    (output / "SHA256SUMS").write_text(checksums)
    print(f"Release assets: {output.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
