"""Exercise the real POSIX installer with local, adversarial download fixtures."""
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
VERSION = re.search(r"VERSION='([^']+)'", (ROOT / "install.sh").read_text())[1]


class InstallerTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="ma installer test ")
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.prefix = self.root / "install prefix"
        self.fake_bin = self.root / "tools"
        self.fake_bin.mkdir()
        self.env = dict(os.environ, PATH=str(self.fake_bin) + os.pathsep + os.environ["PATH"],
                        MA_FIXTURES=str(self.root), MA_TEST_OS="Darwin", MA_TEST_ARCH="arm64")
        self.fake_tool("uname", '#!/bin/sh\ncase "$1" in -s) echo "$MA_TEST_OS";; -m) echo "$MA_TEST_ARCH";; esac\n')
        self.fake_tool("curl", f"#!{sys.executable}\n" + '''import os, pathlib, shutil, sys
args = sys.argv[1:]
assert args[0] == '--disable'
assert args[args.index('--proto') + 1] == '=https'
assert args[args.index('--proto-redir') + 1] == '=https'
assert '--fail' in args and '--tlsv1.2' in args
assert args[-1].startswith('https://github.com/liuyang1520/ma/releases/download/v')
root = pathlib.Path(os.environ['MA_FIXTURES'])
name = args[-1].rsplit('/', 1)[1]
shutil.copyfile(root / name, args[args.index('--output') + 1])
''')
        self.payload = b'#!/bin/sh\necho "installer must not execute downloaded programs" >&2\nexit 99\n'

    def fake_tool(self, name, content):
        path = self.fake_bin / name
        path.write_text(content)
        path.chmod(0o755)

    def fixture(self, target_os="darwin", arch="arm64", extra=None, missing=None):
        name = f"ma_{VERSION}_{target_os}_{arch}.tar.gz"
        entries = {"ma": self.payload, "README.md": b"readme", "LICENSE": b"MIT",
                   "THIRD_PARTY_NOTICES.txt": b"third-party licenses"}
        if missing:
            del entries[missing]
        with tarfile.open(self.root / name, "w:gz") as archive:
            for member, data in entries.items():
                info = tarfile.TarInfo(member)
                info.size = len(data)
                archive.addfile(info, io.BytesIO(data))
            if extra:
                archive.addfile(extra)
        digest = hashlib.sha256((self.root / name).read_bytes()).hexdigest()
        (self.root / "SHA256SUMS").write_text(f"{digest}  {name}\n")
        return name

    def run_installer(self, *args):
        return subprocess.run(["sh", str(ROOT / "install.sh"), "--prefix", str(self.prefix), *args],
                              env=self.env, capture_output=True, text=True, timeout=15)

    def existing_install(self):
        binary = self.prefix / "bin/ma"
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b"existing installation")
        return binary

    def test_supported_platforms_and_atomic_replacement(self):
        self.existing_install()
        for system, machine, target, arch in (("Darwin", "arm64", "darwin", "arm64"),
                                              ("Darwin", "x86_64", "darwin", "amd64"),
                                              ("Linux", "aarch64", "linux", "arm64"),
                                              ("Linux", "x86_64", "linux", "amd64")):
            with self.subTest(system=system, machine=machine):
                self.env.update(MA_TEST_OS=system, MA_TEST_ARCH=machine)
                self.fixture(target, arch)
                result = self.run_installer()
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual((self.prefix / "bin/ma").read_bytes(), self.payload)
                self.assertEqual((self.prefix / "bin/ma").stat().st_mode & 0o777, 0o755)
                self.assertEqual((self.prefix / "share/doc/ma/LICENSE").read_text(), "MIT")
                self.assertFalse(list((self.prefix / "bin").glob(".ma-install.*")))

    def test_bad_checksum_preserves_existing_install(self):
        binary = self.existing_install()
        name = self.fixture()
        with (self.root / name).open("ab") as archive:
            archive.write(b"tampered")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SHA-256 mismatch", result.stderr)
        self.assertEqual(binary.read_bytes(), b"existing installation")

    def test_missing_or_ambiguous_checksum(self):
        self.fixture()
        checksums = self.root / "SHA256SUMS"
        valid = checksums.read_text()
        for value in ("", valid + valid, "invalid  " + valid.split()[1] + "\n"):
            with self.subTest(value=value):
                checksums.write_text(value)
                result = self.run_installer()
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.prefix.exists())

    def test_unexpected_archive_member_cannot_escape(self):
        for member in ("../escaped", "/tmp/ma-installer-escaped", "ma"):
            with self.subTest(member=member):
                self.fixture(extra=tarfile.TarInfo(member))
                result = self.run_installer()
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.prefix.exists())
                self.assertFalse((self.root / "escaped").exists())

    def test_missing_payload_is_rejected(self):
        self.fixture(missing="ma")
        self.assertNotEqual(self.run_installer().returncode, 0)
        self.assertFalse(self.prefix.exists())

    def test_unsupported_platform_and_arguments_fail_before_download(self):
        self.env["MA_TEST_ARCH"] = "riscv64"
        result = self.run_installer()
        self.assertIn("supported architectures", result.stderr)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotEqual(self.run_installer("--unknown").returncode, 0)
        self.assertNotEqual(self.run_installer("--prefix", "relative/path").returncode, 0)


if __name__ == "__main__":
    unittest.main()
