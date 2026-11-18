import pathlib
import subprocess
import sys
import tempfile
import textwrap
import unittest

SCRIPT = pathlib.Path(__file__).with_name("merge_findings.py")
SKILL_DIR = SCRIPT.parent.parent


def finding(file: str | None = "src/api/users.ts", lines: str | None = "120-134", severity: str | None = "high", kind: str | None = "behavior", title="PATCH has no ownership check", extra="", symptoms: str | None = None):
    head = ["---"]
    if file is not None:
        head.append(f"file: {file}")
    if lines is not None:
        head.append(f"lines: {lines}")
    if severity is not None:
        head.append(f"severity: {severity}")
    if kind is not None:
        head.append(f"kind: {kind}")
    if symptoms is not None:
        head.append(f"symptoms: {symptoms}")
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


def verdict(status="confirmed", severity="high", proof="test", evidence="Test went red.", extra_meta="", extra=""):
    proof_line = f"proof: {proof}\n" if proof else ""
    meta = f"---\nstatus: {status}\nseverity: {severity}\n{proof_line}{extra_meta}---\n"
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
        self.assertIn("- critical, confirmed (test): Lost write (b.ts:120-134), findings/correctness/1.md, verified/correctness-1.md\n", final)
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
        self.assertIn("- refuted: Flaky retry (b.ts:120-134). Retry is bounded at line 40.\n", dropped)
        self.assertIn("- duplicate: Same check again (a.ts:12-14). Duplicate of security-1.\n", dropped)


    def test_lists_findings_no_verifier_reached_as_unverified(self):
        self.r.add("security", 1, finding(file="a.ts", title="Weak check"))
        self.r.done("security")

        self.r.run("final")

        final = self.r.final()
        self.assertIn("unverified: 1", final)
        unverified = final[final.index("## Unverified"):]
        self.assertIn("- high: Weak check (a.ts:120-134), findings/security/1.md\n", unverified)


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

    def test_refuses_a_verdict_with_an_unknown_status(self):
        self.r.add("security", 1, finding())
        self.r.done("security")
        self.r.verify("security-1", verdict(status="confirmd"))

        res = self.r.run("final")

        self.assertNotEqual(res.returncode, 0)
        self.assertIn("verified/security-1.md: status", res.stderr)
        self.assertFalse((self.r.dir / "final.md").exists())

    def test_refuses_a_kept_verdict_that_does_not_say_how_it_was_proved(self):
        self.r.add("security", 1, finding())
        self.r.add("security", 2, finding(lines="200-210"))
        self.r.done("security")
        self.r.verify("security-1", verdict(status="plausible", proof=None))
        self.r.verify("security-2", verdict(status="confirmed", proof="guess"))

        res = self.r.run("final")

        self.assertNotEqual(res.returncode, 0)
        self.assertIn("verified/security-1.md: proof", res.stderr)
        self.assertIn("verified/security-2.md: proof", res.stderr)

    def test_refuses_a_confirmed_verdict_proved_only_by_trace(self):
        self.r.add("security", 1, finding())
        self.r.done("security")
        self.r.verify("security-1", verdict(status="confirmed", proof="trace"))

        res = self.r.run("final")

        self.assertNotEqual(res.returncode, 0)
        self.assertIn("verified/security-1.md: proof", res.stderr)

    def test_refuses_a_plausible_verdict_without_a_judgment_record(self):
        self.r.add("security", 1, finding())
        self.r.done("security")
        self.r.verify("security-1", verdict(status="plausible", proof="trace"))

        res = self.r.run("final")

        self.assertNotEqual(res.returncode, 0)
        self.assertIn("verified/security-1.md", res.stderr)

    def test_accepts_a_plausible_verdict_with_a_judgment_record(self):
        self.r.add("security", 1, finding())
        self.r.done("security")
        self.r.verify("security-1", verdict(status="plausible", proof="trace", extra="\n## Judgment\nPath: a.\n"))

        res = self.r.run("final")

        self.assertEqual(res.returncode, 0, res.stderr)

    def test_dropped_verdicts_need_no_proof(self):
        self.r.add("security", 1, finding())
        self.r.done("security")
        self.r.verify("security-1", verdict(status="refuted", proof=None))

        res = self.r.run("final")

        self.assertEqual(res.returncode, 0, res.stderr)


class KindTest(RoundTest):
    def test_refuses_a_finding_without_a_known_kind(self):
        self.r.add("correctness", 1, finding(kind=None))
        self.r.add("correctness", 2, finding(kind="bug"))
        self.r.done("correctness")

        res = self.r.run("merge")

        self.assertNotEqual(res.returncode, 0)
        self.assertIn("findings/correctness/1.md: kind", res.stderr)
        self.assertIn("findings/correctness/2.md: kind", res.stderr)

    def test_related_pr_findings_carry_no_kind(self):
        self.r.add("related-prs", 1, finding(kind=None))
        self.r.done("related-prs")

        res = self.r.run("merge")

        self.assertEqual(res.returncode, 0, res.stderr)


class SymptomsTest(RoundTest):
    def test_accepts_symptoms_that_name_this_rounds_findings(self):
        self.r.add("correctness", 1, finding())
        self.r.add("correctness", 2, finding(file="src/b.ts"))
        self.r.done("correctness")
        self.r.add("root-cause", 1, finding(kind="design", symptoms="correctness-1, correctness-2"))
        self.r.done("root-cause")

        res = self.r.run("merge")

        self.assertEqual(res.returncode, 0, res.stderr)

    def test_refuses_symptoms_that_name_no_finding_or_a_root_cause(self):
        self.r.add("correctness", 1, finding())
        self.r.done("correctness")
        self.r.add("root-cause", 1, finding(kind="design", symptoms="correctness-1, correctness-9"))
        self.r.add("carried", 1, finding(kind="design", symptoms="root-cause-1"))
        self.r.done("root-cause")
        self.r.done("carried")

        res = self.r.run("merge")

        self.assertNotEqual(res.returncode, 0)
        self.assertIn("findings/root-cause/1.md: symptoms", res.stderr)
        self.assertIn("correctness-9", res.stderr)
        self.assertNotIn("correctness-1,", res.stderr)
        self.assertIn("findings/carried/1.md: symptoms", res.stderr)
        self.assertEqual(self.r.jobs(), [])


if __name__ == "__main__":
    unittest.main()
