import base64
import copy
import importlib.util
from pathlib import Path
import tempfile
import unittest

import yaml

import render


BACKEND = "ghcr.io/example/budgetmatch-backend@sha256:" + "1" * 64
WEB = "ghcr.io/example/budgetmatch-web@sha256:" + "2" * 64
SEAL_SPEC = importlib.util.spec_from_file_location("seal_alipay", Path(__file__).with_name("seal-alipay.py"))
seal_alipay = importlib.util.module_from_spec(SEAL_SPEC)
SEAL_SPEC.loader.exec_module(seal_alipay)


def sealed_secret():
    return {
        "apiVersion": "bitnami.com/v1alpha1", "kind": "SealedSecret",
        "metadata": {"name": "alipay", "namespace": "budgetmatch-sim"},
        "spec": {"encryptedData": {
            key: base64.b64encode(b"ciphertext" * 64).decode() for key in render.ALIPAY_KEYS
        }},
    }


class RenderTests(unittest.TestCase):
    def setUp(self):
        self.settings = yaml.safe_load((render.ROOT / "deploy/vps.yaml").read_text())

    def resources(self, sealed=None):
        return {
            (item["kind"], item["metadata"]["name"]): item
            for item in render.render(render.ROOT, self.settings, BACKEND, WEB, sealed)
        }

    def test_only_existing_application_resources_are_managed(self):
        documents = self.resources()
        self.assertEqual(24, len(documents))
        for kind, name in documents:
            self.assertIn(kind, {"ConfigMap", "Service", "Deployment"})
            self.assertIn(name.removesuffix("-config"), {*render.SERVICES, "web-ui"})
            self.assertEqual("budgetmatch-sim", documents[kind, name]["metadata"]["namespace"])

    def test_existing_selectors_and_storage_are_preserved(self):
        documents = self.resources()
        for name in [*render.SERVICES, "web-ui"]:
            deployment = documents["Deployment", name]
            self.assertEqual({"app": name}, deployment["spec"]["selector"]["matchLabels"])
            self.assertEqual({"app": name}, documents["Service", name]["spec"]["selector"])
            pod = deployment["spec"]["template"]["spec"]
            self.assertFalse(any("hostPath" in volume for volume in pod["volumes"]))
            self.assertEqual("IfNotPresent", pod["containers"][0]["imagePullPolicy"])
        agent = documents["Deployment", "agent-rpc"]["spec"]["template"]["spec"]
        self.assertIn({"name": "workspace", "persistentVolumeClaim": {"claimName": "agent-workspace"}}, agent["volumes"])
        self.assertEqual(8080, documents["Service", "web-ui"]["spec"]["ports"][0]["port"])

    def test_configs_use_server_addresses_and_secret_references(self):
        documents = self.resources()
        for name in render.SERVICES:
            text = documents["ConfigMap", f"{name}-config"]["data"]["config.yaml"]
            self.assertNotIn("127.0.0.1", text)
            self.assertNotIn("123456", text)
            pod = documents["Deployment", name]["spec"]["template"]["spec"]
            for setting in pod["containers"][0]["env"]:
                if "valueFrom" in setting:
                    self.assertEqual("runtime", setting["valueFrom"]["secretKeyRef"]["name"])
        app = yaml.safe_load(documents["ConfigMap", "app-config"]["data"]["config.yaml"])
        self.assertEqual(60000, app["AgentRpc"]["Timeout"])
        self.assertEqual(["payment-rpc:10007"], app["PaymentRpc"]["Endpoints"])

    def test_config_changes_restart_only_affected_workload(self):
        before = self.resources()
        self.settings["services"]["app"]["config"]["AgentRpc"]["Timeout"] = 70000
        after = self.resources()
        for name in render.SERVICES:
            old = before["Deployment", name]["spec"]["template"]["metadata"]["annotations"]
            new = after["Deployment", name]["spec"]["template"]["metadata"]["annotations"]
            self.assertEqual(name != "app", old == new)

    def test_alipay_secret_is_isolated_and_rollout_is_automatic(self):
        encrypted = sealed_secret()
        original = copy.deepcopy(encrypted)
        before = self.resources(encrypted)
        self.assertEqual(original, encrypted)
        encrypted["spec"]["encryptedData"]["ALIPAY_APP_ID"] = base64.b64encode(b"changed" * 64).decode()
        after = self.resources(encrypted)
        self.assertEqual("-1", before["SealedSecret", "alipay"]["metadata"]["annotations"]["argocd.argoproj.io/sync-wave"])
        for name in render.SERVICES:
            template = before["Deployment", name]["spec"]["template"]
            for setting in template["spec"]["containers"][0]["env"]:
                if "valueFrom" not in setting:
                    continue
                expected = "alipay" if name == "payment-rpc" and setting["name"] in render.ALIPAY_KEYS else "runtime"
                self.assertEqual(expected, setting["valueFrom"]["secretKeyRef"]["name"])
            old_annotations = template["metadata"]["annotations"]
            new_annotations = after["Deployment", name]["spec"]["template"]["metadata"]["annotations"]
            self.assertEqual(name != "payment-rpc", old_annotations == new_annotations)

    def test_literal_credentials_in_overrides_are_rejected(self):
        for field in ["Password", "Secret", "APIKey", "PrivateKey", "DSN"]:
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "literal credential"):
                render.check_secret_literals({field: "not-a-reference"})

    def test_mutable_image_tags_are_rejected(self):
        with self.assertRaisesRegex(ValueError, "immutable"):
            render.render(render.ROOT, self.settings, "backend:latest", WEB)

    def test_plaintext_secrets_and_templates_are_rejected(self):
        for key in ["data", "stringData"]:
            encrypted = sealed_secret()
            encrypted[key] = {"ALIPAY_PRIVATE_KEY": "not-a-secret"}
            with self.subTest(key=key), self.assertRaises(ValueError):
                render.validate_sealed_secret(encrypted, "budgetmatch-sim")
        encrypted = sealed_secret()
        encrypted["spec"]["template"] = {"data": {"key": "not-a-secret"}}
        with self.assertRaises(ValueError):
            render.validate_sealed_secret(encrypted, "budgetmatch-sim")

    def test_wrong_namespace_broad_scope_and_short_ciphertext_are_rejected(self):
        encrypted = sealed_secret()
        with self.assertRaises(ValueError):
            render.validate_sealed_secret(encrypted, "other-namespace")
        encrypted["metadata"]["annotations"] = {"sealedsecrets.bitnami.com/cluster-wide": "true"}
        with self.assertRaises(ValueError):
            render.validate_sealed_secret(encrypted, "budgetmatch-sim")
        encrypted = sealed_secret()
        encrypted["spec"]["encryptedData"]["ALIPAY_APP_ID"] = "cGxhaW50ZXh0"
        with self.assertRaises(ValueError):
            render.validate_sealed_secret(encrypted, "budgetmatch-sim")

    def test_private_registry_pull_secret_is_applied_to_every_deployment(self):
        self.settings["imagePullSecrets"] = ["ghcr"]
        for (kind, _), document in self.resources().items():
            if kind == "Deployment":
                self.assertEqual([{"name": "ghcr"}], document["spec"]["template"]["spec"]["imagePullSecrets"])

    def test_argocd_cannot_manage_infrastructure_or_prune(self):
        application = yaml.safe_load((render.ROOT / "deploy/argocd/application.yaml").read_text())
        self.assertFalse(application["spec"]["syncPolicy"]["automated"]["prune"])
        self.assertNotIn("finalizers", application["metadata"])
        self.assertEqual("gitops", application["spec"]["source"]["targetRevision"])
        project = yaml.safe_load((render.ROOT / "deploy/argocd/project.yaml").read_text())
        self.assertEqual([], project["spec"]["clusterResourceWhitelist"])
        self.assertNotIn("Secret", {item["kind"] for item in project["spec"]["namespaceResourceWhitelist"]})


