import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]
REPOSITORY = "example/budgetmatch"
SOURCE_SHA = "a" * 40
BACKEND_IMAGE = "ghcr.io/example/backend@sha256:" + "1" * 64
WEB_IMAGE = "ghcr.io/example/web@sha256:" + "2" * 64


def ci_event(sha=SOURCE_SHA, **overrides):
    run = {
        "name": "CI", "status": "completed", "conclusion": "success", "event": "push",
        "head_branch": "main", "head_sha": sha,
        "head_repository": {"full_name": REPOSITORY},
    }
    run.update(overrides)
    return {"action": "completed", "workflow_run": run}


def publish_environment(event_path, sha=SOURCE_SHA):
    environment = dict(os.environ)
    for name in ["GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GITHUB_STEP_SUMMARY"]:
        environment.pop(name, None)
    environment.update({
        "SOURCE_SHA": sha, "SOURCE_BRANCH": "main",
        "GITHUB_EVENT_NAME": "workflow_run", "GITHUB_EVENT_PATH": str(event_path),
        "GITHUB_REPOSITORY": REPOSITORY,
        # workflow_run may have a newer default-branch SHA than the tested one.
        "GITHUB_SHA": "c" * 40, "GITHUB_REF": "refs/heads/main",
        "BACKEND_IMAGE": BACKEND_IMAGE, "WEB_IMAGE": WEB_IMAGE,
    })
    return environment


