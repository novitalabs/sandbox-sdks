#!/usr/bin/env python3
"""Scan tracked content and distributable archives without printing sensitive values."""
import io
import pathlib
import re
import subprocess
import sys
import tarfile
import zipfile

RULES = [
    ("personal-path", re.compile(r"/(?:Users|home)/[A-Za-z][A-Za-z0-9_.-]*/(?:Documents/|work/|projects?/|Desktop/)", re.I)),
    ("github-token", re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,})")),
    ("aws-access-key", re.compile(r"\b(?:AKIA|ASIA)[A-Z0-9]{16}\b")),
    ("package-token", re.compile(r"\b(?:npm_[A-Za-z0-9]{30,}|pypi-[A-Za-z0-9_-]{40,})")),
    ("api-key", re.compile(r"\bsk[-_][A-Za-z0-9_-]{24,}")),
    ("private-key", re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH |DSA |ENCRYPTED )?PRIVATE KEY-----\s+[A-Za-z0-9+/=\r\n]{40,}")),
    ("jwt", re.compile(r"\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}")),
    ("chat-token", re.compile(r"\bxox[baprs]-[A-Za-z0-9-]{20,}")),
]
ENCODINGS = ("utf-8", "utf-16-le", "utf-16-be", "latin-1")
MAX_BYTES = 64 * 1024 * 1024


def candidates(root):
    result = subprocess.run(["git", "-C", str(root), "rev-parse", "--show-toplevel"], capture_output=True, text=True)
    if result.returncode == 0 and pathlib.Path(result.stdout.strip()).resolve() == root.resolve():
        result = subprocess.run(["git", "-C", str(root), "ls-files", "-z", "--cached", "--others", "--exclude-standard"], capture_output=True, check=True)
        return [root / name.decode() for name in set(result.stdout.split(b"\0")) if name]
    return [p for p in root.rglob("*") if p.is_file() and ".git" not in p.relative_to(root).parts]


def inspect_bytes(data, label, findings, depth=0):
    if len(data) > MAX_BYTES:
        findings.add((label, 0, "oversize-file"))
        return
    for encoding in ENCODINGS:
        try:
            text = data.decode(encoding)
        except UnicodeError:
            continue
        for rule, pattern in RULES:
            for match in pattern.finditer(text):
                findings.add((label, text.count("\n", 0, match.start()) + 1, rule))
    if depth >= 3:
        if data.startswith((b"PK\x03\x04", b"\x1f\x8b")):
            findings.add((label, 0, "archive-depth-exceeded"))
        return
    # Inspect release archives in memory: never extract untrusted archive paths.
    try:
        if data.startswith(b"PK\x03\x04"):
            with zipfile.ZipFile(io.BytesIO(data)) as archive:
                total = sum(entry.file_size for entry in archive.infolist())
                if total > MAX_BYTES:
                    raise ValueError("archive too large")
                for entry in archive.infolist():
                    if not entry.is_dir():
                        inspect_bytes(archive.read(entry), label + "!" + entry.filename, findings, depth + 1)
        elif data.startswith(b"\x1f\x8b"):
            with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as archive:
                total = 0
                for entry in archive:
                    if entry.issym() or entry.islnk():
                        findings.add((label + "!" + entry.name, 0, "archive-link"))
                    if entry.isfile():
                        total += entry.size
                        if total > MAX_BYTES:
                            raise ValueError("archive too large")
                        inspect_bytes(archive.extractfile(entry).read(), label + "!" + entry.name, findings, depth + 1)
    except (OSError, ValueError, tarfile.TarError, zipfile.BadZipFile, RuntimeError):
        findings.add((label, 0, "unreadable-archive"))


def scan(root):
    root = pathlib.Path(root).resolve()
    findings = set()
    paths = candidates(root)
    for path in sorted(paths):
        label = path.relative_to(root).as_posix()
        if path.name in {".env", ".npmrc", ".pypirc", "id_rsa", "id_ed25519", "credentials.json"}:
            findings.add((label, 0, "credential-file"))
        inspect_bytes(label.encode(), label, findings)
        if path.is_symlink():
            findings.add((label, 0, "symlink"))
            continue
        if not path.exists():  # staged deletions have no working-tree content
            continue
        try:
            if path.stat().st_size > MAX_BYTES:
                findings.add((label, 0, "oversize-file"))
            else:
                inspect_bytes(path.read_bytes(), label, findings)
        except OSError:
            findings.add((label, 0, "unreadable-file"))
    return len(paths), sorted(findings)


def main():
    count, findings = scan(sys.argv[1] if len(sys.argv) > 1 else ".")
    for label, line, rule in findings:
        print(f"{label}:{line}: {rule} (value redacted)")
    print(f"Scanned {count} files; {len(findings)} findings")
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main())
