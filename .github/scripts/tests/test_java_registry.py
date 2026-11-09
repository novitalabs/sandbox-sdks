"""Central must reject duplicate versions and return every signed artifact."""
import importlib.util
import io
import os
import pathlib
import tempfile
import unittest
import urllib.error
from unittest.mock import patch

SCRIPTS = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("java_published", SCRIPTS / "verify-published.py")
published = importlib.util.module_from_spec(spec)
spec.loader.exec_module(published)
release = published.release


class JavaRegistry(unittest.TestCase):
    tags = ("java-2.1.2", "java-2.1.2-alpha.2", "java-2.1.2-beta.1", "java-2.1.2-rc.1")

    def test_existing_central_version_is_rejected_for_every_channel(self):
        for tag in self.tags:
            with self.subTest(tag=tag):
                info = release.describe(tag)
                expected = ("https://repo.maven.apache.org/maven2/ai/novita/novita-sandbox/"
                            + info["version"] + "/novita-sandbox-" + info["version"] + ".pom")
                def existing(request, **kwargs):
                    self.assertEqual(request.full_url, expected)
                    self.assertIsNone(request.get_header("Authorization"))
                    return io.BytesIO(b"existing pom")
                with patch.object(release.urllib.request, "urlopen", side_effect=existing):
                    with self.assertRaisesRegex(ValueError, "already published"):
                        release.check_registry(info)

    def test_prerelease_registry_check_only_accepts_404(self):
        info = release.describe("java-2.1.2-alpha.2")
        for code in (404, 401, 403, 429, 500):
            error = urllib.error.HTTPError("fixture", code, "fixture", None, io.BytesIO())
            self.addCleanup(error.close)
            with self.subTest(code=code), patch.object(
                    release.urllib.request, "urlopen",
                    side_effect=error):
                if code == 404:
                    release.check_registry(info)
                else:
                    with self.assertRaisesRegex(ValueError, "Registry check failed"):
                        release.check_registry(info)
        with patch.object(release.urllib.request, "urlopen", side_effect=urllib.error.URLError("offline")):
            with self.assertRaisesRegex(ValueError, "Registry unavailable"):
                release.check_registry(info)

    def artifacts(self, root, version):
        files = {}
        for suffix in (".jar", "-sources.jar", "-javadoc.jar", ".pom"):
            for signature in ("", ".asc"):
                name = "novita-sandbox-" + version + suffix + signature
                data = ("fixture bytes for " + name).encode()
                (root / name).write_bytes(data)
                files[name] = data
        return files

    def test_every_signed_artifact_is_read_from_public_central(self):
        for tag in self.tags:
            with self.subTest(tag=tag), tempfile.TemporaryDirectory() as tmp:
                info = release.describe(tag)
                root = pathlib.Path(tmp)
                files = self.artifacts(root, info["version"])
                seen = set()
                prefix = ("https://repo.maven.apache.org/maven2/ai/novita/novita-sandbox/"
                          + info["version"] + "/")
                def read(request, **kwargs):
                    self.assertTrue(request.full_url.startswith(prefix))
                    self.assertIsNone(request.get_header("Authorization"))
                    name = request.full_url.removeprefix(prefix)
                    seen.add(name)
                    return io.BytesIO(files[name])
                with patch.dict(os.environ, {}, clear=True), patch.object(
                        published.urllib.request, "urlopen", side_effect=read):
                    published.verify(info, root)
                self.assertEqual(seen, set(files))

    def test_missing_or_changed_central_artifact_cannot_pass_verification(self):
        info = release.describe("java-2.1.2-alpha.2")
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            files = self.artifacts(root, info["version"])
            for name in files:
                with self.subTest(missing=name):
                    (root / name).unlink()
                    with self.assertRaisesRegex(ValueError, "Missing signed Java release artifact"):
                        published.verify(info, root)
                    (root / name).write_bytes(files[name])
                def read(request, **kwargs):
                    remote_name = request.full_url.rsplit("/", 1)[-1]
                    return io.BytesIO(b"corrupt" if remote_name == name else files[remote_name])
                with self.subTest(changed=name), patch.dict(os.environ, {}, clear=True), patch.object(
                        published.urllib.request, "urlopen", side_effect=read):
                    with self.assertRaisesRegex(ValueError, "Maven artifact differs"):
                        published.verify(info, root)


if __name__ == "__main__":
    unittest.main()
