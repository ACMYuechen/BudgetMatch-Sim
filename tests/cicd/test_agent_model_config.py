"""Local Compose and model example contracts; no provider calls."""

from pathlib import Path
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]


class AgentModelConfigTests(unittest.TestCase):
    def test_compose_passes_thinking_without_changing_default(self):
        compose = yaml.safe_load((ROOT / "docker-compose.yml").read_text())
        environments = [service.get("environment", []) for service in compose["services"].values()]
        model_env = next(env for env in environments if "LLM_MODEL=${LLM_MODEL:-}" in env)
        self.assertIn("LLM_THINKING=${LLM_THINKING:-}", model_env)
        self.assertIn("LLM_API_KEY=${LLM_API_KEY:-}", model_env)
        self.assertIn("EMBEDDING_DIMENSIONS=${EMBEDDING_DIMENSIONS:-1536}", model_env)


    def test_embedding_example_preserves_disabled_legacy_default(self):
        example = dict(line.split("=", 1) for line in
                       (ROOT / ".env.example").read_text().splitlines()
                       if line.startswith("EMBEDDING_"))
        self.assertEqual(example["EMBEDDING_PROVIDER"], "")
        self.assertEqual(example["EMBEDDING_MODEL"], "text-embedding-3-small")
        self.assertEqual(example["EMBEDDING_DIMENSIONS"], "1536")
        self.assertEqual(example["EMBEDDING_API_KEY"], "")


    def test_example_uses_explicit_flash_profile_without_credentials(self):
        # Deliberately read the tracked example, never the private .env.
        example = dict(line.split("=", 1) for line in
                       (ROOT / ".env.example").read_text().splitlines()
                       if line.startswith("LLM_"))
        self.assertEqual(example["LLM_PROVIDER"], "openai")
        self.assertEqual(example["LLM_MODEL"], "deepseek-flash")
        self.assertEqual(example["LLM_THINKING"], "disabled")
        self.assertEqual(example["LLM_API_KEY"], "")



if __name__ == "__main__":
    unittest.main()
