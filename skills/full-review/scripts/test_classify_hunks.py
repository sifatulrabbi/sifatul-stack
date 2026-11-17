import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("classify_hunks.py")
ENV = {
    **os.environ,
    "GIT_CONFIG_GLOBAL": os.devnull,
    "GIT_CONFIG_NOSYSTEM": "1",
    "GIT_AUTHOR_NAME": "t",
    "GIT_AUTHOR_EMAIL": "t@t",
    "GIT_COMMITTER_NAME": "t",
    "GIT_COMMITTER_EMAIL": "t@t",
}
CODE = "".join(f"x{i} = {i}\n" for i in range(1, 11))


class Repo:
    def __init__(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = pathlib.Path(self._tmp.name)
        self.git("init", "-q", "-b", "main")

    def close(self):
        self._tmp.cleanup()

    def git(self, *args):
        subprocess.run(["git", *args], cwd=self.dir, env=ENV, check=True, capture_output=True)

    def write(self, path, text):
        p = self.dir / path
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(text)

    def commit(self):
        self.git("add", "-A")
        self.git("commit", "-q", "-m", "c")

    def hunks(self):
        res = subprocess.run(
            [sys.executable, "-I", str(SCRIPT), "--base", "main", "--head", "feature", "-o", "hunks.md"],
            cwd=self.dir,
            env=ENV,
            capture_output=True,
            text=True,
        )
        assert res.returncode == 0, res.stderr
        return (self.dir / "hunks.md").read_text()


class ClassifyHunksTest(unittest.TestCase):
    def setUp(self):
        self.repo = Repo()
        self.addCleanup(self.repo.close)

    def test_lists_semantic_hunks_with_head_lines_and_changed_counts(self):
        self.repo.write("src/app.py", CODE)
        self.repo.commit()
        self.repo.git("checkout", "-q", "-b", "feature")
        lines = CODE.splitlines(keepends=True)
        lines[2] = "x3 = 300\n"
        del lines[6:8]
        self.repo.write("src/app.py", "".join(lines))
        self.repo.commit()

        hunks = self.repo.hunks()

        self.assertIn("semantic_lines: 4", hunks)
        self.assertIn("| src/app.py | modified | 3 · 2, removed 7-8 · 2 |", hunks)
        self.assertNotIn("src/app.py", hunks[hunks.index("## Mechanical"):])

    def test_groups_mechanical_changes_by_kind(self):
        self.repo.write("package-lock.json", '{\n  "v": 1\n}\n')
        self.repo.write("util.py", CODE)
        self.repo.write("old.py", CODE)
        self.repo.commit()
        self.repo.git("checkout", "-q", "-b", "feature")
        self.repo.write("package-lock.json", '{\n  "v": 2\n}\n')
        self.repo.write("util.py", "# note\n" + CODE)
        self.repo.git("mv", "old.py", "new.py")
        self.repo.commit()

        hunks = self.repo.hunks()

        self.assertIn("semantic_files: 0", hunks)
        self.assertIn("### lockfile\n\n- package-lock.json · 2 lines\n", hunks)
        self.assertIn("### comment\n\n- util.py: 1 · 1\n", hunks)
        self.assertIn("### rename\n\n- old.py → new.py\n", hunks)


if __name__ == "__main__":
    unittest.main()
