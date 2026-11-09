"""Execute the workflow's shell steps without contacting a package registry."""
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import textwrap
import unittest


WORKFLOW = pathlib.Path(__file__).resolve().parents[2] / "workflows/publish.yaml"


def workflow_script(step_name):
    # Extract a named literal run block; execute the actual workflow shell code.
    for step in WORKFLOW.read_text().split("      - name: ")[1:]:
        if step.splitlines()[0] != step_name:
            continue
        match = re.search(r"^        run: \|\n((?:          .*\n|\n)+)", step, re.M)
        if match:
            return textwrap.dedent(match[1])
    raise AssertionError("Missing workflow run block: " + step_name)


class JavaPublishing(unittest.TestCase):
    credentials = {
        "MAVEN_GPG_PRIVATE_KEY": "fixture-signing-key",
        "MAVEN_GPG_PASSPHRASE": "fixture-signing-passphrase",
        "CENTRAL_USERNAME": "fixture-central-user",
        "CENTRAL_TOKEN": "fixture-central-token",
    }

    def check_credentials(self, channel, credentials=None, component="java"):
        env = {"PATH": os.environ["PATH"], "CHANNEL": channel, "COMPONENT": component}
        env.update(credentials or {})
        return subprocess.run(
            ["bash", "-eu", "-o", "pipefail", "-c",
             workflow_script("Verify configured publishing credentials")],
            env=env, text=True, capture_output=True,
        )

    def test_all_java_channels_reject_missing_signing_or_central_secrets(self):
        for channel in ("stable", "alpha", "beta", "rc"):
            for missing in self.credentials:
                with self.subTest(channel=channel, missing=missing):
                    supplied = {key: value for key, value in self.credentials.items() if key != missing}
                    result = self.check_credentials(channel, supplied)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(missing, result.stderr)
                    for value in supplied.values():
                        self.assertNotIn(value, result.stdout + result.stderr)

    def test_all_java_channels_accept_complete_credentials(self):
        for channel in ("stable", "alpha", "beta", "rc"):
            with self.subTest(channel=channel):
                result = self.check_credentials(channel, self.credentials)
                self.assertEqual(result.returncode, 0, result.stderr)

    def deployment(self, channel):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary).resolve()
            (root / "sdk-java").mkdir()
            binary = root / "bin"
            binary.mkdir()
            # Maven is the external publishing boundary. Capture exactly what
            # the real workflow would ask it to do instead of uploading a package.
            executable = binary / "mvn"
            executable.write_text("#!" + sys.executable + "\n" + textwrap.dedent("""\
                import json, os, pathlib, sys
                pathlib.Path(os.environ["MVN_TRACE"]).write_text(json.dumps({
                    "args": sys.argv[1:], "cwd": str(pathlib.Path.cwd())
                }))
                """))
            executable.chmod(0o755)
            trace = root / "maven-call.json"
            result = subprocess.run(
                ["bash", "-eu", "-o", "pipefail", "-c",
                 workflow_script("Publish Java package and wait for Central publication")],
                cwd=root, env={"PATH": str(binary) + os.pathsep + os.environ["PATH"],
                               "CHANNEL": channel, "SOURCE_DATE_EPOCH": "1700000000",
                               "MVN_TRACE": str(trace)},
                text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            invocation = json.loads(trace.read_text())
            self.assertEqual(pathlib.Path(invocation["cwd"]), root / "sdk-java")
            self.assertIn("deploy", invocation["args"])
            return invocation["args"]

    def test_all_java_channels_deploy_signed_artifacts_only_to_central(self):
        for channel in ("stable", "alpha", "beta", "rc"):
            with self.subTest(channel=channel):
                arguments = self.deployment(channel)
                self.assertIn("-Prelease,central", arguments)
                self.assertNotIn("-Dgpg.skip=true", arguments)
                self.assertFalse(any(arg.startswith("-DaltDeploymentRepository=") for arg in arguments))


if __name__ == "__main__":
    unittest.main()
