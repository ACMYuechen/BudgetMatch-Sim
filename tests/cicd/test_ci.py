import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]
CHECKS = ["GO", "WEB", "SECURITY", "CONTAINER"]
ALL_IMAGES = ["auth-rpc", "seckill-rpc", "mall-rpc", "agent-rpc", "payment-rpc", "app", "admin", "web-ui"]


class ChangeDetectionTests(unittest.TestCase):
    def detect(self, paths=None, event="pull_request"):
        with tempfile.TemporaryDirectory(prefix="budgetmatch-ci-selection-") as directory:
            path = Path(directory)
            changed = path / "changed-files"
            output = path / "output"
            environment = dict(os.environ)
            environment.update({
                "CI_EVENT_NAME": event,
                "GITHUB_OUTPUT": str(output),
                "GITHUB_STEP_SUMMARY": str(path / "summary"),
            })
            environment.pop("CI_CHANGED_FILES_FILE", None)
            if paths is not None:
                changed.write_bytes(b"".join(filename.encode() + b"\0" for filename in paths))
                environment["CI_CHANGED_FILES_FILE"] = str(changed)
            result = subprocess.run(
                [str(ROOT / "scripts/ci/detect-changes.sh")], cwd=directory,
                env=environment, capture_output=True, text=True, check=False, timeout=10,
            )
            self.assertEqual(0, result.returncode, result.stderr)
            return dict(line.split("=", 1) for line in output.read_text().splitlines())

    def test_ci_cd_paths_still_select_all_checks(self):
        for path in [
            ".github/workflows/ci.yml", "scripts/ci/gate.sh", "tests/cicd/test_ci.py",
        ]:
            with self.subTest(path=path):
                output = self.detect([path])
                for check in CHECKS:
                    self.assertEqual("true", output[f"{check.lower()}_check"])
                self.assertEqual(ALL_IMAGES, json.loads(output["container_matrix"]))

    def test_documentation_does_not_select_expensive_checks(self):
        output = self.detect(["README.md", "docs/README.md", "docs/CONTRIBUTION.md", "docs/AGENT.md"])
        for check in CHECKS:
            self.assertEqual("false", output[f"{check.lower()}_check"])
        self.assertEqual([], json.loads(output["container_matrix"]))

    def test_regular_development_scripts_do_not_expand_to_full_ci(self):
        output = self.detect(["scripts/dev.sh"])
        self.assertEqual("true", output["security_check"])
        for check in ["go_check", "web_check", "container_check"]:
            self.assertEqual("false", output[check])

    def test_frontend_and_backend_selection_is_preserved(self):
        for path, images, go_check, web_check in [
            ("web-ui/src/App.tsx", ["web-ui"], "false", "true"),
            ("cmd/app/main.go", ["app"], "true", "false"),
            ("services/rpc/payment/main.go", ["payment-rpc"], "true", "false"),
        ]:
            with self.subTest(path=path):
                output = self.detect([path])
                self.assertEqual(images, json.loads(output["container_matrix"]))
                self.assertEqual(go_check, output["go_check"])
                self.assertEqual(web_check, output["web_check"])
                self.assertEqual("true", output["security_check"])
                self.assertEqual("true", output["container_check"])

    def test_manual_ci_run_still_selects_all_checks(self):
        output = self.detect(event="workflow_dispatch")
        self.assertEqual(ALL_IMAGES, json.loads(output["container_matrix"]))
        for check in CHECKS:
            self.assertEqual("true", output[f"{check.lower()}_check"])


class GateTests(unittest.TestCase):
    def gate(self, changes="success", **overrides):
        environment = dict(os.environ)
        environment["CHANGES_RESULT"] = changes
        for check in CHECKS:
            environment[f"{check}_REQUIRED"] = "false"
            environment[f"{check}_RESULT"] = "skipped"
        environment.update(overrides)
        return subprocess.run(
            [str(ROOT / "scripts/ci/gate.sh")], env=environment,
            capture_output=True, text=True, check=False, timeout=10,
        )

    def test_documentation_only_and_successful_selected_checks_pass(self):
        self.assertEqual(0, self.gate().returncode)
        self.assertEqual(0, self.gate(GO_REQUIRED="true", GO_RESULT="success").returncode)

    def test_unsuccessful_required_checks_fail(self):
        for check in CHECKS:
            for outcome in ["failure", "cancelled", "skipped"]:
                with self.subTest(check=check, outcome=outcome):
                    result = self.gate(**{f"{check}_REQUIRED": "true", f"{check}_RESULT": outcome})
                    self.assertEqual(1, result.returncode)

    def test_failed_selection_and_unexpected_jobs_fail(self):
        self.assertEqual(1, self.gate(changes="failure").returncode)
        self.assertEqual(1, self.gate(changes="cancelled").returncode)
        self.assertEqual(1, self.gate(WEB_RESULT="success").returncode)