class SealTests(unittest.TestCase):
    def values(self):
        return {
            "ALIPAY_APP_ID": "2026000000000001", "ALIPAY_SELLER_ID": "2088000000000001",
            "ALIPAY_PRIVATE_KEY": "private-test-value", "ALIPAY_PUBLIC_KEY": "public-test-value",
            "ALIPAY_NOTIFY_URL": "http://51.79.164.39:8080/api/pay/notify/alipay",
            "DATABASE_DSN": "must-not-be-copied", "JWT_SECRET": "must-not-be-copied",
        }

    def test_only_alipay_settings_are_selected(self):
        secret = seal_alipay.alipay_secret(self.values(), "budgetmatch-sim")
        self.assertEqual(render.ALIPAY_KEYS, set(secret["data"]))
        self.assertEqual("", secret["data"]["ALIPAY_RETURN_URL"])

    def test_merchant_pid_is_not_accepted_as_app_id(self):
        values = self.values()
        values["ALIPAY_APP_ID"] = values["ALIPAY_SELLER_ID"]
        with self.assertRaisesRegex(ValueError, "sandbox application AppID"):
            seal_alipay.alipay_secret(values, "budgetmatch-sim")

    def test_invalid_callback_url_is_rejected(self):
        values = self.values()
        for address in ["/api/notify", "file:///tmp/key", "https://user:password@example.com"]:
            values["ALIPAY_NOTIFY_URL"] = address
            with self.subTest(address=address), self.assertRaises(ValueError):
                seal_alipay.alipay_secret(values, "budgetmatch-sim")

    def test_env_parser_preserves_quoted_values_without_execution(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / ".env"
            path.write_text('export APP="with spaces"\nEMPTY=\nKEY="line1\nline2"\nLITERAL=$(no-execution)\n')
            self.assertEqual({
                "APP": "with spaces", "EMPTY": "", "KEY": "line1\nline2", "LITERAL": "$(no-execution)",
            }, seal_alipay.read_env(path))


if __name__ == "__main__":
    unittest.main()
