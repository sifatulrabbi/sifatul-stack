#!/usr/bin/env python3
"""Turn a round's findings into verifier jobs, its final list, and fixer jobs, then report on the run.

Usage:
  merge_findings.py merge <round-dir> --review-tree <path> --worktree-base <path> [--batch-size 8]
  merge_findings.py final <round-dir>
  merge_findings.py fix <round-dir> --review-tree <path> --fixes-ref <ref> [--min-severity high]
  merge_findings.py report <round-dir> [<later-round-dir> ...]
"""

import argparse
import pathlib
import re
import sys

SKILL_DIR = pathlib.Path(__file__).resolve().parent.parent
SEV = {"critical": 0, "high": 1, "medium": 2, "low": 3}
STATUSES = ("confirmed", "plausible", "refuted", "unproven", "duplicate", "escalate")
KEPT = ("confirmed", "plausible")
KINDS = ("behavior", "scenario", "test-gap", "design", "instructions")
RUNNABLE = ("behavior", "test-gap")
PROOFS = ("test", "run", "fact", "trace")
UNKINDED = "related-prs"


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
    try:
        return [int(start), int(end or start)]
    except ValueError:
        raise Malformed("lines must be start-end or a single line") from None


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
                lines = parse_lines(meta.get("lines", ""))
            except Malformed as e:
                errors.append(f"{rel}: {e}")
                continue
            if meta.get("severity") not in SEV:
                errors.append(f"{rel}: severity must be one of {', '.join(SEV)}")
                continue
            if job_dir.name != UNKINDED and meta.get("kind") not in KINDS:
                errors.append(f"{rel}: kind must be one of {', '.join(KINDS)}")
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
                    "lines": lines,
                    "severity": meta["severity"],
                    "title": title,
                    "sections": sections,
                    "kind": meta.get("kind", ""),
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
    clusters = []
    for f in ordered:
        home = next(
            (c for c in clusters if c["file"] == f["file"] and overlaps(c["lines"], f["lines"])),
            None,
        )
        if home is None:
            clusters.append({"file": f["file"], "lines": list(f["lines"]), "members": [f]})
            continue
        home["members"].append(f)
        home["lines"] = [min(home["lines"][0], f["lines"][0]), max(home["lines"][1], f["lines"][1])]
    clusters.sort(key=lambda c: (c["file"], c["lines"][0]))
    return clusters


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


def escalated(round_dir, f):
    path = round_dir / "verified" / f"{f['id']}.md"
    try:
        return path.exists() and read_md(path)[0].get("status") == "escalate"
    except Malformed:
        return False


def agent_for(cluster):
    runnable = all(f["kind"] in RUNNABLE and not f["escalated"] for f in cluster["members"])
    return "junior" if runnable else "senior"


def write_verifier_job(round_dir, name, agent, clusters, review_tree, worktree_base):
    role = "verifier-junior.md" if agent == "junior" else "verifier.md"
    lines = [
        f"# Job: {name}",
        "",
        f"Agent: {agent}",
        f"Role: {SKILL_DIR}/agents/{role}",
        f"Protocol: {SKILL_DIR}/references/verify-protocol.md",
        f"Format: {SKILL_DIR}/references/findings-format.md",
        f"Severity: {SKILL_DIR}/references/severity-rubric.md",
        f"Brief: {round_dir}/brief.md",
        f"Code: {review_tree}",
        f"Scratch: {worktree_base}/{name}",
        f"Notes: {round_dir}/notes/{name}.md",
        f"Output: {round_dir}/verified/<id>.md, one file per finding",
        "",
        "## Findings",
    ]
    for c in clusters:
        lines += ["", f"### {location(c['file'], c['lines'])}", ""]
        for f in c["members"]:
            earlier = f", escalated from {round_dir}/verified/{f['id']}.md" if f["escalated"] else ""
            lines.append(f"- {f['id']}: {f['path']}{earlier}")
    (round_dir / "jobs" / f"{name}.md").write_text("\n".join(lines) + "\n")


def merge(round_dir, review_tree, worktree_base, batch_size):
    findings, errors = load_findings(round_dir)
    if errors:
        fail(errors)
    for f in findings:
        f["escalated"] = escalated(round_dir, f)
    pending = [f for f in findings if f["escalated"] or not (round_dir / "verified" / f"{f['id']}.md").exists()]
    clusters = cluster(pending)
    planned = [
        (agent, group)
        for agent in ("junior", "senior")
        for group in batches([c for c in clusters if agent_for(c) == agent], batch_size)
    ]
    jobs_dir = round_dir / "jobs"
    jobs_dir.mkdir(exist_ok=True)
    last = max((int(m.group(1)) for p in jobs_dir.glob("verify-*.md") if (m := re.fullmatch(r"verify-(\d+)", p.stem))), default=0)
    print(f"{len(findings)} findings, {len(findings) - len(pending)} already verified, {len(clusters)} clusters, {len(planned)} verifier jobs")
    for n, (agent, group) in enumerate(planned, last + 1):
        write_verifier_job(round_dir, f"verify-{n}", agent, group, review_tree, worktree_base)
        print(f"verify-{n}: {agent}")


