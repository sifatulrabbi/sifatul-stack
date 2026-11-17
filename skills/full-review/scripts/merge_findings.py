#!/usr/bin/env python3
"""Merge specialist findings into verifier jobs, then assemble the round's final list.

Usage:
  merge_findings.py merge <round-dir> --review-tree <path> --worktree-base <path> [--batch-size 8]
  merge_findings.py final <round-dir>
"""

import argparse
import pathlib
import sys

SKILL_DIR = pathlib.Path(__file__).resolve().parent.parent
SEV = {"critical": 0, "high": 1, "medium": 2, "low": 3}
STATUSES = ("confirmed", "plausible", "refuted", "unproven", "duplicate")


class Malformed(Exception):
    pass


def read_md(path):
    text = path.read_text()
    if not text.startswith("---\n"):
        raise Malformed("missing frontmatter")
    end = text.find("\n---\n", 3)
    if end == -1:
        raise Malformed("frontmatter not closed")
    meta = {}
    for line in text[4:end].splitlines():
        key, sep, value = line.partition(":")
        if not sep or not key.strip():
            raise Malformed(f"bad frontmatter line {line!r}")
        meta[key.strip()] = value.strip()
    title, sections, current = "", {}, None
    for line in text[end + 5 :].splitlines():
        if line.startswith("# ") and not title:
            title = line[2:].strip()
        elif line.startswith("## "):
            current = line[3:].strip()
            sections[current] = []
        elif current:
            sections[current].append(line)
    return meta, title, {k: "\n".join(v).strip() for k, v in sections.items()}


def parse_lines(value):
    if not value:
        return [0, 0]
    start, _, end = value.partition("-")
    return [int(start), int(end or start)]


def load_findings(round_dir):
    findings, errors = [], []
    for job_dir in sorted(p for p in (round_dir / "findings").iterdir() if p.is_dir()):
        if not (job_dir / "_done.md").exists():
            errors.append(f"{job_dir.name}: no _done.md, the specialist has not finished")
        for path in sorted(job_dir.glob("*.md")):
            if path.name == "_done.md":
                continue
            rel = path.relative_to(round_dir)
            try:
                meta, title, sections = read_md(path)
            except Malformed as e:
                errors.append(f"{rel}: {e}")
                continue
            if meta.get("severity") not in SEV:
                errors.append(f"{rel}: severity must be one of {', '.join(SEV)}")
                continue
            if not title:
                errors.append(f"{rel}: no '# ' title")
                continue
            findings.append(
                {
                    "id": f"{job_dir.name}-{path.stem}",
                    "category": job_dir.name,
                    "path": path,
                    "file": meta.get("file", ""),
                    "lines": parse_lines(meta.get("lines", "")),
                    "severity": meta["severity"],
                    "title": title,
                    "sections": sections,
                }
            )
    return findings, errors


def fail(errors):
    for e in errors:
        print(f"error: {e}", file=sys.stderr)
    sys.exit(1)


def overlaps(a, b, slack=3):
    return a[0] <= b[1] + slack and b[0] <= a[1] + slack


def cluster(findings):
    ordered = sorted(findings, key=lambda f: (SEV[f["severity"]], f["file"], f["lines"][0]))
    clusters, folded = [], []
    for f in ordered:
        home = next(
            (c for c in clusters if c["file"] == f["file"] and overlaps(c["lines"], f["lines"])),
            None,
        )
        if home is None:
            clusters.append({"file": f["file"], "lines": list(f["lines"]), "members": [f]})
            continue
        primary = next(
            (k for k in home["members"] if k["category"] == f["category"] and overlaps(k["lines"], f["lines"])),
            None,
        )
        if primary:
            folded.append((f, primary))
            continue
        home["members"].append(f)
        home["lines"] = [min(home["lines"][0], f["lines"][0]), max(home["lines"][1], f["lines"][1])]
    clusters.sort(key=lambda c: (c["file"], c["lines"][0]))
    return clusters, folded


def batches(clusters, size):
    out, current = [], []
    for c in clusters:
        if current and sum(len(x["members"]) for x in current) + len(c["members"]) > size:
            out.append(current)
            current = []
        current.append(c)
    if current:
        out.append(current)
    return out


def location(file, lines):
    if not file:
        return "PR-wide"
    return f"{file}:{lines[0]}-{lines[1]}" if lines[0] != lines[1] else f"{file}:{lines[0]}"


def write_job(round_dir, name, clusters, review_tree, worktree_base):
    lines = [
        f"# Job: {name}",
        "",
        f"Role: {SKILL_DIR}/agents/verifier.md",
        f"Format: {SKILL_DIR}/references/findings-format.md",
        f"Severity: {SKILL_DIR}/references/severity-rubric.md",
        f"Brief: {round_dir}/brief.md",
        f"Code: {review_tree}",
        f"Scratch: {worktree_base}/{name}",
        f"Notes: {round_dir}/notes/{name}.md",
        f"Output: {round_dir}/verified/<id>.md, one file per finding",
        "",
        "## Findings",
        "",
        "Findings under one heading overlap: if two describe the same issue, keep the better one and mark the other duplicate.",
    ]
    for c in clusters:
        lines += ["", f"### {location(c['file'], c['lines'])}", ""]
        lines += [f"- {f['id']}: {f['path']}" for f in c["members"]]
    (round_dir / "jobs" / f"{name}.md").write_text("\n".join(lines) + "\n")


