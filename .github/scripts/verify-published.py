#!/usr/bin/env python3
"""Confirm registry metadata and bytes before announcing a GitHub Release."""
import base64
import hashlib
import importlib.util
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import time
import urllib.request
import urllib.error
spec = importlib.util.spec_from_file_location("release", pathlib.Path(__file__).with_name("release.py"))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


def verify(info, assets):
    component, version = info["component"], info["version"]
    headers = {"User-Agent": "sandbox-sdk-release-check"}
    def read(url):
        with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=30) as response:
            return response.read()
    base = "https://repo.maven.apache.org/maven2"
    files = [assets / f"novita-sandbox-{version}{suffix}{signature}"
             for suffix in (".jar", "-sources.jar", "-javadoc.jar", ".pom")
             for signature in ("", ".asc")]
    for file in files:
        if not file.is_file():
            raise ValueError("Missing signed Java release artifact: " + file.name)
    for file in files:
        published = read(base + f"/ai/novita/novita-sandbox/{version}/" + file.name)
        if hashlib.sha256(published).digest() != hashlib.sha256(file.read_bytes()).digest():
            raise ValueError("Maven artifact differs from the validated build")


def verify_go(info):
    visibility = os.environ.get("REPOSITORY_PRIVATE")
    if visibility not in ("true", "false"):
        raise ValueError("Set REPOSITORY_PRIVATE explicitly to true or false")
    private = visibility == "true"
    module, version = info["package"], "v" + info["version"]
    # Ignore caller Git overrides, tracing, and persisted Go configuration. No
    # credential or Go settings are written to the checkout or global config.
    env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
    for key in ("GH_DEBUG", "GITHUB_TOKEN", "SSH_ASKPASS"):
        env.pop(key, None)
    env.update(GOENV="off", GOWORK="off", GO111MODULE="on", GOFLAGS="-modcacherw",
               GOPROXY="https://proxy.golang.org,direct", GOSUMDB="sum.golang.org",
               GOPRIVATE="", GONOPROXY="", GONOSUMDB="",
               GIT_TERMINAL_PROMPT="0", GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1")
    if private:
        repository = os.environ.get("GITHUB_REPOSITORY", "")
        prefix = "github.com/" + repository
        if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository) or not module.startswith(prefix + "/"):
            raise ValueError("Private Go module must belong to GITHUB_REPOSITORY")
        if not env.get("GH_TOKEN", "").strip():
            raise ValueError("Private Go verification requires GH_TOKEN with repository read access")
        env.update(GOPRIVATE=prefix, GONOPROXY=prefix, GONOSUMDB=prefix, GH_HOST="github.com",
                   GIT_CONFIG_COUNT="2", GIT_CONFIG_KEY_0="credential.https://github.com.helper",
                   GIT_CONFIG_VALUE_0="", GIT_CONFIG_KEY_1="credential.https://github.com.helper",
                   GIT_CONFIG_VALUE_1="!gh auth git-credential")
    else:
        env.pop("GH_TOKEN", None)
    with tempfile.TemporaryDirectory(prefix="verify-go-release-") as tmp:
        root = pathlib.Path(tmp)
        cwd, cache = root / "consumer", root / "module-cache"
        cwd.mkdir()
        env["GOMODCACHE"] = str(cache)
        result = subprocess.run(["go", "mod", "download", "-json", module + "@" + version],
                                cwd=cwd, env=env, capture_output=True, text=True)
        # Never echo tool output: an authentication failure may include remote
        # diagnostics. Success requires the actual downloaded module and source.
        if result.returncode:
            raise ValueError("Go module download failed; check repository access and tag availability")
        try:
            metadata = json.loads(result.stdout)
            if (not isinstance(metadata, dict) or metadata.get("Path") != module
                    or metadata.get("Version") != version or "Replace" in metadata or metadata.get("Error")):
                raise ValueError("Go module metadata differs from tag")
            directory = pathlib.Path(metadata["Dir"]).resolve()
            if not directory.is_relative_to(cache.resolve()):
                raise ValueError("Go module was not downloaded into the fresh cache")
            source = (directory / "core/connection_config.go").read_text()
            sdk_version = re.search(r'^\s*(?:const\s+)?SDKVersion\s*=\s*"([^"]+)"', source, re.M)
            if sdk_version is None or sdk_version[1] != info["version"]:
                raise ValueError("Downloaded Go SDKVersion differs from tag")
        except (OSError, KeyError, TypeError, json.JSONDecodeError):
            raise ValueError("Downloaded Go module could not be verified") from None
    print(f"Verified {'private' if private else 'public'} Go module {module}@{version} from a fresh cache")


def main():
    info = release.describe(sys.argv[1])
    if info["component"] == "go":
        verify_go(info)
        return
    error_name = "unavailable"
    for attempt in range(24):
        try:
            verify(info, pathlib.Path("release-assets"))
            print(f"Verified registry artifacts for {info['package']} {info['version']}")
            return
        except (urllib.error.HTTPError, urllib.error.URLError, ValueError, KeyError, TimeoutError) as error:
            error_name = type(error).__name__
        if attempt < 23:
            time.sleep(5)
    raise SystemExit(f"Registry artifact verification failed ({error_name}); GitHub Release remains unannounced")


if __name__ == "__main__":
    main()