def verdict_of(round_dir, f):
    path = round_dir / "verified" / f"{f['id']}.md"
    if not path.exists():
        return None
    meta, _, sections = read_md(path)
    if meta.get("status") not in STATUSES:
        raise Malformed(f"status must be one of {', '.join(STATUSES)}")
    if meta["status"] in KEPT and meta.get("proof") not in PROOFS:
        raise Malformed(f"proof must be one of {', '.join(PROOFS)} on a {meta['status']} verdict")
    if meta["status"] == "confirmed" and meta["proof"] == "trace":
        raise Malformed("proof: trace cannot back a confirmed verdict")
    if meta["status"] == "plausible" and "Judgment" not in sections:
        raise Malformed("a plausible verdict needs a Judgment section")
    evidence = sections.get("Evidence", "")
    return {
        "path": path,
        "status": meta["status"],
        "proof": meta.get("proof", ""),
        "severity": meta.get("severity") if meta.get("severity") in SEV else f["severity"],
        "duplicate_of": meta.get("duplicate_of", ""),
        "alternative": meta.get("alternative", ""),
        "fix_check": meta.get("fix_check", ""),
        "regression_test": bool(sections.get("Regression test")),
        "reason": evidence.splitlines()[0] if evidence else "",
    }


def judge(round_dir):
    findings, errors = load_findings(round_dir)
    if errors:
        fail(errors)
    ordered = [f for c in cluster(findings) for f in c["members"]]
    verdicts = {}
    for f in ordered:
        try:
            verdicts[f["id"]] = verdict_of(round_dir, f)
        except Malformed as e:
            errors.append(f"verified/{f['id']}.md: {e}")
    if errors:
        fail(errors)
    kept, dropped, unverified = [], [], []
    for f in ordered:
        v = verdicts[f["id"]]
        if not v or v["status"] == "escalate":
            unverified.append(f)
        elif v["status"] in KEPT:
            kept.append((f, v))
        else:
            dropped.append((f, v))
    kept.sort(key=lambda fv: (SEV[fv[1]["severity"]], fv[1]["status"] != "confirmed", fv[0]["file"], fv[0]["lines"][0]))
    return kept, dropped, unverified


def serious(kept):
    return sum(1 for _, v in kept if SEV[v["severity"]] <= SEV["high"])


def kept_line(f, v):
    return f"- {v['severity']}, {v['status']} ({v['proof']}): {f['title']} ({location(f['file'], f['lines'])}), {f['path']}, {v['path']}"


def alternatives(kept, label=lambda f: f["id"]):
    out = []
    for f, v in kept:
        if f["sections"].get("Alternative") and v["alternative"] != "drop":
            out += ["", f"### {f['title']} ({label(f)})", "", f["sections"]["Alternative"]]
    return out


def final(round_dir):
    kept, dropped, unverified = judge(round_dir)
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
    ]
    out += [kept_line(f, v) for f, v in kept]
    directions = alternatives(kept)
    if directions:
        out += ["", "## Alternative directions"] + directions
    out += ["", "## Dropped", ""]
    for f, v in dropped:
        reason = f"Duplicate of {v['duplicate_of']}." if v["status"] == "duplicate" and v["duplicate_of"] else v["reason"]
        out.append(f"- {v['status']}: {f['title']} ({location(f['file'], f['lines'])}).{' ' + reason if reason else ''}")
    out += ["", "## Unverified", ""]
    for f in sorted(unverified, key=lambda f: (SEV[f["severity"]], f["file"], f["lines"][0])):
        out.append(f"- {f['severity']}: {f['title']} ({location(f['file'], f['lines'])}), {f['path']}")
    (round_dir / "final.md").write_text("\n".join(out) + "\n")
    print(f"kept {len(kept)} ({serious(kept)} critical or high), dropped {len(dropped)}, unverified {len(unverified)}")


