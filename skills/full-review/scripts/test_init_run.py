import os
import pathlib
import subprocess
import tempfile
import unittest

SCRIPTS = pathlib.Path(__file__).parent
ENV = {
    **os.environ,
    "GIT_CONFIG_GLOBAL": os.devnull,
    "GIT_CONFIG_NOSYSTEM": "1",
    "GIT_AUTHOR_NAME": "t",
    "GIT_AUTHOR_EMAIL": "t@t",
    "GIT_COMMITTER_NAME": "t",
    "GIT_COMMITTER_EMAIL": "t@t",
}


class InitRunTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.repo = pathlib.Path(tmp.name).resolve()
        (self.repo / ".planning").mkdir()
        self.git("init", "-q", "-b", "main")
        self.first = self.commit("a")
        self.addCleanup(self.sh, "cleanup_run.sh", "pr7")

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.repo, env=ENV, check=True, capture_output=True, text=True).stdout.strip()

    def commit(self, text):
        (self.repo / "f.txt").write_text(text)
        self.git("add", "-A")
        self.git("commit", "-q", "-m", text)
        return self.git("rev-parse", "HEAD")

    def sh(self, script, *args):
        res = subprocess.run(["bash", str(SCRIPTS / script), *args], cwd=self.repo, env=ENV, capture_output=True, text=True)
        assert res.returncode == 0, res.stderr
        return dict(line.split("=", 1) for line in res.stdout.splitlines() if "=" in line)

    def finish(self, round_dir):
        (pathlib.Path(round_dir) / "review.md").write_text("# Review\n")

    def test_first_round_records_the_head_and_checks_it_out(self):
        out = self.sh("init_run.sh", "pr7", self.first)

        self.assertEqual(out["ROUND"], "1")
        self.assertEqual(out["ROUND_DIR"], f"{self.repo}/.planning/full-review/pr7/round-1")
        self.assertEqual(out["PREV_ROUND_DIR"], "")
        self.assertIn(f"head: {self.first}", (pathlib.Path(out["ROUND_DIR"]) / "round.md").read_text())
        self.assertEqual((pathlib.Path(out["REVIEW_TREE"]) / "f.txt").read_text(), "a")

    def test_resumes_an_unfinished_round_at_the_same_head(self):
        self.sh("init_run.sh", "pr7", self.first)

        out = self.sh("init_run.sh", "pr7", self.first)

        self.assertEqual(out["ROUND"], "1")

    def test_new_push_after_a_finished_round_starts_the_next_round(self):
        first = self.sh("init_run.sh", "pr7", self.first)
        self.finish(first["ROUND_DIR"])
        second_head = self.commit("b")

        out = self.sh("init_run.sh", "pr7", second_head)

        self.assertEqual(out["ROUND"], "2")
        self.assertEqual(out["PREV_ROUND_DIR"], first["ROUND_DIR"])
        self.assertEqual((pathlib.Path(out["REVIEW_TREE"]) / "f.txt").read_text(), "b")


if __name__ == "__main__":
    unittest.main()