class PublishPolicyTests(unittest.TestCase):
    def test_workflow_requires_successful_main_ci_from_this_repository(self):
        workflow = yaml.load((ROOT / ".github/workflows/deploy.yml").read_text(), Loader=yaml.BaseLoader)
        self.assertEqual({"workflow_run"}, set(workflow["on"]))
        self.assertEqual({
            "workflows": ["CI"], "types": ["completed"], "branches": ["main"],
        }, workflow["on"]["workflow_run"])
        release = workflow["jobs"]["release"]
        for condition in [
            "github.event.workflow_run.conclusion == 'success'",
            "github.event.workflow_run.head_branch == 'main'",
            "github.event.workflow_run.head_repository.full_name == github.repository",
            "(github.event.workflow_run.event == 'push' || github.event.workflow_run.event == 'workflow_dispatch')",
        ]:
            self.assertIn(condition, release["if"])
        tested_sha = "${{ github.event.workflow_run.head_sha }}"
        self.assertEqual(tested_sha, release["steps"][0]["with"]["ref"])
        self.assertEqual(tested_sha, release["env"]["SOURCE_SHA"])
        self.assertEqual("${{ github.event.workflow_run.head_branch }}", release["env"]["SOURCE_BRANCH"])
        builds = [step for step in release["steps"] if step.get("uses", "").startswith("docker/build-push-action@")]
        self.assertEqual(2, len(builds))
        for step in builds:
            self.assertIn("${{ env.SOURCE_SHA }}", step["with"]["tags"])
            self.assertIn("org.opencontainers.image.revision=${{ env.SOURCE_SHA }}", step["with"]["labels"])
        self.assertNotIn("${{ github.sha }}", (ROOT / ".github/workflows/deploy.yml").read_text())

    def test_argocd_requires_manual_sync(self):
        application = yaml.safe_load((ROOT / "deploy/argocd/application.yaml").read_text())
        policy = application["spec"]["syncPolicy"]["automated"]
        for key in ["enabled", "selfHeal", "prune"]:
            self.assertIs(False, policy[key])

    def run_publish(self, event=None, checkout_sha=SOURCE_SHA, **overrides):
        with tempfile.TemporaryDirectory(prefix="budgetmatch-publish-policy-") as directory:
            path = Path(directory)
            git_log = path / "git-calls"
            fake_git = path / "git"
            fake_git.write_text(
                '#!/bin/sh\n'
                'printf "%s\\n" "$*" >> "$GIT_TEST_LOG"\n'
                'if [ "$1" = rev-parse ]; then\n'
                '  printf "%s\\n" "$GIT_TEST_CHECKOUT_SHA"\n'
                '  exit 0\n'
                'fi\n'
                'if [ "$1" = ls-remote ]; then\n'
                '  printf "%s\\trefs/heads/main\\n" "$GIT_TEST_REMOTE_SHA"\n'
                '  exit 0\n'
                'fi\n'
                'exit 91\n'
            )
            fake_git.chmod(0o755)
            event_path = path / "event.json"
            event_path.write_text(json.dumps(ci_event() if event is None else event))
            environment = publish_environment(event_path)
            environment.update({
                "PATH": str(path) + os.pathsep + os.environ["PATH"],
                "GIT_TEST_LOG": str(git_log), "GIT_TEST_CHECKOUT_SHA": checkout_sha,
                "GIT_TEST_REMOTE_SHA": "b" * 40,
            })
            environment.update(overrides)
            result = subprocess.run(
                ["bash", str(ROOT / "scripts/deploy/publish.sh")],
                env=environment, capture_output=True, text=True, check=False, timeout=10,
            )
            return result, git_log.read_text() if git_log.exists() else ""

    def test_direct_triggers_and_non_main_sources_are_rejected_before_git_access(self):
        cases = [{"SOURCE_BRANCH": "refactor/agent"}]
        cases += [{"GITHUB_EVENT_NAME": name} for name in ["push", "pull_request", "workflow_dispatch", "schedule", ""]]
        for overrides in cases:
            with self.subTest(overrides=overrides):
                result, calls = self.run_publish(**overrides)
                self.assertEqual(1, result.returncode)
                self.assertIn("Only a successful main CI workflow_run", result.stderr)
                self.assertEqual("", calls)

    def test_unsuccessful_untrusted_or_mismatched_ci_runs_are_rejected(self):
        cases = [
            {"conclusion": conclusion} for conclusion in ["failure", "cancelled", "skipped", None]
        ] + [
            {"status": "in_progress"}, {"name": "Other Workflow"},
            {"event": "pull_request"}, {"event": "pull_request_target"}, {"event": "schedule"},
            {"head_branch": "refactor/agent"},
            {"head_repository": {"full_name": "untrusted/fork"}}, {"head_sha": "b" * 40},
        ]
        for overrides in cases:
            with self.subTest(overrides=overrides):
                result, calls = self.run_publish(ci_event(**overrides))
                self.assertEqual(1, result.returncode)
                self.assertIn("must match SOURCE_SHA", result.stderr)
                self.assertEqual("", calls)

    def test_missing_or_incomplete_events_are_rejected(self):
        for event in [{}, {"workflow_run": {}}, {**ci_event(), "action": "requested"}]:
            with self.subTest(event=event):
                result, calls = self.run_publish(event)
                self.assertEqual(1, result.returncode)
                self.assertEqual("", calls)
        result, calls = self.run_publish(GITHUB_EVENT_PATH="/nonexistent/budgetmatch-event.json")
        self.assertEqual(1, result.returncode)
        self.assertEqual("", calls)

    def test_checkout_must_match_the_tested_commit(self):
        result, calls = self.run_publish(checkout_sha="b" * 40)
        self.assertEqual(1, result.returncode)
        self.assertIn("checked-out source does not match", result.stderr)
        self.assertEqual("rev-parse HEAD\n", calls)

    def test_successful_main_ci_skips_an_outdated_commit(self):
        for event in ["push", "workflow_dispatch"]:
            with self.subTest(event=event):
                result, calls = self.run_publish(ci_event(event=event))
                self.assertEqual(0, result.returncode, result.stderr)
                self.assertIn("A newer source commit exists", result.stdout)
                self.assertEqual("rev-parse HEAD\nls-remote --exit-code origin refs/heads/main\n", calls)


class PublishIntegrationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="budgetmatch-publish-integration-")
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        self.source = self.directory / "source"
        self.remote = self.directory / "remote.git"
        self.source.mkdir()
        files = [
            "scripts/deploy/publish.sh", "scripts/deploy/render.py", "deploy/environments/vps.yaml",
            "web-ui/nginx/default.conf", "cmd/app/etc/config.yaml", "cmd/admin/etc/config.yaml",
        ] + [f"services/rpc/{service}/etc/config.yaml" for service in ["auth", "mall", "seckill", "agent", "payment"]]
        for name in files:
            target = self.source / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / name, target)
        self.git(self.directory, "init", "--bare", "--initial-branch=main", str(self.remote))
        self.git(self.source, "init", "--initial-branch=main")
        self.git(self.source, "config", "user.name", "CI Test")
        self.git(self.source, "config", "user.email", "ci-test@example.invalid")
        self.git(self.source, "config", "commit.gpgsign", "false")
        self.git(self.source, "config", "core.hooksPath", "/dev/null")
        self.git(self.source, "add", ".")
        self.git(self.source, "commit", "-m", "source")
        self.git(self.source, "remote", "add", "origin", str(self.remote))
        self.git(self.source, "push", "origin", "main")
        self.sha = self.git(self.source, "rev-parse", "HEAD")

    def git(self, cwd, *args):
        result = subprocess.run(
            ["git", *args], cwd=cwd, capture_output=True, text=True, check=False, timeout=15,
        )
        self.assertEqual(0, result.returncode, result.stderr)
        return result.stdout.strip()

    def publish(self, **overrides):
        event_path = self.directory / "event.json"
        event_path.write_text(json.dumps(ci_event(self.sha)))
        environment = publish_environment(event_path, self.sha)
        environment["GITHUB_STEP_SUMMARY"] = str(self.directory / "summary")
        environment.update(overrides)
        return subprocess.run(
            ["bash", str(self.source / "scripts/deploy/publish.sh")],
            env=environment, capture_output=True, text=True, check=False, timeout=20,
        )

    def test_publish_records_tested_source_and_digests_and_is_idempotent(self):
        result = self.publish()
        self.assertEqual(0, result.returncode, result.stderr)
        revision = self.git(self.remote, "rev-parse", "gitops")
        self.assertEqual("apps.yaml\nrelease.json", self.git(self.remote, "ls-tree", "--name-only", "gitops"))
        release = json.loads(self.git(self.remote, "show", "gitops:release.json"))
        self.assertEqual({
            "source_sha": self.sha, "source_branch": "main",
            "backend_image": BACKEND_IMAGE, "web_image": WEB_IMAGE,
        }, release)
        documents = list(yaml.safe_load_all(self.git(self.remote, "show", "gitops:apps.yaml")))
        deployments = [document for document in documents if document["kind"] == "Deployment"]
        self.assertEqual(8, len(deployments))
        for deployment in deployments:
            expected = WEB_IMAGE if deployment["metadata"]["name"] == "web-ui" else BACKEND_IMAGE
            self.assertEqual(expected, deployment["spec"]["template"]["spec"]["containers"][0]["image"])
        self.assertIn(revision, (self.directory / "summary").read_text())
        self.assertIn("Sync it manually", result.stdout)
        repeated = self.publish()
        self.assertEqual(0, repeated.returncode, repeated.stderr)
        self.assertIn("already published", repeated.stdout)
        self.assertEqual(revision, self.git(self.remote, "rev-parse", "gitops"))
        self.assertEqual(self.sha, self.git(self.remote, "rev-parse", "main"))

    def test_new_candidate_keeps_previous_gitops_revision_in_history(self):
        first = self.publish()
        self.assertEqual(0, first.returncode, first.stderr)
        previous = self.git(self.remote, "rev-parse", "gitops")
        self.git(self.source, "commit", "--allow-empty", "-m", "new source")
        self.git(self.source, "push", "origin", "main")
        self.sha = self.git(self.source, "rev-parse", "HEAD")
        result = self.publish()
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(previous, self.git(self.remote, "rev-parse", "gitops^"))
        release = json.loads(self.git(self.remote, "show", "gitops:release.json"))
        self.assertEqual(self.sha, release["source_sha"])

    def test_invalid_images_do_not_create_a_candidate_branch(self):
        result = self.publish(BACKEND_IMAGE="ghcr.io/example/backend:latest")
        self.assertNotEqual(0, result.returncode)
        self.assertIn("immutable", result.stderr)
        self.assertEqual("main", self.git(self.remote, "for-each-ref", "--format=%(refname:short)", "refs/heads"))


if __name__ == "__main__":
    unittest.main()