def merge(round_dir, review_tree, worktree_base, batch_size):
    findings, errors = load_findings(round_dir)
    if errors:
        fail(errors)
    clusters, folded = cluster(findings)
    planned = batches(clusters, batch_size)
    for n, group in enumerate(planned, 1):
        write_job(round_dir, f"verify-{n}", group, review_tree, worktree_base)
    print(f"{len(findings)} findings, {len(folded)} folded as duplicates, {len(clusters)} clusters, {len(planned)} verifier jobs")


def verdict_of(round_dir, f):
    path = round_dir / "verified" / f"{f['id']}.md"
    if not path.exists():
        return None
    meta, _, sections = read_md(path)
    if meta.get("status") not in STATUSES:
        raise Malformed(f"status must be one of {', '.join(STATUSES)}")
    evidence = sections.get("Evidence", "")
    return {
        "path": path,
        "status": meta.get("status", ""),
        "severity": meta.get("severity") if meta.get("severity") in SEV else f["severity"],
        "duplicate_of": meta.get("duplicate_of", ""),
        "reason": evidence.splitlines()[0] if evidence else "",
    }


def final(round_dir):
    findings, errors = load_findings(round_dir)
    if errors:
        fail(errors)
    clusters, folded = cluster(findings)
    verdicts = {}
    for c in clusters:
        for f in c["members"]:
            try:
                verdicts[f["id"]] = verdict_of(round_dir, f)
            except Malformed as e:
                errors.append(f"verified/{f['id']}.md: {e}")
    if errors:
        fail(errors)
    kept, dropped, unverified = [], [], []
    for c in clusters:
        for f in c["members"]:
            v = verdicts[f["id"]]
            if v and v["status"] in ("confirmed", "plausible"):
                kept.append((f, v))
            elif not v:
                unverified.append(f)
            else:
                reason = f"duplicate of {v['duplicate_of']}" if v["status"] == "duplicate" and v["duplicate_of"] else v["reason"]
                dropped.append((v["status"], f, reason))
    dropped += [("duplicate", f, f"folded into {primary['id']}") for f, primary in folded]
    kept.sort(key=lambda fv: (SEV[fv[1]["severity"]], fv[1]["status"] != "confirmed", fv[0]["file"], fv[0]["lines"][0]))
    out = [
        "---",
        f"kept: {len(kept)}",
        f"dropped: {len(dropped)}",
        f"unverified: {len(unverified)}",
        "---",
        "",
        "# Final",
        "",
        "## Kept",
        "",
        "| Severity | Status | Finding | Location | Files |",
        "| --- | --- | --- | --- | --- |",
    ]
    for f, v in kept:
        files = f"{f['path'].relative_to(round_dir)} · {v['path'].relative_to(round_dir)}"
        out.append(f"| {v['severity']} | {v['status']} | {f['title']} | {location(f['file'], f['lines'])} | {files} |")
    alternatives = [(f, f["sections"]["Alternative"]) for f, _ in kept if f["sections"].get("Alternative")]
    if alternatives:
        out += ["", "## Alternative directions"]
        for f, text in alternatives:
            out += ["", f"### {f['title']} ({f['id']})", "", text]
    out += [
        "",
        "## Dropped",
        "",
        "| Status | Finding | Location | Reason |",
        "| --- | --- | --- | --- |",
    ]
    for status, f, reason in dropped:
        out.append(f"| {status} | {f['title']} | {location(f['file'], f['lines'])} | {reason} |")
    out += [
        "",
        "## Unverified",
        "",
        "| Severity | Finding | Location | File |",
        "| --- | --- | --- | --- |",
    ]
    for f in sorted(unverified, key=lambda f: (SEV[f["severity"]], f["file"], f["lines"][0])):
        out.append(f"| {f['severity']} | {f['title']} | {location(f['file'], f['lines'])} | {f['path'].relative_to(round_dir)} |")
    (round_dir / "final.md").write_text("\n".join(out) + "\n")


def main():
    ap = argparse.ArgumentParser(description="Merge review findings and assemble the final list.")
    ap.add_argument("mode", choices=["merge", "final"])
    ap.add_argument("round_dir")
    ap.add_argument("--review-tree")
    ap.add_argument("--worktree-base")
    ap.add_argument("--batch-size", type=int, default=8)
    args = ap.parse_args()
    round_dir = pathlib.Path(args.round_dir).resolve()
    if args.mode == "merge":
        if not (args.review_tree and args.worktree_base):
            ap.error("merge needs --review-tree and --worktree-base")
        merge(round_dir, args.review_tree, args.worktree_base, args.batch_size)
    else:
        final(round_dir)


if __name__ == "__main__":
    main()