class ReadinessTests(unittest.TestCase):
    def wait_for_etcd(self, health, curl_exit):
        with tempfile.TemporaryDirectory(prefix="budgetmatch-ci-readiness-") as directory:
            path = Path(directory)
            log = path / "curl-calls"
            curl = path / "curl"
            curl.write_text(
                '#!/bin/sh\n'
                'printf "call\\n" >> "$CI_TEST_CURL_LOG"\n'
                'printf "%s\\n" "$CI_TEST_HEALTH"\n'
                'exit "$CI_TEST_CURL_EXIT"\n'
            )
            sleep = path / "sleep"
            sleep.write_text('#!/bin/sh\nexit 0\n')
            curl.chmod(0o755)
            sleep.chmod(0o755)
            environment = dict(os.environ)
            environment.update({
                "PATH": str(path) + os.pathsep + os.environ["PATH"],
                "CI_TEST_CURL_LOG": str(log),
                "CI_TEST_HEALTH": health,
                "CI_TEST_CURL_EXIT": str(curl_exit),
            })
            result = subprocess.run(
                [str(ROOT / "scripts/ci/wait-etcd.sh")], env=environment,
                capture_output=True, text=True, check=False, timeout=10,
            )
            return result, len(log.read_text().splitlines())

    def test_healthy_etcd_does_not_wait(self):
        result, attempts = self.wait_for_etcd('{"health":"true"}', 0)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(1, attempts)

    def test_unhealthy_or_unreachable_etcd_fails(self):
        for health, curl_exit in [('{"health":"false"}', 0), ("", 7)]:
            with self.subTest(health=health, curl_exit=curl_exit):
                result, attempts = self.wait_for_etcd(health, curl_exit)
                self.assertEqual(1, result.returncode)
                self.assertIn("etcd did not become healthy", result.stderr)
                self.assertEqual(31, attempts)


class WorkflowLayoutTests(unittest.TestCase):
    def workflow(self, name):
        return yaml.load((ROOT / f".github/workflows/{name}.yml").read_text(), Loader=yaml.BaseLoader)

    def test_workflow_script_paths_exist_and_have_a_checkout(self):
        for name in ["ci"]:
            for job_name, job in self.workflow(name)["jobs"].items():
                checked_out = False
                for step in job["steps"]:
                    if step.get("uses", "").startswith("actions/checkout@"):
                        checked_out = True
                    for path in re.findall(r"[.\w/-]+\.(?:sh|py)\b", step.get("run", "")):
                        with self.subTest(workflow=name, job=job_name, path=path):
                            self.assertTrue(checked_out, "local scripts require checkout first")
                            self.assertTrue((ROOT / path).is_file(), f"missing script: {path}")
                            if step["run"].strip() == path:
                                self.assertTrue(os.access(ROOT / path, os.X_OK))
                    if step.get("uses", "").startswith("docker/build-push-action@"):
                        self.assertTrue((ROOT / step["with"]["file"]).is_file())
                        self.assertTrue((ROOT / step["with"]["context"]).is_dir())

    def test_ci_entry_points_and_required_check_names_are_preserved(self):
        workflow = self.workflow("ci")
        self.assertEqual({"push", "pull_request", "workflow_dispatch"}, set(workflow["on"]))
        self.assertEqual(["main"], workflow["on"]["push"]["branches"])
        self.assertEqual("CI", workflow["name"])
        self.assertEqual("CI Gate", workflow["jobs"]["ci-gate"]["name"])
        self.assertEqual(
            {"changes", "go-check", "web-check", "security-check", "container-prepare", "container-check", "ci-gate"},
            set(workflow["jobs"]),
        )


if __name__ == "__main__":
    unittest.main()
