import json
import pathlib
import subprocess
import tempfile
import unittest

SCRIPTS = pathlib.Path(__file__).resolve().parents[1]

class ReleaseContract(unittest.TestCase):
    def invoke(self, *args, cwd=None):
        return subprocess.run(["python3", str(SCRIPTS / "release.py"), *args], cwd=cwd, capture_output=True, text=True)

    def test_component_versions_have_one_unambiguous_channel(self):
        for tag, component, version, channel in [
            ("sdk-go/v2.1.2", "go", "2.1.2", "stable"),
            ("java-2.1.2-alpha1", "java", "2.1.2-alpha1", "alpha"),
        ]:
            with self.subTest(tag=tag):
                r = self.invoke("describe", tag)
                self.assertEqual(r.returncode, 0, r.stderr)
                d = json.loads(r.stdout)
                self.assertEqual((d["component"], d["version"], d["channel"]), (component, version, channel))

    def test_removed_product_release_tags_are_rejected(self):
        for tag in ["ts-2.1.2", "py-2.1.2", "cli-2.1.2", "skills-2.1.2"]:
            with self.subTest(tag=tag):
                result = self.invoke("describe", tag)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Unknown component tag prefix", result.stderr)

    def test_noncanonical_or_injected_tags_are_rejected(self):
        for tag in ["ts-2.1.2a1", "sdk-go/v2.1.2a1", "sdk-go/v02.1.2", "ts-2.1.2-alpha.01", "v2.1.2", "java-2.1.2;echo bad", "py-2.1.2-alpha.1"]:
            with self.subTest(tag=tag):
                r = self.invoke("describe", tag)
                self.assertNotEqual(r.returncode, 0)

    def test_check_binds_tag_to_branch_and_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            def git(*args):
                r = subprocess.run(["git", *args], cwd=root, capture_output=True, text=True)
                self.assertEqual(r.returncode, 0, r.stderr)
                return r.stdout.strip()
            git("init", "-q", "-b", "main")
            git("config", "user.name", "Release Test")
            git("config", "user.email", "test@example.com")
            (root / "sdk-go/core").mkdir(parents=True)
            (root / "sdk-go/go.mod").write_text("module github.com/novitalabs/sandbox-sdks/sdk-go/v2\n")
            manifest = root / "sdk-go/core/connection_config.go"
            manifest.write_text('const SDKVersion = "2.1.2"\n')
            git("add", "."); git("commit", "-qm", "stable")
            main = git("rev-parse", "HEAD")
            git("checkout", "-q", "--orphan", "alpha")
            manifest.write_text('const SDKVersion = "2.1.2-alpha.1"\n')
            git("add", "."); git("commit", "-qm", "alpha")
            self.assertNotEqual(self.invoke("check", "sdk-go/v2.1.2", cwd=root).returncode, 0)
            self.assertEqual(self.invoke("check", "sdk-go/v2.1.2-alpha.1", cwd=root).returncode, 0)
            self.assertNotEqual(self.invoke("check", "sdk-go/v2.1.3-alpha.1", cwd=root).returncode, 0)
            git("checkout", "-q", "main")
            self.assertEqual(self.invoke("check", "sdk-go/v2.1.2", "--commit", main, cwd=root).returncode, 0)

class ArtifactContract(unittest.TestCase):
    def invoke(self, tag, root):
        return subprocess.run(["python3", str(SCRIPTS / "release.py"), "artifacts", tag, "--directory", str(root)], capture_output=True, text=True)

    def test_java_names_alone_do_not_prove_package_identity(self):
        import zipfile
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            for suffix in ("", "-sources", "-javadoc"):
                with zipfile.ZipFile(root / ("novita-sandbox-2.1.2" + suffix + ".jar"), "w") as archive:
                    archive.writestr("META-INF/maven/ai.novita/novita-sandbox/pom.properties", "groupId=ai.novita\nartifactId=novita-sandbox\nversion=9.9.9\n")
            self.assertNotEqual(self.invoke("java-2.1.2", root).returncode, 0)

    def test_java_archives_require_embedded_license_text(self):
        import zipfile
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            for suffix in ("", "-sources", "-javadoc"):
                with zipfile.ZipFile(root / ("novita-sandbox-2.1.2" + suffix + ".jar"), "w") as archive:
                    archive.writestr("META-INF/maven/ai.novita/novita-sandbox/pom.properties", "groupId=ai.novita\nartifactId=novita-sandbox\nversion=2.1.2\n")
            self.assertNotEqual(self.invoke("java-2.1.2", root).returncode, 0)
            for suffix in ("", "-sources", "-javadoc"):
                with zipfile.ZipFile(root / ("novita-sandbox-2.1.2" + suffix + ".jar"), "a") as archive:
                    archive.writestr("doc-files/LICENSE" if suffix == "-javadoc" else "META-INF/LICENSE", "MIT License\nCopyright (c) 2025 Novita AI\nPermission is hereby granted, free of charge")
            self.assertEqual(self.invoke("java-2.1.2", root).returncode, 0)

    def test_go_source_must_match_module_path_and_version(self):
        import tarfile, io
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            with tarfile.open(root / "novita-sandbox-go-2.1.2.tar.gz", "w:gz") as archive:
                data = b"module example.com/wrong/v2\n"
                entry = tarfile.TarInfo("./go.mod"); entry.size = len(data)
                archive.addfile(entry, io.BytesIO(data))
            self.assertNotEqual(self.invoke("sdk-go/v2.1.2", root).returncode, 0)

    def test_hook_rejects_unpublished_commit_before_scanning(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            def git(*args):
                return subprocess.check_output(["git", *args], cwd=root, text=True, stderr=subprocess.DEVNULL).strip()
            git("init", "-q", "-b", "main")
            git("config", "user.name", "Release Test"); git("config", "user.email", "test@example.com")
            (root / "sdk-go/core").mkdir(parents=True)
            (root / "sdk-go/go.mod").write_text("module github.com/novitalabs/sandbox-sdks/sdk-go/v2\n")
            (root / "sdk-go/core/connection_config.go").write_text('const SDKVersion = "2.1.2"\n')
            git("add", "."); git("commit", "-qm", "public")
            remote = root / "remote.git"; git("clone", "--bare", ".", str(remote)); git("remote", "add", "origin", str(remote))
            (root / "extra.txt").write_text("unpublished")
            git("add", "extra.txt"); git("commit", "-qm", "local only")
            sha = git("rev-parse", "HEAD")
            r = subprocess.run(["python3", str(SCRIPTS / "pre-push.py"), "origin"], input=f"refs/tags/sdk-go/v2.1.2 {sha} refs/tags/sdk-go/v2.1.2 {'0'*40}\n", cwd=root, text=True, capture_output=True)
            self.assertNotEqual(r.returncode, 0)
            self.assertIn("must belong to main", r.stderr)

if __name__ == "__main__":
    unittest.main()
