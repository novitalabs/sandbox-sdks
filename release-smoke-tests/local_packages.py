#!/usr/bin/env python3
"""Build this checkout and consume its Go module / Maven JAR from clean projects."""

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import xml.etree.ElementTree as ET
import zipfile


ROOT = Path(__file__).resolve().parents[1]
GO_MODULE = "github.com/novitalabs/sandbox-sdks/sdk-go"


def local_environment(live=False):
    injected_options = {"MAVEN_ARGS", "MAVEN_OPTS", "MAVEN_CONFIG", "MAVEN_DEBUG_OPTS",
                        "JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS", "_JAVA_OPTIONS"}
    env = {key: value for key, value in os.environ.items()
           if key not in injected_options and (live or not key.startswith("NOVITA_"))}
    env.update(MAVEN_SKIP_RC="1", GOENV="off", GOFLAGS="")
    return env


def run(args, cwd, env=None):
    print("+ " + " ".join(str(arg) for arg in args), flush=True)
    subprocess.run([str(arg) for arg in args], cwd=cwd,
                   env=local_environment() if env is None else env, check=True)


def go_package(out):
    source = ROOT / "sdk-go"
    version = re.search(r'SDKVersion\s*=\s*"([^"]+)"',
                        (source / "core/connection_config.go").read_text())[1]
    module_version = "v" + version
    proxy = out / "go-proxy"
    versions = proxy / GO_MODULE / "@v"
    versions.mkdir(parents=True)
    (versions / "list").write_text(module_version + "\n")
    (versions / (module_version + ".mod")).write_bytes((source / "go.mod").read_bytes())
    (versions / (module_version + ".info")).write_text(json.dumps({
        "Version": module_version,
        "Time": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }) + "\n")
    # Git's ignore rules exclude local credentials/build output, while --others
    # includes the fixes being tested before they have been committed.
    paths = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "sdk-go"],
        cwd=ROOT,
    ).decode().split("\0")
    prefix = GO_MODULE + "@" + module_version + "/"
    with zipfile.ZipFile(versions / (module_version + ".zip"), "w", zipfile.ZIP_DEFLATED) as archive:
        for item in sorted(set(paths)):
            if not item:
                continue
            path = ROOT / item
            if path.is_file() and not path.is_symlink():
                archive.write(path, prefix + path.relative_to(source).as_posix())
        if not (source / "LICENSE").exists():
            archive.write(ROOT / "LICENSE", prefix + "LICENSE")
    consumer = out / "go-consumer"
    shutil.copytree(ROOT / "release-smoke-tests/go", consumer)
    # Use the local SDK archive and verify checksums for its public dependencies.
    env = dict(local_environment(), GOWORK="off", GOPROXY=proxy.as_uri() + ",https://proxy.golang.org",
               GOSUMDB="sum.golang.org", GOPRIVATE="", GONOPROXY="", GONOSUMDB=GO_MODULE, GOFLAGS="",
               GOTOOLCHAIN="local", GOMODCACHE=str(out / "go-modules"), GOCACHE=str(out / "go-build-cache"),
               NOVITA_EXPECTED_VERSION=version, NOVITA_RUN_LIVE="0")
    # Changing require in this isolated consumer is intentional; no replace is used.
    run(["go", "mod", "edit", "-require=" + GO_MODULE + "@" + module_version], consumer, env)
    run(["go", "mod", "download", GO_MODULE], consumer, env)
    origin = json.loads(subprocess.check_output(
        ["go", "list", "-m", "-json", GO_MODULE], cwd=consumer, env=env))
    if origin.get("Replace") or Path(origin["Dir"]).resolve().parent == source.parent:
        raise RuntimeError("consumer must resolve a module archive, not checkout sources")
    if not Path(origin["Dir"]).resolve().is_relative_to(out / "go-modules"):
        raise RuntimeError("consumer module did not come from the isolated module cache")
    (out / "go-package-origin.json").write_text(json.dumps(origin, indent=2) + "\n")
    run(["go", "mod", "tidy"], consumer, env)
    run(["go", "test", "-count=1", "-timeout=90s", "-v", "./..."], consumer, env)
    # Save the non-secret environment needed to reuse this consumer for a live check.
    (out / "go-environment.json").write_text(json.dumps({key: env[key] for key in (
        "GOWORK", "GOPROXY", "GOSUMDB", "GOPRIVATE", "GONOPROXY", "GONOSUMDB",
        "GOFLAGS", "GOTOOLCHAIN", "GOMODCACHE", "GOCACHE", "NOVITA_EXPECTED_VERSION")}, indent=2) + "\n")
    return {"module": GO_MODULE, "version": module_version, "consumer": str(consumer)}