def write_fixer_job(round_dir, agent, todo, review_tree, fixes_ref):
    name = f"fixer-{agent}"
    lines = [
        f"# Job: {name}",
        "",
        f"Agent: {agent}",
        f"Role: {SKILL_DIR}/agents/{name}.md",
        f"Protocol: {SKILL_DIR}/references/fix-protocol.md",
        f"Format: {SKILL_DIR}/references/findings-format.md",
        f"Brief: {round_dir}/brief.md",
        f"Code: {review_tree}",
        f"Pin: {fixes_ref}",
        f"Notes: {round_dir}/notes/{name}.md",
        f"Output: {round_dir}/fixes.md",
        "",
        "## Findings",
        "",
    ]
    lines += [f"- {f['id']}: {f['path']}, {v['path']}" for f, v in todo]
    (round_dir / "jobs" / f"{name}.md").write_text("\n".join(lines) + "\n")


def fix(round_dir, review_tree, fixes_ref, min_severity):
    kept, _, _ = judge(round_dir)
    todo = [(f, v) for f, v in kept if f["category"] != UNKINDED and SEV[v["severity"]] <= SEV[min_severity]]
    if not todo:
        print("0 to fix")
        return
    proven = [(f, v) for f, v in todo if v["status"] == "confirmed" and v["fix_check"] == "pass" and v["regression_test"]]
    rest = [fv for fv in todo if fv not in proven]
    if proven:
        write_fixer_job(round_dir, "junior", proven, review_tree, fixes_ref)
    write_fixer_job(round_dir, "senior", rest, review_tree, fixes_ref)
    print(f"{len(todo)} to fix: {len(proven)} junior, {len(rest)} senior")


def fix_outcomes(round_dir):
    path = round_dir / "fixes.md"
    if not path.exists():
        return {}
    items = (re.fullmatch(r"- ([\w-]+): (.+)", line) for line in path.read_text().splitlines())
    return {m.group(1): m.group(2) for m in items if m}


def report(round_dirs):
    fixed, still_open, directions, counts, unproven = [], [], [], {}, []
    for n, round_dir in enumerate(round_dirs, 1):
        kept, dropped, _ = judge(round_dir)
        outcomes = fix_outcomes(round_dir)
        for f, v in kept:
            outcome = outcomes.get(f["id"], "")
            if outcome.startswith("fixed in "):
                fixed.append(f"- round {n}, {f['id']}: {f['title']} ({location(f['file'], f['lines'])}), {outcome}")
            else:
                still_open.append((f, v, kept_line(f, v) + (f" -- fixer: {outcome}" if outcome else "")))
        directions += alternatives(kept, lambda f: f"round {n}, {f['id']}")
        for f, v in dropped:
            counts[v["status"]] = counts.get(v["status"], 0) + 1
            if v["status"] == "unproven" and SEV[v["severity"]] <= SEV["high"]:
                unproven.append(f["title"])
    still_open.sort(key=lambda fvl: SEV[fvl[1]["severity"]])
    out = ["# Report", "", "## Fixed", ""] + fixed + ["", "## Open", ""] + [line for _, _, line in still_open]
    if directions:
        out += ["", "## Alternative directions"] + directions
    out += ["", "## Dropped", ""]
    out += [f"- {status}: {counts[status]}" for status in STATUSES if status in counts]
    out.append(f"- unproven at critical or high: {'; '.join(unproven) or 'none'}")
    path = round_dirs[-1] / "report.md"
    path.write_text("\n".join(out) + "\n")
    print(f"fixed {len(fixed)}, open {len(still_open)} ({serious([(f, v) for f, v, _ in still_open])} critical or high), {path}")


def main():
    ap = argparse.ArgumentParser(description="Merge review findings, assemble the final list, route fixes, report the run.")
    ap.add_argument("mode", choices=["merge", "final", "fix", "report"])
    ap.add_argument("round_dirs", nargs="+")
    ap.add_argument("--review-tree")
    ap.add_argument("--worktree-base")
    ap.add_argument("--fixes-ref")
    ap.add_argument("--min-severity", choices=list(SEV), default="low")
    ap.add_argument("--batch-size", type=int, default=8)
    args = ap.parse_args()
    round_dirs = [pathlib.Path(d).resolve() for d in args.round_dirs]
    if args.mode != "report" and len(round_dirs) > 1:
        ap.error(f"{args.mode} takes one round dir")
    if args.mode == "merge":
        if not (args.review_tree and args.worktree_base):
            ap.error("merge needs --review-tree and --worktree-base")
        merge(round_dirs[0], args.review_tree, args.worktree_base, args.batch_size)
    elif args.mode == "final":
        final(round_dirs[0])
    elif args.mode == "fix":
        if not (args.review_tree and args.fixes_ref):
            ap.error("fix needs --review-tree and --fixes-ref")
        fix(round_dirs[0], args.review_tree, args.fixes_ref, args.min_severity)
    else:
        report(round_dirs)


if __name__ == "__main__":
    main()
