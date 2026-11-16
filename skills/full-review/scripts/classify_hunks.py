#!/usr/bin/env python3
"""Label each diff hunk as mechanical or semantic so only real changes get reviewed.

Usage: classify_hunks.py --base <ref> [--head <ref>] [-o hunks.json]

Kinds: whitespace, comment, lockfile, generated, rename, docs, binary, semantic.
These are heuristics; the lead skims non-semantic hunks before excluding them.
"""

import argparse
import fnmatch
import json
import re
import subprocess
import sys

LOCKFILES = {
    "package-lock.json",
    "yarn.lock",
    "pnpm-lock.yaml",
    "poetry.lock",
    "Pipfile.lock",
    "uv.lock",
    "Cargo.lock",
    "go.sum",
    "composer.lock",
    "Gemfile.lock",
    "mix.lock",
    "pubspec.lock",
    "packages.lock.json",
    "bun.lockb",
}
GENERATED_GLOBS = [
    "*.min.js",
    "*.min.css",
    "*.map",
    "*_pb2.py",
    "*_pb2_grpc.py",
    "*.pb.go",
    "*.generated.*",
    "*.g.dart",
    "*.snap",
]
GENERATED_DIRS = [
    "dist/",
    "build/",
    "vendor/",
    "node_modules/",
    "__generated__/",
    "generated/",
    "__snapshots__/",
]
GENERATED_MARKER = re.compile(r"@generated|DO NOT EDIT|auto-?generated", re.I)
DOC_EXTS = (".md", ".mdx", ".rst", ".txt", ".adoc")
COMMENT = re.compile(r"^\s*(//|/\*|\*|\*/|#|--|<!--|-->)")
NOT_COMMENT = re.compile(
    r"^\s*#\s*(include|define|undef|if|ifdef|ifndef|elif|else|endif|pragma|import|!)"
)
HUNK = re.compile(r"^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@")


def git(*args):
    return subprocess.run(
        ["git", *args], capture_output=True, text=True, check=True
    ).stdout


def parse(diff):
    files, cur, hunk = [], None, None
    for line in diff.splitlines():
        if line.startswith("diff --git "):
            m = re.match(r"^diff --git a/(.+) b/(.+)$", line)
            cur = {
                "path": m.group(2) if m else line[11:],
                "status": "modified",
                "binary": False,
                "similarity": None,
                "hunks": [],
            }
            files.append(cur)
            hunk = None
        elif cur is None:
            continue
        elif line.startswith("@@"):
            m = HUNK.match(line)
            hunk = {
                "old": [int(m[1]), int(m[2] if m[2] is not None else 1)],
                "new": [int(m[3]), int(m[4] if m[4] is not None else 1)],
                "removed": [],
                "added": [],
            }
            cur["hunks"].append(hunk)
        elif hunk is None:  # file header
            if line.startswith("new file mode"):
                cur["status"] = "added"
            elif line.startswith("deleted file mode"):
                cur["status"] = "deleted"
            elif line.startswith("rename to "):
                cur["status"], cur["path"] = "renamed", line[10:]
            elif line.startswith("similarity index "):
                cur["similarity"] = int(line[17:].rstrip("%"))
            elif line.startswith("Binary files"):
                cur["binary"] = True
        elif line.startswith("+"):
            hunk["added"].append(line[1:])
        elif line.startswith("-"):
            hunk["removed"].append(line[1:])
    return files


def is_generated(path, head):
    name = path.rsplit("/", 1)[-1]
    if any(fnmatch.fnmatch(name, g) for g in GENERATED_GLOBS):
        return True
    if any(f"/{d}" in f"/{path}" for d in GENERATED_DIRS):
        return True
    try:
        top = "\n".join(git("show", f"{head}:{path}").splitlines()[:5])
        return bool(GENERATED_MARKER.search(top))
    except subprocess.CalledProcessError:
        return False


def file_kind(f, head):
    path = f["path"]
    if f["binary"]:
        return "binary"
    if path.rsplit("/", 1)[-1] in LOCKFILES:
        return "lockfile"
    if f["status"] == "renamed" and f["similarity"] == 100 and not f["hunks"]:
        return "rename"
    if path.lower().endswith(DOC_EXTS):
        return "docs"
    if f["status"] != "deleted" and is_generated(path, head):
        return "generated"
    return None


def is_comment(line):
    return bool(COMMENT.match(line)) and not NOT_COMMENT.match(line)


def hunk_kind(h):
    removed = [l for l in h["removed"] if l.strip()]
    added = [l for l in h["added"] if l.strip()]
    squash = lambda ls: sorted(re.sub(r"\s+", "", l) for l in ls)
    if squash(removed) == squash(added):
        return "whitespace"
    if all(is_comment(l) for l in removed + added):
        return "comment"
    return "semantic"


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--base", required=True)
    ap.add_argument("--head", default="HEAD")
    ap.add_argument("-o", "--output")
    args = ap.parse_args()

    diff = git(
        "diff",
        "--find-renames",
        "-U0",
        "--no-color",
        "--no-ext-diff",
        f"{args.base}...{args.head}",
    )
    out_files, by_kind, sem_lines = [], {}, 0
    for f in parse(diff):
        fk = file_kind(f, args.head)
        hunks = []
        for h in f["hunks"]:
            kind = fk or hunk_kind(h)
            changed = len(h["added"]) + len(h["removed"])
            hunks.append(
                {"old": h["old"], "new": h["new"], "kind": kind, "lines": changed}
            )
            by_kind[kind] = by_kind.get(kind, 0) + 1
            if kind == "semantic":
                sem_lines += changed
        if not hunks:  # pure renames, binaries, empty files
            by_kind[fk or "semantic"] = by_kind.get(fk or "semantic", 0) + 1
        kinds = {h["kind"] for h in hunks} or {fk or "semantic"}
        out_files.append(
            {
                "path": f["path"],
                "status": f["status"],
                "kind": kinds.pop() if len(kinds) == 1 else "mixed",
                "hunks": hunks,
            }
        )

    semantic_files = [
        f["path"]
        for f in out_files
        if f["kind"] in ("semantic", "mixed")
        and (any(h["kind"] == "semantic" for h in f["hunks"]) or not f["hunks"])
    ]
    result = {
        "base": args.base,
        "head": args.head,
        "summary": {
            "files": len(out_files),
            "semantic_files": len(semantic_files),
            "semantic_lines": sem_lines,
            "hunks_by_kind": by_kind,
        },
        "files": out_files,
    }
    text = json.dumps(result, indent=2)
    if args.output:
        with open(args.output, "w") as fh:
            fh.write(text)
    else:
        print(text)
    s = result["summary"]
    print(
        f"{s['files']} files, {s['semantic_files']} with semantic changes, "
        f"{s['semantic_lines']} semantic lines. Hunks by kind: {by_kind}",
        file=sys.stderr,
    )


if __name__ == "__main__":
    main()
