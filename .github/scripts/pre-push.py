#!/usr/bin/env python3
"""Validate release tags before they can reach a remote (especially Go modules)."""
import importlib.util
import os
import pathlib
import subprocess
import sys
import tarfile
import tempfile
import io
import shutil
spec = importlib.util.spec_from_file_location("release", pathlib.Path(__file__).with_name("release.py"))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)
try:
    remote = sys.argv[1]
    for line in sys.stdin:
        local_ref, local_sha, remote_ref, remote_sha = line.split()
        if not remote_ref.startswith("refs/tags/") or set(local_sha) == {"0"}:
            continue
        tag = remote_ref[len("refs/tags/"):]
        description = release.describe(tag)
        branch = "refs/heads/" + description["branch"]
        # Require branches to be published first: a same-push branch update may
        # fail independently of its tag unless the sender uses atomic push.
        result = subprocess.check_output(["git", "ls-remote", remote, branch], text=True).split()
        if len(result) != 2 or result[1] != branch:
            raise ValueError("Push the release branch before pushing its tags")
        branch_tip = result[0]
        subprocess.run(["git", "fetch", "--no-tags", "--no-write-fetch-head", remote, branch_tip], check=True)
        info = release.check(tag, local_sha, branch_ref=branch_tip)
        if set(remote_sha) != {"0"}:
            raise ValueError("Release tags are immutable; refusing to replace a remote tag")
        release.check_registry(info)
        # Validate the exact tagged tree, never the unrelated current working tree.
        with tempfile.TemporaryDirectory(prefix="release-prepush-") as tmp:
            data = subprocess.check_output(["git", "archive", info["commit"]])
            with tarfile.open(fileobj=io.BytesIO(data)) as archive:
                for member in archive.getmembers():
                    if member.issym() or member.islnk() or member.name.startswith("/") or ".." in pathlib.PurePosixPath(member.name).parts:
                        raise ValueError("Unsafe file in tagged tree")
                archive.extractall(tmp, filter="data")
            subprocess.run([sys.executable, str(pathlib.Path(__file__).with_name("hygiene_scan.py")), tmp], check=True)
            local_scanner = pathlib.Path(release.git("rev-parse", "--git-path", "release-tools/gitleaks"))
            scanner = str(local_scanner.resolve()) if local_scanner.is_file() else shutil.which("gitleaks")
            if scanner is None:
                raise ValueError("Run bash .github/scripts/install-gitleaks.sh before pushing release tags")
            if subprocess.check_output([scanner, "version"], text=True).strip().lstrip("v") != "8.30.1":
                raise ValueError("Use Gitleaks 8.30.1, matching CI")
            subprocess.run([scanner, "dir", tmp, "--redact", "--max-archive-depth", "3", "--config", str(pathlib.Path(tmp)/".gitleaks.toml")], check=True)
            if info["component"] == "go":
                subprocess.run(["go", "test", "./..."], cwd=pathlib.Path(tmp)/"sdk-go", check=True)
        print(f"Validated {tag} before push")
except (ValueError, OSError, subprocess.CalledProcessError) as error:
    print(f"Push rejected: {error}", file=sys.stderr)
    sys.exit(1)