def java_package(out, cache, offline):
    source = out / "java-sdk-build"
    shutil.copytree(ROOT / "sdk-java", source, ignore=shutil.ignore_patterns("target", ".DS_Store"))
    version = ET.parse(source / "pom.xml").getroot().find(
        "{http://maven.apache.org/POM/4.0.0}version").text
    repository = out / "m2"
    if cache:
        cache = cache.expanduser().resolve()
        if not cache.is_dir():
            raise ValueError("--maven-cache must name a Maven artifact repository directory")
        shutil.copytree(cache, repository, ignore=shutil.ignore_patterns(
            "settings.xml", "*.lastUpdated", "_remote.repositories"))
        # The SDK under test must be installed by this run, never copied from a cache.
        shutil.rmtree(repository / "ai/novita/novita-sandbox", ignore_errors=True)
    else:
        repository.mkdir()
    settings = out / "maven-settings.xml"
    settings.write_text('<settings xmlns="http://maven.apache.org/SETTINGS/1.0.0"/>\n')
    mvn = ["mvn", "-B", "--no-transfer-progress", "-s", settings, "-gs", settings,
           "-Dmaven.repo.local=" + str(repository)]
    if offline:
        mvn.append("--offline")
    run(mvn + ["-DskipTests", "package",
               "org.apache.maven.plugins:maven-install-plugin:3.1.4:install"], source)
    consumer = out / "java-consumer"
    shutil.copytree(ROOT / "release-smoke-tests/java", consumer)
    run(mvn + ["-Dnovita.version=" + version, "test"], consumer,
        dict(local_environment(), NOVITA_RUN_LIVE="0"))
    return {"artifact": "ai.novita:novita-sandbox", "version": version,
            "consumer": str(consumer), "repository": str(repository)}


def live_packages(out, language):
    if not os.environ.get("NOVITA_API_KEY"):
        raise ValueError("Set NOVITA_API_KEY in your environment before --run-live")
    result = json.loads((out / "result.json").read_text())
    env = dict(local_environment(live=True), NOVITA_RUN_LIVE="1")
    selected = ("go", "java") if language == "all" else (language,)
    for name in selected:
        if name not in result:
            raise ValueError("No built " + name + " consumer in " + str(out))
        consumer = result[name]
        if name == "go":
            env.update(json.loads((out / "go-environment.json").read_text()))
            run(["go", "test", "-count=1", "-timeout=240s", "-v", "-run", "^TestLiveCustomerWorkflow$"],
                consumer["consumer"], env)
        else:
            run(["mvn", "-B", "--no-transfer-progress", "--offline", "-s", out / "maven-settings.xml",
                 "-gs", out / "maven-settings.xml",
                 "-Dmaven.repo.local=" + consumer["repository"], "-Dnovita.version=" + consumer["version"],
                 "-Dtest=LiveCustomerTest", "test"], consumer["consumer"], env)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--language", choices=["go", "java", "all"], default="all")
    parser.add_argument("--output", type=Path, help="New/empty directory outside this checkout")
    parser.add_argument("--maven-cache", type=Path, help="Optional artifact cache copied into isolated Maven repo")
    parser.add_argument("--offline", action="store_true", help="Maven must use cached dependencies only")
    parser.add_argument("--run-live", type=Path, metavar="ARTIFACT_DIRECTORY",
                        help="Explicitly run real, billable sandbox tests using previously built packages")
    args = parser.parse_args()
    if args.run_live:
        if args.output or args.maven_cache:
            parser.error("--run-live reuses existing artifacts; omit --output and --maven-cache")
        live_packages(args.run_live.expanduser().resolve(), args.language)
        return
    out = args.output.expanduser().resolve() if args.output else Path(tempfile.mkdtemp(prefix="novita-consumer-")).resolve()
    if out == ROOT or ROOT in out.parents:
        parser.error("--output must be outside the repository")
    if out.exists() and any(out.iterdir()):
        parser.error("--output must be empty")
    out.mkdir(parents=True, exist_ok=True)
    result = {"output": str(out)}
    print("Consumer artifacts: " + str(out), flush=True)
    if args.language in ("go", "all"):
        result["go"] = go_package(out)
    if args.language in ("java", "all"):
        result["java"] = java_package(out, args.maven_cache, args.offline)
    (out / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
