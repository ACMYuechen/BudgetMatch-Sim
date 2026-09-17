import os
from pathlib import Path
import subprocess
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]


class PublishPolicyTests(unittest.TestCase):
    def test_workflow_only_accepts_manual_runs_on_main(self):
        workflow = yaml.load((ROOT / ".github/workflows/deploy.yml").read_text(), Loader=yaml.BaseLoader)
        self.assertEqual({"workflow_dispatch"}, set(workflow["on"]))
        release = workflow["jobs"]["release"]
        self.assertEqual(
            "github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/main'",
            release["if"],
        )
        self.assertEqual("${{ github.sha }}", release["steps"][0]["with"]["ref"])

    def run_publish(self, source_branch, github_ref, event_name):
        with tempfile.TemporaryDirectory(prefix="budgetmatch-publish-policy-") as directory:
            path = Path(directory)
            git_log = path / "git-calls"
            fake_git = path / "git"
            fake_git.write_text(
                '#!/bin/sh\n'
                'printf "%s\\n" "$*" >> "$GIT_TEST_LOG"\n'
                'if [ "$1" = ls-remote ]; then\n'
                '  printf "%s\\trefs/heads/main\\n" "$GIT_TEST_REMOTE_SHA"\n'
                '  exit 0\n'
                'fi\n'
                'exit 91\n'
            )
            fake_git.chmod(0o755)
            environment = dict(os.environ)
            environment.update({
                "PATH": str(path) + os.pathsep + os.environ["PATH"],
                "GIT_TEST_LOG": str(git_log),
                "GIT_TEST_REMOTE_SHA": "b" * 40,
                "SOURCE_SHA": "a" * 40,
                "SOURCE_BRANCH": source_branch,
                "GITHUB_REF": github_ref,
                "GITHUB_EVENT_NAME": event_name,
                "BACKEND_IMAGE": "ghcr.io/example/backend@sha256:" + "1" * 64,
                "WEB_IMAGE": "ghcr.io/example/web@sha256:" + "2" * 64,
            })
            result = subprocess.run(
                ["bash", str(ROOT / "scripts/deploy/publish.sh")],
                env=environment, capture_output=True, text=True, check=False, timeout=10,
            )
            calls = git_log.read_text() if git_log.exists() else ""
            return result, calls

    def test_non_main_sources_are_rejected_before_git_access(self):
        for source_branch, github_ref in [
            ("refactor/agent", "refs/heads/refactor/agent"),
            ("main", "refs/heads/refactor/agent"),
            ("refactor/agent", "refs/heads/main"),
            ("main", "refs/tags/main"),
        ]:
            with self.subTest(source_branch=source_branch, github_ref=github_ref):
                result, calls = self.run_publish(source_branch, github_ref, "workflow_dispatch")
                self.assertEqual(1, result.returncode)
                self.assertIn("Only a manual workflow_dispatch run on main", result.stderr)
                self.assertEqual("", calls)

    def test_automatic_and_missing_events_are_rejected_before_git_access(self):
        for event_name in ["push", "pull_request", "schedule", "workflow_run", ""]:
            with self.subTest(event_name=event_name):
                result, calls = self.run_publish("main", "refs/heads/main", event_name)
                self.assertEqual(1, result.returncode)
                self.assertIn("Only a manual workflow_dispatch run on main", result.stderr)
                self.assertEqual("", calls)

    def test_manual_main_run_passes_policy_but_skips_an_outdated_commit(self):
        result, calls = self.run_publish("main", "refs/heads/main", "workflow_dispatch")
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertIn("A newer source commit exists", result.stdout)
        self.assertEqual("ls-remote --exit-code origin refs/heads/main\n", calls)


if __name__ == "__main__":
    unittest.main()
