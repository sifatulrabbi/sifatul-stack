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

    def test_instructions_agents_read_are_semantic_and_plain_docs_are_not(self):
        prose = "# Title\n\nDo the thing.\n"
        paths = ["skills/review/SKILL.md", "agents/verifier.md", "AGENTS.md", ".claude/commands/ship.md", "GEMINI.md", ".github/copilot-instructions.md", ".cursorrules", ".gemini/commands/review.toml", "AGENTS.override.md", "README.md"]
        for path in paths:
            self.repo.write(path, prose)
        self.repo.commit()
        self.repo.git("checkout", "-q", "-b", "feature")
        for path in paths:
            self.repo.write(path, "# Renamed title\n\nDo the thing.\n")
        self.repo.commit()

        hunks = self.repo.hunks()

        semantic, mechanical = hunks.split("## Mechanical")
        for path in paths[:-1]:
            self.assertIn(f"| {path} |", semantic)
        self.assertIn("### docs\n\n- README.md", mechanical)

    def test_code_under_an_instruction_dir_keeps_the_comment_label(self):
        self.repo.write(".claude/commands/tool.py", "# old note\nx = 1\n")
        self.repo.commit()
        self.repo.git("checkout", "-q", "-b", "feature")
        self.repo.write(".claude/commands/tool.py", "# new note\nx = 1\n")
        self.repo.commit()

        self.assertIn("### comment\n\n- .claude/commands/tool.py: 1 · 2\n", self.repo.hunks())

    def test_joining_words_in_an_instruction_file_is_semantic(self):
        self.repo.write("skills/x/SKILL.md", "You can not skip this.\n")
        self.repo.commit()
        self.repo.git("checkout", "-q", "-b", "feature")
        self.repo.write("skills/x/SKILL.md", "You cannot skip this.\n")
        self.repo.commit()

        self.assertIn("| skills/x/SKILL.md |", self.repo.hunks().split("## Mechanical")[0])

    def test_reindenting_an_instruction_file_is_whitespace(self):
        self.repo.write("skills/y/SKILL.md", "Check it.\n")
        self.repo.commit()
        self.repo.git("checkout", "-q", "-b", "feature")
        self.repo.write("skills/y/SKILL.md", "  Check  it.\n")
        self.repo.commit()

        self.assertIn("skills/y/SKILL.md", self.repo.hunks().split("## Mechanical")[1])

    def test_cursor_rules_and_extensionless_prompts_are_semantic(self):
        paths = [".cursor/rules/style.mdc", "prompts/system"]
        for path in paths:
            self.repo.write(path, "# Never use any\n")
        self.repo.commit()
        self.repo.git("checkout", "-q", "-b", "feature")
        for path in paths:
            self.repo.write(path, "# Always use any\n")
        self.repo.commit()

        semantic = self.repo.hunks().split("## Mechanical")[0]
        for path in paths:
            self.assertIn(f"| {path} |", semantic)


if __name__ == "__main__":
    unittest.main()
