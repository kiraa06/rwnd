import hashlib
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


INSTALLER = Path(__file__).resolve().parents[1] / "install.sh"
UPSTREAM = "https://github.com/kiraa06/rwnd/releases"
FORK = "https://github.com/Somchandra17/claude-cl/releases"


class InstallTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.script = self.root / "install.sh"
        self.script.write_bytes(INSTALLER.read_bytes())
        self.bin = self.root / "bin directory"
        self.assets = self.root / "assets"
        self.assets.mkdir()
        shims = self.root / "shims"
        shims.mkdir()
        self.log = self.root / "urls"
        self.env = dict(os.environ, BINDIR=str(self.bin), FIXTURE=str(self.assets),
                        URL_LOG=str(self.log), PATH=f"{shims}:{os.environ['PATH']}")
        for key in ("VERSION", "RWND_REPO"):
            self.env.pop(key, None)
        self.shim(shims, "uname", """case "$1" in -s) echo Linux;; -m) echo x86_64;; esac""")
        self.shim(shims, "curl", """
            out=
            while [ "$#" -gt 0 ]; do
                case "$1" in
                    -o) out=$2; shift;;
                    -w) shift;;
                    https://*) url=$1;;
                esac
                shift
            done
            printf '%s\\n' "$url" >> "$URL_LOG"
            case "$url" in
                https://github.com/kiraa06/rwnd/releases/latest|https://github.com/Somchandra17/claude-cl/releases/latest)
                    printf '%s' "$LATEST";;
                https://github.com/kiraa06/rwnd/releases/download/*|https://github.com/Somchandra17/claude-cl/releases/download/*)
                    cp "$FIXTURE/${url##*/}" "$out";;
                *) exit 22;;
            esac
        """)
        self.env["LATEST"] = UPSTREAM + "/tag/v1.2.3"

    def shim(self, directory, name, script):
        path = directory / name
        path.write_text("#!/bin/sh\nset -eu\n" + script + "\n")
        path.chmod(0o755)

    def release(self, version="1.2.3", *, checksums=True, corrupt=False, runnable=True):
        name = f"cl_{version}_linux_amd64.tar.gz"
        archive = self.assets / name
        content = (f"#!/bin/sh\nprintf '%s\\n' 'cl {version}'\n" if runnable else
                   "#!/bin/sh\nexit 2\n").encode()
        with tarfile.open(archive, "w:gz") as out:
            member = tarfile.TarInfo("cl")
            member.size = len(content)
            member.mode = 0o755
            out.addfile(member, io.BytesIO(content))
        if checksums:
            digest = "0" * 64 if corrupt else hashlib.sha256(archive.read_bytes()).hexdigest()
            (self.assets / "checksums.txt").write_text(f"{digest}  {name}\n")
        return content

    def install(self, **env):
        return subprocess.run(["sh", str(self.script)], env=self.env | env,
                              text=True, capture_output=True, timeout=10)

    def assert_success(self, result):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_standalone_installer_defaults_to_upstream(self):
        content = self.release()
        self.assert_success(self.install())
        self.assertEqual((self.bin / "cl").read_bytes(), content)
        self.assertEqual(self.log.read_text().splitlines()[0], UPSTREAM + "/latest")

    def test_checked_out_installer_uses_the_fork_origin(self):
        for args in (("init", "-q"), ("add", "install.sh"),
                     ("remote", "add", "origin", "git@github.com:Somchandra17/claude-cl.git")):
            subprocess.run(["git", "-C", str(self.root), *args], check=True)
        self.release()
        self.assert_success(self.install(LATEST=FORK + "/tag/v1.2.3"))
        self.assertEqual(self.log.read_text().splitlines()[0], FORK + "/latest")

    def test_update_replaces_binary_and_repeat_is_a_noop(self):
        self.release()
        self.assert_success(self.install())
        home = self.root / "config directory"
        home.mkdir()
        settings = home / "tool"
        settings.write_text("hermes\n")
        self.env["CL_CONFIG_DIR"] = str(home)
        new = self.release("1.2.4")
        self.env["LATEST"] = UPSTREAM + "/tag/v1.2.4"
        self.assert_success(self.install())
        installed = self.bin / "cl"
        self.assertEqual(installed.read_bytes(), new)
        inode = installed.stat().st_ino
        self.assert_success(self.install(VERSION="v1.2.4"))
        self.assertEqual(installed.stat().st_ino, inode)
        self.assertEqual(settings.read_text(), "hermes\n")
        self.assertEqual(list(self.bin.iterdir()), [installed])

    def test_failed_updates_keep_existing_binary(self):
        self.bin.mkdir()
        installed = self.bin / "cl"
        installed.write_text("original binary")
        installed.chmod(0o755)
        for mode, expected in [("missing", "checksum"), ("bad", "checksum mismatch"),
                               ("broken", "could not run")]:
            with self.subTest(mode=mode):
                self.release(checksums=mode != "missing", corrupt=mode == "bad",
                             runnable=mode != "broken")
                result = self.install(VERSION="v1.2.3")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected, result.stderr.lower())
                self.assertEqual(installed.read_text(), "original binary")
                self.assertEqual(list(self.bin.iterdir()), [installed])

    def test_no_fork_release_does_not_install_upstream(self):
        self.release()
        result = self.install(RWND_REPO="Somchandra17/claude-cl", LATEST=FORK)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Somchandra17/claude-cl", result.stderr)
        self.assertEqual(self.log.read_text().splitlines(), [FORK + "/latest"])
        self.assertFalse(self.bin.exists())

    def test_invalid_version_makes_no_request(self):
        for version in ("../not-a-tag", "v1.2.3\nother-text"):
            with self.subTest(version=version):
                result = self.install(VERSION=version)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("invalid version", result.stderr.lower())
        self.assertFalse(self.log.exists())

    def test_does_not_replace_a_directory_named_cl(self):
        self.release()
        dest = self.bin / "cl"
        dest.mkdir(parents=True)
        result = self.install(VERSION="v1.2.3")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(list(dest.iterdir()), [])

    def test_does_not_replace_a_package_managed_symlink(self):
        self.release()
        self.bin.mkdir()
        managed = self.root / "managed-cl"
        managed.write_text("package-managed binary")
        dest = self.bin / "cl"
        dest.symlink_to(managed)
        result = self.install(VERSION="v1.2.3")
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(dest.is_symlink())
        self.assertEqual(managed.read_text(), "package-managed binary")
        self.assertFalse(self.log.exists())


if __name__ == "__main__":
    unittest.main()
