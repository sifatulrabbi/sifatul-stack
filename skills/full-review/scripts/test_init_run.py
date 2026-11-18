import os
import pathlib
import shutil
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


class Repo:
    def __init__(self, test, path):
        self.test = test
        self.path = path
        self.env = ENV

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.path, env=ENV, check=True, capture_output=True, text=True).stdout.strip()

    def commit(self, text):
        (self.path / "f.txt").write_text(text)
        self.git("add", "-A")
        self.git("commit", "-q", "-m", text)
        return self.git("rev-parse", "HEAD")

    def worktree(self, path, planning):
        self.git("worktree", "add", "-q", "--detach", str(path))
        if planning:
            (path / ".planning").mkdir()
        return Repo(self.test, path)

    def run(self, script, *args):
        return subprocess.run(["bash", str(SCRIPTS / script), *args], cwd=self.path, env=self.env, capture_output=True, text=True)

    def sh(self, script, *args):
        res = self.run(script, *args)
        assert res.returncode == 0, res.stderr
        out = dict(line.split("=", 1) for line in res.stdout.splitlines() if "=" in line)
        if script == "init_run.sh":
            self.test.addCleanup(shutil.rmtree, pathlib.Path(out["ROUND_DIR"]).parents[2], True)
            self.test.addCleanup(self.run, "cleanup_run.sh", args[0])
        return out


class InitRunTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = pathlib.Path(tmp.name).resolve()
        self.repo = self.make_repo("repo", planning=True)
        self.first = self.repo.commit("a")

    def make_repo(self, rel, planning):
        path = self.tmp / rel
        path.mkdir(parents=True)
        if planning:
            (path / ".planning").mkdir()
        repo = Repo(self, path)
        repo.git("init", "-q", "-b", "main")
        return repo

    def finish(self, round_dir):
        (pathlib.Path(round_dir) / "review.md").write_text("# Review\n")

    def test_first_round_records_the_head_and_checks_it_out(self):
        out = self.repo.sh("init_run.sh", "wip/main", self.first)

        self.assertEqual(out["ROUND"], "1")
        self.assertEqual(out["ROUND_DIR"], f"{self.repo.path}/.planning/full-review/wip/main/round-1")
        self.assertEqual(out["PREV_ROUND_DIR"], "")
        self.assertIn(f"head: {self.first}", (pathlib.Path(out["ROUND_DIR"]) / "round.md").read_text())
        self.assertEqual((pathlib.Path(out["REVIEW_TREE"]) / "f.txt").read_text(), "a")

    def test_resumes_an_unfinished_round_at_the_same_head(self):
        self.repo.sh("init_run.sh", "pr/7", self.first)

        out = self.repo.sh("init_run.sh", "pr/7", self.first)

        self.assertEqual(out["ROUND"], "1")

    def test_resumes_a_round_whose_review_tree_has_fixer_commits(self):
        out = self.repo.sh("init_run.sh", "pr/7", self.first)
        Repo(self, pathlib.Path(out["REVIEW_TREE"])).commit("fixed")

        resumed = self.repo.sh("init_run.sh", "pr/7", self.first)

        self.assertEqual(resumed["REVIEW_TREE"], out["REVIEW_TREE"])

    def test_refuses_a_review_tree_left_at_another_head(self):
        out = self.repo.sh("init_run.sh", "pr/7", self.first)
        shutil.rmtree(out["ROUND_DIR"])
        other_head = self.repo.commit("b")

        res = self.repo.run("init_run.sh", "pr/7", other_head)

        self.assertNotEqual(res.returncode, 0)
        self.assertEqual((pathlib.Path(out["REVIEW_TREE"]) / "f.txt").read_text(), "a")

    def test_new_push_after_a_finished_round_starts_the_next_round(self):
        first = self.repo.sh("init_run.sh", "pr/7", self.first)
        self.finish(first["ROUND_DIR"])
        second_head = self.repo.commit("b")

        out = self.repo.sh("init_run.sh", "pr/7", second_head)

        self.assertEqual(out["ROUND"], "2")
        self.assertEqual(out["PREV_ROUND_DIR"], first["ROUND_DIR"])
        self.assertEqual((pathlib.Path(out["REVIEW_TREE"]) / "f.txt").read_text(), "b")

    def test_cleanup_drops_the_fixes_ref_with_the_worktrees(self):
        out = self.repo.sh("init_run.sh", "branch/feature/x", self.first)
        self.repo.git("update-ref", out["FIXES_REF"], self.first)

        self.repo.sh("cleanup_run.sh", "branch/feature/x")

        self.assertEqual(self.repo.git("for-each-ref", "refs/full-review/"), "")
        self.assertFalse(pathlib.Path(out["REVIEW_TREE"]).exists())

    def test_branches_that_differ_only_by_a_slash_get_separate_rounds(self):
        slashed = self.repo.sh("init_run.sh", "branch/feature/x", self.first)
        dashed = self.repo.sh("init_run.sh", "branch/feature-x", self.first)

        self.assertNotEqual(slashed["ROUND_DIR"], dashed["ROUND_DIR"])
        self.assertNotEqual(slashed["REVIEW_TREE"], dashed["REVIEW_TREE"])
        self.assertNotEqual(slashed["FIXES_REF"], dashed["FIXES_REF"])

    def test_cleaning_a_branch_leaves_its_nested_branch_alone(self):
        nested = self.repo.sh("init_run.sh", "branch/a/b", self.first)
        self.repo.sh("init_run.sh", "branch/a", self.first)

        self.repo.sh("cleanup_run.sh", "branch/a")

        self.assertTrue((pathlib.Path(nested["REVIEW_TREE"]) / "f.txt").exists())

    def test_an_escaped_slash_never_collides_with_a_literal_one(self):
        slashed = self.repo.sh("init_run.sh", "branch/a/b", self.first)
        literal = self.repo.sh("init_run.sh", "branch/a%2Fb", self.first)

        self.assertNotEqual(slashed["ROUND_DIR"], literal["ROUND_DIR"])
        self.assertNotEqual(slashed["FIXES_REF"], literal["FIXES_REF"])

    def test_repos_with_the_same_name_never_share_rounds_or_trees(self):
        one = self.make_repo("one/app", planning=False)
        two = self.make_repo("two/app", planning=False)
        one_head, two_head = one.commit("one"), two.commit("two")
        first = one.sh("init_run.sh", "branch/main", one_head)
        self.finish(first["ROUND_DIR"])

        second = two.sh("init_run.sh", "branch/main", two_head)

        self.assertEqual(second["ROUND"], "1")
        self.assertEqual(second["PREV_ROUND_DIR"], "")
        self.assertEqual((pathlib.Path(second["REVIEW_TREE"]) / "f.txt").read_text(), "two")

    def test_cleanup_leaves_a_same_named_repos_worktrees_alone(self):
        one = self.make_repo("one/app", planning=False)
        two = self.make_repo("two/app", planning=False)
        one_tree = one.sh("init_run.sh", "branch/main", one.commit("one"))["REVIEW_TREE"]
        two.sh("init_run.sh", "branch/main", two.commit("two"))

        two.sh("cleanup_run.sh", "branch/main")

        self.assertTrue((pathlib.Path(one_tree) / "f.txt").exists())

    def test_worktrees_of_one_repo_never_share_rounds_trees_or_fixes(self):
        for planning in [True, False]:
            with self.subTest(planning=planning):
                main = self.make_repo(f"{planning}/x/app", planning)
                head = main.commit("a")
                other = main.worktree(self.tmp / f"{planning}/y/app", planning)

                first = main.sh("init_run.sh", "pr/1", head)
                second = other.sh("init_run.sh", "pr/1", head)

                for key in ["ROUND_DIR", "REVIEW_TREE", "FIXES_REF"]:
                    self.assertNotEqual(first[key], second[key])

    def test_cleanup_in_one_worktree_keeps_the_others_fixes(self):
        main = self.make_repo("main", planning=False)
        head = main.commit("a")
        other = main.worktree(self.tmp / "other", planning=False)
        fixes = main.sh("init_run.sh", "pr/8", head)["FIXES_REF"]
        other.sh("init_run.sh", "pr/8", head)
        main.git("update-ref", fixes, head)

        other.sh("cleanup_run.sh", "pr/8")

        self.assertEqual(main.git("rev-parse", fixes), head)

    def test_keeps_run_state_under_the_users_temp_dir(self):
        repo = self.make_repo("plain", planning=False)
        head = repo.commit("a")
        root = self.tmp / "private"
        repo.env = {**ENV, "TMPDIR": f"{root}/"}

        out = repo.sh("init_run.sh", "pr/1", head)

        self.assertTrue(out["ROUND_DIR"].startswith(f"{root}/full-review/"))
        self.assertTrue(out["REVIEW_TREE"].startswith(f"{root}/full-review-worktrees/"))

    def test_refuses_a_target_without_a_kind_and_a_name(self):
        for target in ["pr7", "pr", "pr/", "x/1"]:
            with self.subTest(target=target):
                res = self.repo.run("init_run.sh", target, self.first)

                self.assertNotEqual(res.returncode, 0)


if __name__ == "__main__":
    unittest.main()
