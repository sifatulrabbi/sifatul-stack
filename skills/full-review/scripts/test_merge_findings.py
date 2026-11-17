import pathlib
import subprocess
import sys
import tempfile
import textwrap
import unittest

SCRIPT = pathlib.Path(__file__).with_name("merge_findings.py")
SKILL_DIR = SCRIPT.parent.parent


def finding(file: str | None = "src/api/users.ts", lines: str | None = "120-134", severity: str | None = "high", title="PATCH has no ownership check", extra=""):
    head = ["---"]
    if file is not None:
        head.append(f"file: {file}")
    if lines is not None:
        head.append(f"lines: {lines}")
    if severity is not None:
        head.append(f"severity: {severity}")
    head.append("---")
    body = textwrap.dedent(
        f"""
        # {title}

        ## Claim
        Any user can update another user.

        ## Trigger
        PATCH /users/<other id> as user A.

        ## Fix
        Call requireOwner before loading.
        """
    )
    return "\n".join(head) + "\n" + body + extra


def verdict(status="confirmed", severity="high", evidence="Test went red.", extra_meta="", extra=""):
    meta = f"---\nstatus: {status}\nseverity: {severity}\n{extra_meta}---\n"
    return meta + f"\n## Evidence\n{evidence}\n" + extra


class Round:
    def __init__(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = pathlib.Path(self._tmp.name).resolve() / "round-1"
        (self.dir / "findings").mkdir(parents=True)
        (self.dir / "verified").mkdir()
        (self.dir / "jobs").mkdir()

    def close(self):
        self._tmp.cleanup()

    def add(self, job, n, text):
        d = self.dir / "findings" / job
        d.mkdir(exist_ok=True)
        (d / f"{n}.md").write_text(text)

    def done(self, job, text="# Done\n"):
        d = self.dir / "findings" / job
        d.mkdir(exist_ok=True)
        (d / "_done.md").write_text(text)

    def verify(self, finding_id, text):
        (self.dir / "verified" / f"{finding_id}.md").write_text(text)

    def run(self, mode, *args):
        cmd = [sys.executable, "-I", str(SCRIPT), mode, str(self.dir)]
        if mode == "merge":
            cmd += ["--review-tree", "/wt/review", "--worktree-base", "/wt"]
        return subprocess.run(cmd + list(args), capture_output=True, text=True)

    def job(self, name):
        return (self.dir / "jobs" / f"{name}.md").read_text()

    def jobs(self):
        return sorted(p.stem for p in (self.dir / "jobs").glob("verify-*.md"))

    def final(self):
        return (self.dir / "final.md").read_text()


class RoundTest(unittest.TestCase):
    def setUp(self):
        self.r = Round()
        self.addCleanup(self.r.close)


class MergeTest(RoundTest):
    def test_writes_a_verifier_job_listing_each_finding_file(self):
        self.r.add("security", 1, finding())
        self.r.done("security")

        res = self.r.run("merge")

        self.assertEqual(res.returncode, 0, res.stderr)
        self.assertEqual(self.r.jobs(), ["verify-1"])
        job = self.r.job("verify-1")
        self.assertIn(f"security-1: {self.r.dir}/findings/security/1.md", job)
        self.assertIn(f"Role: {SKILL_DIR}/agents/verifier.md", job)
        self.assertIn("Code: /wt/review", job)
        self.assertIn("Scratch: /wt/verify-1", job)
        self.assertIn(f"Output: {self.r.dir}/verified/<id>.md", job)

    def test_refuses_when_a_specialist_has_not_finished(self):
        self.r.add("security", 1, finding())
        self.r.done("security")
        self.r.add("tests", 1, finding(file="src/a.ts"))

        res = self.r.run("merge")

        self.assertNotEqual(res.returncode, 0)
        self.assertIn("tests: no _done.md", res.stderr)
        self.assertEqual(self.r.jobs(), [])

    def test_refuses_a_finding_without_severity(self):
        self.r.add("security", 1, finding(severity=None))
        self.r.done("security")

        res = self.r.run("merge")

        self.assertNotEqual(res.returncode, 0)
        self.assertIn("findings/security/1.md: severity", res.stderr)
        self.assertEqual(self.r.jobs(), [])

    def test_sends_overlapping_findings_from_one_specialist_to_the_verifier(self):
        self.r.add("security", 1, finding(lines="120-134", title="Off by one"))
        self.r.add("security", 2, finding(lines="122", title="Null deref"))
        self.r.done("security")

        self.r.run("merge")

        job = self.r.job("verify-1")
        self.assertIn("security-1:", job)
        self.assertIn("security-2:", job)

    def test_refuses_a_finding_with_unreadable_lines(self):
        self.r.add("security", 1, finding(lines="10–14"))
        self.r.done("security")

        res = self.r.run("merge")

        self.assertNotEqual(res.returncode, 0)
        self.assertIn("findings/security/1.md: lines", res.stderr)
        self.assertEqual(self.r.jobs(), [])

    def test_rerun_writes_new_jobs_only_for_unverified_findings(self):
        self.r.add("security", 1, finding(file="a.ts"))
        self.r.add("security", 2, finding(file="b.ts"))
        self.r.done("security")
        self.r.run("merge")
        self.r.verify("security-1", verdict())

        self.r.run("merge")

        self.assertEqual(self.r.jobs(), ["verify-1", "verify-2"])
        rerun = self.r.job("verify-2")
        self.assertIn("security-2:", rerun)
        self.assertNotIn("security-1:", rerun)

    def test_keeps_overlapping_findings_together_when_splitting_batches(self):
        self.r.add("security", 1, finding(file="a.ts", lines="10-20"))
        self.r.add("correctness", 1, finding(file="a.ts", lines="15-18"))
        self.r.add("tests", 1, finding(file="b.ts", lines="1-5"))
        for job in ("security", "correctness", "tests"):
            self.r.done(job)

        self.r.run("merge", "--batch-size", "2")

        self.assertEqual(self.r.jobs(), ["verify-1", "verify-2"])
        first = self.r.job("verify-1")
        self.assertIn("security-1:", first)
        self.assertIn("correctness-1:", first)
        self.assertIn("tests-1:", self.r.job("verify-2"))



class FinalTest(RoundTest):
    def test_orders_kept_findings_by_the_verifiers_severity(self):
        self.r.add("security", 1, finding(file="a.ts", severity="high", title="Weak check"))
        self.r.add("correctness", 1, finding(file="b.ts", severity="low", title="Lost write"))
        self.r.done("security")
        self.r.done("correctness")
        self.r.verify("security-1", verdict(severity="low"))
        self.r.verify("correctness-1", verdict(severity="critical"))

        res = self.r.run("final")

        self.assertEqual(res.returncode, 0, res.stderr)
        final = self.r.final()
        self.assertIn("| critical | confirmed | Lost write | b.ts:120-134 |", final)
        self.assertLess(final.index("Lost write"), final.index("Weak check"))


    def test_lists_dropped_findings_with_their_reasons(self):
        self.r.add("security", 1, finding(file="a.ts", lines="10-20", title="Weak check"))
        self.r.add("security", 2, finding(file="a.ts", lines="12-14", severity="low", title="Same check again"))
        self.r.add("tests", 1, finding(file="b.ts", title="Flaky retry"))
        self.r.done("security")
        self.r.done("tests")
        self.r.verify("security-1", verdict())
        self.r.verify("security-2", verdict(status="duplicate", extra_meta="duplicate_of: security-1\n"))
        self.r.verify("tests-1", verdict(status="refuted", evidence="Retry is bounded at line 40.\nSee test output."))

        self.r.run("final")

        final = self.r.final()
        dropped = final[final.index("## Dropped"):]
        self.assertIn("| refuted | Flaky retry | b.ts:120-134 | Retry is bounded at line 40. |", dropped)
        self.assertIn("| duplicate | Same check again | a.ts:12-14 | duplicate of security-1 |", dropped)


    def test_lists_findings_no_verifier_reached_as_unverified(self):
        self.r.add("security", 1, finding(file="a.ts", title="Weak check"))
        self.r.done("security")

        self.r.run("final")

        final = self.r.final()
        self.assertIn("unverified: 1", final)
        unverified = final[final.index("## Unverified"):]
        self.assertIn("| high | Weak check | a.ts:120-134 | findings/security/1.md |", unverified)


    def test_carries_alternatives_only_from_kept_findings(self):
        self.r.add("tests", 1, finding(file="a.ts", extra="\n## Alternative\nMove auth into middleware.\n"))
        self.r.add("tests", 2, finding(file="b.ts", extra="\n## Alternative\nReplace the mock framework.\n"))
        self.r.done("tests")
        self.r.verify("tests-1", verdict())
        self.r.verify("tests-2", verdict(status="refuted"))

        self.r.run("final")

        final = self.r.final()
        self.assertIn("Move auth into middleware.", final[final.index("## Alternative directions"):])
        self.assertNotIn("Replace the mock framework.", final)

    def test_leaves_out_an_alternative_the_verifier_rejected(self):
        self.r.add("tests", 1, finding(extra="\n## Alternative\nMove auth into middleware.\n"))
        self.r.done("tests")
        self.r.verify("tests-1", verdict(extra_meta="alternative: drop\n"))

        self.r.run("final")

        self.assertNotIn("Move auth into middleware.", self.r.final())

    def test_escapes_pipes_in_titles_so_tables_hold(self):
        self.r.add("security", 1, finding(file="a.ts", title="a || b short-circuits"))
        self.r.done("security")
        self.r.verify("security-1", verdict())

        self.r.run("final")

        self.assertIn("| a \\|\\| b short-circuits | a.ts:120-134 |", self.r.final())


    def test_refuses_a_verdict_with_an_unknown_status(self):
        self.r.add("security", 1, finding())
        self.r.done("security")
        self.r.verify("security-1", verdict(status="confirmd"))

        res = self.r.run("final")

        self.assertNotEqual(res.returncode, 0)
        self.assertIn("verified/security-1.md: status", res.stderr)
        self.assertFalse((self.r.dir / "final.md").exists())


if __name__ == "__main__":
    unittest.main()
