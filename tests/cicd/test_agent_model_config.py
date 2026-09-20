"""Offline Agent model-config rendering checks; no .env, kubectl or provider I/O."""

from pathlib import Path
import sys
import unittest

import yaml

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "scripts/deploy"))

import render


class AgentModelConfigTests(unittest.TestCase):
    def setUp(self):
        self.settings = yaml.safe_load(
            (render.ROOT / "deploy/environments/vps.yaml").read_text()
        )

    def documents(self):
        return render.render(
            render.ROOT, self.settings,
            "local/backend@sha256:" + "1" * 64,
            "local/web@sha256:" + "2" * 64,
        )

    def agent_resources(self):
        documents = self.documents()
        config_map = next(d for d in documents if d["kind"] == "ConfigMap"
                          and d["metadata"]["name"] == "agent-rpc-config")
        deployment = next(d for d in documents if d["kind"] == "Deployment"
                          and d["metadata"]["name"] == "agent-rpc")
        return config_map, deployment

    def test_thinking_is_optional_but_credentials_remain_required(self):
        config_map, deployment = self.agent_resources()
        config = yaml.safe_load(config_map["data"]["config.yaml"])
        self.assertEqual(config["Model"]["Thinking"], "${LLM_THINKING}")
        env = deployment["spec"]["template"]["spec"]["containers"][0]["env"]
        refs = {e["name"]: e["valueFrom"]["secretKeyRef"] for e in env if "valueFrom" in e}
        self.assertEqual(refs["LLM_THINKING"], {
            "name": self.settings["runtimeSecret"], "key": "LLM_THINKING", "optional": True,
        })
        self.assertIn("LLM_API_KEY", refs)
        for name, ref in refs.items():
            if name != "LLM_THINKING":
                self.assertFalse(ref.get("optional", False), name)

    def test_other_services_do_not_receive_thinking(self):
        for document in self.documents():
            if document["kind"] != "Deployment" or document["metadata"]["name"] == "agent-rpc":
                continue
            container = document["spec"]["template"]["spec"]["containers"][0]
            self.assertNotIn("LLM_THINKING", [e["name"] for e in container.get("env", [])])

    def test_explicit_override_is_rendered_and_changes_checksum(self):
        _, before = self.agent_resources()
        self.settings["services"]["agent-rpc"]["config"]["Model"] = {
            "Model": "deepseek-flash", "Thinking": "disabled",
        }
        config_map, after = self.agent_resources()
        config = yaml.safe_load(config_map["data"]["config.yaml"])
        self.assertEqual(config["Model"]["Thinking"], "disabled")
        self.assertEqual(config["Model"]["APIKey"], "${LLM_API_KEY}")
        env = after["spec"]["template"]["spec"]["containers"][0]["env"]
        self.assertNotIn("LLM_THINKING", [e["name"] for e in env])
        self.assertNotEqual(
            before["spec"]["template"]["metadata"]["annotations"]["checksum/config"],
            after["spec"]["template"]["metadata"]["annotations"]["checksum/config"],
        )

    def test_compose_passes_thinking_without_changing_default(self):
        compose = yaml.safe_load((render.ROOT / "docker-compose.yml").read_text())
        environments = [service.get("environment", []) for service in compose["services"].values()]
        model_env = next(env for env in environments if "LLM_MODEL=${LLM_MODEL:-}" in env)
        self.assertIn("LLM_THINKING=${LLM_THINKING:-}", model_env)
        self.assertIn("LLM_API_KEY=${LLM_API_KEY:-}", model_env)

    def test_example_uses_explicit_flash_profile_without_credentials(self):
        # Deliberately read the tracked example, never the private .env.
        example = dict(line.split("=", 1) for line in
                       (render.ROOT / ".env.example").read_text().splitlines()
                       if line.startswith("LLM_"))
        self.assertEqual(example["LLM_PROVIDER"], "openai")
        self.assertEqual(example["LLM_MODEL"], "deepseek-flash")
        self.assertEqual(example["LLM_THINKING"], "disabled")
        self.assertEqual(example["LLM_API_KEY"], "")


if __name__ == "__main__":
    unittest.main()
