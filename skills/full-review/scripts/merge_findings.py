#!/usr/bin/env python3
"""Merge specialist findings and plan verification, then assemble the final kept set.

Usage:
  merge_findings.py <run-dir> merge [--batch-size 8]
      Reads findings/*.json. Folds duplicates (same file, same category, overlapping
      lines), clusters overlapping findings, and plans verification batches.
      Writes merged.json.
  merge_findings.py <run-dir> final
      Reads merged.json and verified/*.json. Writes final.json with confirmed and
      plausible findings sorted by severity, plus dropped counts.
"""

import argparse
import json
import pathlib
import sys

SEV = {"critical": 0, "high": 1, "medium": 2, "low": 3}


def overlaps(a, b, slack=3):
    return a[0] <= b[1] + slack and b[0] <= a[1] + slack


def norm_lines(v):
    if isinstance(v, int):
        return [v, v]
    if isinstance(v, list) and len(v) == 2:
        return [int(v[0]), int(v[1])]
    return [0, 0]


def merge(run, batch_size):
    raw, extras, errors = [], {"escalations": [], "handoffs": [], "uncovered": []}, []
    for fp in sorted((run / "findings").glob("*.json")):
        try:
            data = json.loads(fp.read_text())
        except (json.JSONDecodeError, OSError) as e:
            errors.append(f"{fp.name}: {e}")
            continue
        job = data.get("job") or fp.stem
        for i, f in enumerate(data.get("findings", []), 1):
            f = dict(
                f,
                id=f"{job}-{i}",
                source=job,
                file=f.get("file") or "",
                lines=norm_lines(f.get("lines")),
            )
            if f.get("severity") not in SEV:
                errors.append(
                    f"{f['id']}: unknown severity {f.get('severity')!r}, treated as medium"
                )
                f["severity"] = "medium"
            raw.append(f)
        for key in extras:
            for x in data.get(key, []):
                extras[key].append(
                    {"job": job, **x}
                    if isinstance(x, dict)
                    else {"job": job, "item": x}
                )

    raw.sort(key=lambda f: (SEV[f["severity"]], f["file"], f["lines"][0]))
    clusters = []
    for f in raw:
        for c in clusters:
            if c["file"] == f["file"] and overlaps(c["lines"], f["lines"]):
                c["members"].append(f)
                c["lines"] = [
                    min(c["lines"][0], f["lines"][0]),
                    max(c["lines"][1], f["lines"][1]),
                ]
                break
        else:
            clusters.append(
                {"file": f["file"], "lines": list(f["lines"]), "members": [f]}
            )

    findings, batch_plan = [], {"deep": [], "standard": []}
    for n, c in enumerate(
        sorted(clusters, key=lambda c: (c["file"], c["lines"][0])), 1
    ):
        cid, kept = f"c{n}", []
        for f in c[
            "members"
        ]:  # already severity-ordered: first of each category is kept
            primary = next(
                (
                    k
                    for k in kept
                    if k["category"] == f.get("category")
                    and overlaps(k["lines"], f["lines"])
                ),
                None,
            )
            if primary:
                primary.setdefault("duplicates", []).append(f["id"])
            else:
                kept.append(dict(f, cluster=cid))
        findings.extend(kept)
        tier = "deep" if any(SEV[f["severity"]] <= 1 for f in kept) else "standard"
        batch_plan[tier].append([f["id"] for f in kept])

    batches = []
    for tier, groups in batch_plan.items():
        chunks, current = [], []
        for ids in groups:  # a cluster always stays within one batch
            if current and len(current) + len(ids) > batch_size:
                chunks.append(current)
                current = []
            current += ids
        if current:
            chunks.append(current)
        batches += [
            {"name": f"{tier}-{i}", "tier": tier, "ids": ids}
            for i, ids in enumerate(chunks, 1)
        ]

    out = {"findings": findings, "batches": batches, **extras, "errors": errors}
    (run / "merged.json").write_text(json.dumps(out, indent=2))
    sev_counts = {s: sum(f["severity"] == s for f in findings) for s in SEV}
    print(
        f"{len(raw)} raw findings -> {len(findings)} after folding duplicates, "
        f"{len(clusters)} clusters. By severity: {sev_counts}"
    )
    for b in batches:
        print(f"  batch {b['name']} ({b['tier']}): {len(b['ids'])} findings")
    for key, items in extras.items():
        if items:
            print(f"  {key}: {len(items)}")
    for e in errors:
        print(f"  error: {e}", file=sys.stderr)


def final(run):
    merged = json.loads((run / "merged.json").read_text())
    results = {}
    for fp in sorted((run / "verified").glob("*.json")):
        for r in json.loads(fp.read_text()).get("results", []):
            results[r["id"]] = r
    kept, dropped, missing = [], {}, []
    folded = sum(len(f.get("duplicates", [])) for f in merged["findings"])
    if folded:
        dropped["duplicate"] = folded
    for f in merged["findings"]:
        r = results.get(f["id"])
        if r is None:
            missing.append(f["id"])
            continue
        status = r.get("status", "unproven")
        if status in ("confirmed", "plausible"):
            kept.append(
                dict(
                    f,
                    status=status,
                    severity=r.get("severity", f["severity"]),
                    verification=r.get("evidence", ""),
                    verifier_notes=r.get("notes", ""),
                )
            )
        else:
            dropped[status] = dropped.get(status, 0) + 1
    kept.sort(
        key=lambda f: (SEV.get(f["severity"], 2), f["status"] != "confirmed", f["file"])
    )
    (run / "final.json").write_text(
        json.dumps(
            {"findings": kept, "dropped": dropped, "unverified": missing}, indent=2
        )
    )
    print(f"{len(kept)} kept, dropped: {dropped or 'none'}")
    if missing:
        print(
            f"WARNING: no verification result for {missing}; rerun their verifier batch",
            file=sys.stderr,
        )


def main():
    ap = argparse.ArgumentParser(description="Merge and finalize review findings.")
    ap.add_argument("run_dir")
    ap.add_argument("mode", choices=["merge", "final"])
    ap.add_argument("--batch-size", type=int, default=8)
    args = ap.parse_args()
    run = pathlib.Path(args.run_dir)
    merge(run, args.batch_size) if args.mode == "merge" else final(run)


if __name__ == "__main__":
    main()
