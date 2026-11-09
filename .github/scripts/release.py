#!/usr/bin/env python3
"""Validate component releases before tagging and again before publishing."""
import argparse
import json
import os
import pathlib
import re
import subprocess
import sys
import tarfile
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
import zipfile

COMPONENTS = {
    "sdk-go/v": ("go", "sdk-go", "github.com/novitalabs/sandbox-sdks/sdk-go/v2"),
    "java-": ("java", "sdk-java", "ai.novita:novita-sandbox"),
}

NUMBER = r"(?:0|[1-9][0-9]*)"
BASE = rf"{NUMBER}\.{NUMBER}\.{NUMBER}"


def describe(tag):
    for prefix, (component, directory, package) in COMPONENTS.items():
        if tag.startswith(prefix):
            version = tag[len(prefix):]
            match = re.fullmatch(rf"({BASE})(?:-(alpha|beta|rc)(?:\.({NUMBER})|([1-9][0-9]*|0)))?", version)
            channel = (match[2] or "stable") if match else None
            if channel is None:
                raise ValueError("Invalid version: use SemVer for Go and Java")
            if component == "go" and version.split(".")[0] != "2":
                raise ValueError("Go tag major must match the /v2 module")
            return dict(tag=tag, prefix=prefix, component=component, directory=directory,
                        package=package, version=version, channel=channel,
                        branch={"stable": "main", "alpha": "alpha", "beta": "beta", "rc": "alpha"}[channel],
                        dist_tag="latest" if channel == "stable" else channel)
    raise ValueError("Unknown component tag prefix")


def git(*args):
    return subprocess.check_output(["git", *args], text=True, stderr=subprocess.PIPE).strip()


def read_at(commit, path):
    return git("show", f"{commit}:{path}")


def check(tag, commit="HEAD", remote=None, branch_ref=None):
    info = describe(tag)
    commit = git("rev-parse", "--verify", "--end-of-options", f"{commit}^{{commit}}")
    ref = branch_ref or (f"refs/remotes/{remote}/{info['branch']}" if remote else f"refs/heads/{info['branch']}")
    # Resolve the ref explicitly; a missing branch must not be treated as a pass.
    git("rev-parse", "--verify", ref)
    result = subprocess.run(["git", "merge-base", "--is-ancestor", commit, ref], capture_output=True)
    if result.returncode != 0:
        raise ValueError(f"{tag} must belong to {info['branch']}")
    directory = info["directory"]
    if info["component"] == "java":
        root = ET.fromstring(read_at(commit, f"{directory}/pom.xml"))
        ns = {"m": "http://maven.apache.org/POM/4.0.0"}
        actual = root.findtext("m:version", namespaces=ns)
        name = root.findtext("m:groupId", namespaces=ns) + ":" + root.findtext("m:artifactId", namespaces=ns)
    else:
        name = re.search(r"^module\s+(\S+)", read_at(commit, "sdk-go/go.mod"), re.M)[1]
        actual = re.search(r'SDKVersion\s*=\s*"([^"]+)"', read_at(commit, "sdk-go/core/connection_config.go"))[1]
    if name != info["package"] or actual != info["version"]:
        raise ValueError(f"Tag does not match package metadata: expected {info['package']} {info['version']}, found {name} {actual}")
    info["commit"] = commit
    return info


def request_json(url, headers=None):
    req = urllib.request.Request(url, headers={"User-Agent": "sandbox-sdk-release-check", **(headers or {})})
    with urllib.request.urlopen(req, timeout=30) as response:
        return json.load(response)


def check_registry(info):
    component, version, name = info["component"], info["version"], info["package"]
    if component != "java":
        return  # Go is published by its immutable module tag.
    url = f"https://repo.maven.apache.org/maven2/ai/novita/novita-sandbox/{version}/novita-sandbox-{version}.pom"
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "sandbox-sdk-release-check"})
        with urllib.request.urlopen(req, timeout=30):
            pass
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return
        raise ValueError(f"Registry check failed with HTTP {error.code}") from error
    except urllib.error.URLError as error:
        raise ValueError("Registry unavailable; refusing to assume the version is free") from error
    raise ValueError(f"{name} {version} is already published; choose a new version")


def verify_artifacts(info, directory):
    directory = pathlib.Path(directory)
    component, version, package = info["component"], info["version"], info["package"]
    if component == "java":
        for suffix in (".jar", "-sources.jar", "-javadoc.jar"):
            if not (directory / f"novita-sandbox-{version}{suffix}").is_file():
                raise ValueError("Missing Java release artifact: " + suffix)
        for suffix in (".jar", "-sources.jar", "-javadoc.jar"):
            with zipfile.ZipFile(directory / f"novita-sandbox-{version}{suffix}") as archive:
                try:
                    license_path = "doc-files/LICENSE" if suffix == "-javadoc.jar" else "META-INF/LICENSE"
                    license_text = archive.read(license_path).decode()
                except KeyError:
                    raise ValueError("Java archive is missing its license: " + suffix) from None
                if "MIT License" not in license_text or "Copyright" not in license_text or "Permission is hereby granted" not in license_text:
                    raise ValueError("Java archive has an invalid license: " + suffix)
        with zipfile.ZipFile(directory / f"novita-sandbox-{version}.jar") as archive:
            metadata = archive.read("META-INF/maven/ai.novita/novita-sandbox/pom.properties").decode()
            values = dict(re.findall(r"^(groupId|artifactId|version)=(.*)$", metadata, re.M))
            if (values.get("groupId"), values.get("artifactId"), values.get("version")) != ("ai.novita", "novita-sandbox", version):
                raise ValueError("JAR Maven coordinates differ from tag")
    else:
        path = directory / f"novita-sandbox-go-{version}.tar.gz"
        if not path.is_file():
            raise ValueError("Missing Go source archive")
        with tarfile.open(path) as archive:
            members = {m.name.removeprefix("./"): m for m in archive.getmembers()}
            module = archive.extractfile(members["go.mod"]).read().decode()
            source = archive.extractfile(members["core/connection_config.go"]).read().decode()
            if not re.search(r"^module\s+" + re.escape(package) + r"\s*$", module, re.M):
                raise ValueError("Go archive module differs from tag")
            if not re.search(r'SDKVersion\s*=\s*"' + re.escape(version) + '"', source):
                raise ValueError("Go archive SDKVersion differs from tag")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("describe", "check", "artifacts"))
    parser.add_argument("tag")
    parser.add_argument("--commit", default="HEAD")
    parser.add_argument("--remote")
    parser.add_argument("--check-registry", action="store_true")
    parser.add_argument("--directory")
    parser.add_argument("--github-env", action="store_true")
    args = parser.parse_args()
    try:
        info = describe(args.tag) if args.command in ("describe", "artifacts") else check(args.tag, args.commit, args.remote)
        if args.check_registry:
            check_registry(info)
        if args.command == "artifacts":
            if not args.directory:
                raise ValueError("--directory is required")
            verify_artifacts(info, args.directory)
        if args.github_env:
            with open(os.environ["GITHUB_ENV"], "a") as output:
                for key in ("tag", "component", "directory", "package", "version", "channel", "branch", "dist_tag"):
                    output.write(f"{key.upper()}={info[key]}\n")
        print(json.dumps(info))
        return 0
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError, ET.ParseError) as error:
        print(f"Release validation failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
