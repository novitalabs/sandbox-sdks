import importlib.util
import os
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("local_packages", Path(__file__).with_name("local_packages.py"))
packages = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packages)


class LocalPackageIsolationTest(unittest.TestCase):
    def test_default_subprocess_does_not_inherit_authentication_or_injected_maven_flags(self):
        injected = {"NOVITA_ACCESS_TOKEN": "fixture-private-value", "NOVITA_API_KEY": "fixture-key",
                    "NOVITA_DEBUG": "1", "MAVEN_ARGS": "-Pcentral", "MAVEN_OPTS": "-Dfixture=private",
                    "JAVA_TOOL_OPTIONS": "-Dfixture=private", "JDK_JAVA_OPTIONS": "-Dfixture=private"}
        with patch.dict(os.environ, injected), patch.object(packages.subprocess, "run") as executed:
            packages.run(["mvn", "test"], Path("/tmp"))
            actual = executed.call_args.kwargs.get("env") or os.environ
            self.assertFalse(any(key.startswith("NOVITA_") for key in actual))
            for key in ("MAVEN_ARGS", "MAVEN_OPTS", "JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS"):
                self.assertNotIn(key, actual)
            self.assertEqual("1", actual.get("MAVEN_SKIP_RC"))
            self.assertIn("PATH", actual)

    def test_live_run_retains_explicit_key_but_cannot_replace_go_dependency_with_inherited_flags(self):
        with patch.dict(os.environ, {"NOVITA_API_KEY": "fixture-key", "GOFLAGS": "-modfile=alternate.mod"}):
            actual = packages.local_environment(live=True)
            self.assertEqual("fixture-key", actual["NOVITA_API_KEY"])
            self.assertEqual("", actual["GOFLAGS"])
            self.assertEqual("off", actual["GOENV"])


if __name__ == "__main__":
    unittest.main()
