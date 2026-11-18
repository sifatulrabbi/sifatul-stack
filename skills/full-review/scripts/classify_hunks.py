#!/usr/bin/env python3
"""Label each diff hunk as mechanical or semantic so only real changes get reviewed.

Usage: classify_hunks.py --base <ref> [--head <ref>] [-o hunks.md]

Kinds: whitespace, comment, lockfile, generated, rename, docs, binary, semantic.
Instruction files agents read (skills, agent prompts, AGENTS.md, rule files) are never docs or comments; code under their directories still is code.
These are heuristics; the lead skims non-semantic hunks before excluding them.
"""

import argparse
import fnmatch
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
INSTRUCTION_NAMES = ["SKILL.md", "*AGENTS*.md", "CLAUDE*.md", "GEMINI*.md", "copilot-instructions.md", ".cursorrules", ".windsurfrules", ".clinerules"]
INSTRUCTION_DIRS = {"skills", "agents", "prompts", "instructions", "commands", ".claude", ".codex", ".opencode", ".agents", ".cursor", ".windsurf", ".gemini", ".clinerules"}
PROMPT_EXTS = DOC_EXTS + (".mdc", ".toml", ".yaml", ".yml")
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
                "old_path": None,
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
            elif line.startswith("rename from "):
                cur["old_path"] = line[12:]
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


def is_instructions(path):
    *dirs, name = path.split("/")
    if any(fnmatch.fnmatch(name, n) for n in INSTRUCTION_NAMES):
        return True
    prompt_like = name.lower().endswith(PROMPT_EXTS) or "." not in name.lstrip(".")
    return prompt_like and any(d in INSTRUCTION_DIRS for d in dirs)


def file_kind(f, head):
    path = f["path"]
    if f["binary"]:
        return "binary"
    if path.rsplit("/", 1)[-1] in LOCKFILES:
        return "lockfile"
    if f["status"] == "renamed" and f["similarity"] == 100 and not f["hunks"]:
        return "rename"
    if path.lower().endswith(DOC_EXTS) and not is_instructions(path):
        return "docs"
    if f["status"] != "deleted" and is_generated(path, head):
        return "generated"
    return None


def is_comment(line):
    return bool(COMMENT.match(line)) and not NOT_COMMENT.match(line)


def hunk_kind(h, prose):
    removed = [l for l in h["removed"] if l.strip()]
    added = [l for l in h["added"] if l.strip()]
    squash = lambda ls: sorted(re.sub(r"\s+", "", l) for l in ls)
    spaced = lambda ls: [re.sub(r"\s+", " ", l).strip() for l in ls]
    if (spaced(removed) == spaced(added)) if prose else (squash(removed) == squash(added)):
        return "whitespace"
    if not prose and all(is_comment(l) for l in removed + added):
        return "comment"
    return "semantic"


FILE_KINDS = ("lockfile", "generated", "docs", "binary", "rename")


def span(h):
    if h["new"][1] == 0:
        start, count = h["old"]
        where = f"removed {start}-{start + count - 1}" if count > 1 else f"removed {start}"
    else:
        start, count = h["new"]
        where = f"{start}-{start + count - 1}" if count > 1 else f"{start}"
    return f"{where} · {h['lines']}"


def spans(hunks):
    return ", ".join(span(h) for h in hunks)


def render(base, head, files, by_kind, semantic_files, sem_lines):
    out = ["---", f"base: {base}", f"head: {head}", f"files: {len(files)}"]
    out += [f"semantic_files: {semantic_files}", f"semantic_lines: {sem_lines}"]
    out += [f"{k}: {n}" for k, n in sorted(by_kind.items())]
    out += ["---", "", "# Hunks", "", "## Semantic", ""]
    out += ["| File | Status | Hunks (head lines · changed lines) |", "| --- | --- | --- |"]
    for f in files:
        sem = [h for h in f["hunks"] if h["kind"] == "semantic"]
        if not sem and (f["hunks"] or f["kind"] != "semantic"):
            continue
        status = f"renamed from {f['old_path']}" if f["status"] == "renamed" else f["status"]
        out.append(f"| {f['path']} | {status} | {spans(sem) or 'no content hunks'} |")
    out += ["", "## Mechanical (skim before excluding)"]
    kinds = sorted(({h["kind"] for f in files for h in f["hunks"]} | {f["kind"] for f in files}) - {"semantic", "mixed"})
    for kind in kinds:
        out += ["", f"### {kind}", ""]
        for f in files:
            if kind == "rename" and f["kind"] == "rename":
                out.append(f"- {f['old_path']} → {f['path']}")
            elif kind in FILE_KINDS and f["kind"] == kind:
                out.append(f"- {f['path']} · {sum(h['lines'] for h in f['hunks'])} lines")
            elif kind not in FILE_KINDS:
                own = [h for h in f["hunks"] if h["kind"] == kind]
                if own:
                    out.append(f"- {f['path']}: {spans(own)}")
    return "\n".join(out) + "\n"


def main():
    ap = argparse.ArgumentParser(description="Label each diff hunk as mechanical or semantic.")
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
            kind = fk or hunk_kind(h, prose=is_instructions(f["path"]))
            changed = len(h["added"]) + len(h["removed"])
            hunks.append({"old": h["old"], "new": h["new"], "kind": kind, "lines": changed})
            by_kind[kind] = by_kind.get(kind, 0) + 1
            if kind == "semantic":
                sem_lines += changed
        if not hunks:
            by_kind[fk or "semantic"] = by_kind.get(fk or "semantic", 0) + 1
        kinds = {h["kind"] for h in hunks} or {fk or "semantic"}
        out_files.append(
            {
                "path": f["path"],
                "old_path": f["old_path"],
                "status": f["status"],
                "kind": kinds.pop() if len(kinds) == 1 else "mixed",
                "hunks": hunks,
            }
        )

    semantic_files = sum(
        1
        for f in out_files
        if any(h["kind"] == "semantic" for h in f["hunks"]) or (not f["hunks"] and f["kind"] == "semantic")
    )
    text = render(args.base, args.head, out_files, by_kind, semantic_files, sem_lines)
    if args.output:
        with open(args.output, "w") as fh:
            fh.write(text)
    else:
        print(text, end="")
    print(
        f"{len(out_files)} files, {semantic_files} with semantic changes, "
        f"{sem_lines} semantic lines. Hunks by kind: {by_kind}",
        file=sys.stderr,
    )


if __name__ == "__main__":
    main()
