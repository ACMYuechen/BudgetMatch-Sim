"""Offline datasource wiring checks; never read the private .env or contact a DB."""

from pathlib import Path
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]
RPCS = ("auth", "mall", "seckill", "payment", "agent")


class DataConfigTests(unittest.TestCase):
    def test_host_services_consume_datasource_environment(self):
        for service in RPCS:
            config = yaml.safe_load((ROOT / f"services/rpc/{service}/etc/config.yaml").read_text())
            with self.subTest(service=service):
                self.assertEqual("${DATABASE_DSN}", config["Database"]["DSN"])
                self.assertEqual("${REDIS_ADDRESS}", config["CacheRedis"]["Address"])
                self.assertEqual("${REDIS_PASSWORD}", config["CacheRedis"]["Password"])
        app = yaml.safe_load((ROOT / "cmd/app/etc/config.yaml").read_text())
        self.assertEqual("${REDIS_ADDRESS}", app["Redis"]["Address"])
        self.assertEqual("${REDIS_PASSWORD}", app["Redis"]["Password"])

    def test_compose_services_keep_container_network_addresses(self):
        services = yaml.safe_load((ROOT / "docker-compose.yml").read_text())["services"]
        for name in [*(name + "-rpc" for name in RPCS), "app"]:
            env = dict(line.split("=", 1) for line in services[name]["environment"])
            with self.subTest(service=name):
                self.assertEqual("redis:6379", env["REDIS_ADDRESS"])
                self.assertNotIn("127.0.0.1", env.get("DATABASE_DSN", ""))
                if name != "app":
                    self.assertIn("host=postgres ", env["DATABASE_DSN"])
                    self.assertIn("port=5432 ", env["DATABASE_DSN"])

    def test_existing_images_and_host_ports_can_be_preserved(self):
        compose = yaml.safe_load((ROOT / "docker-compose.yml").read_text())
        postgres = compose["services"]["postgres"]
        self.assertEqual("${POSTGRES_IMAGE:-pgvector/pgvector:pg16}", postgres["image"])
        self.assertEqual(["127.0.0.1:${POSTGRES_PORT:-15432}:5432"], postgres["ports"])
        self.assertEqual(["127.0.0.1:${REDIS_PORT:-6379}:6379"], compose["services"]["redis"]["ports"])
        self.assertIn("postgres_data:/var/lib/postgresql/data", postgres["volumes"])
        self.assertIn("redis_data:/data", compose["services"]["redis"]["volumes"])

    def test_example_contains_only_the_local_application_database(self):
        env = dict(line.split("=", 1) for line in (ROOT / ".env.example").read_text().splitlines()
                   if line and not line.startswith("#") and "=" in line)
        self.assertEqual("127.0.0.1:6379", env["REDIS_ADDRESS"])
        self.assertIn("host=127.0.0.1 ", env["DATABASE_DSN"])
        self.assertIn("dbname=budgetmatch-sim ", env["DATABASE_DSN"])
        self.assertEqual({"DATABASE_DSN"}, {key for key in env if key.endswith("_DSN")})

    def test_postgres_tests_share_the_existing_ci_connection(self):
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text())
        self.assertIn("RAG_TEST_PG_DSN", workflow["jobs"]["go-check"]["env"])
        for source in (
            "services/rpc/mall/internal/testdb/postgres.go",
            "services/rpc/mall/model/product_index/snapshot_integration_test.go",
            "services/rpc/mall/model/product_index/candidates_integration_test.go",
        ):
            with self.subTest(source=source):
                text = (ROOT / source).read_text()
                self.assertIn('os.Getenv("RAG_TEST_PG_DSN")', text)
                self.assertNotIn('os.Getenv("DATABASE_DSN")', text)
                self.assertIn("t.Skip(", text)

    def test_retired_test_connection_is_not_used_by_code_or_scripts(self):
        retired_key = "BUDGETMATCH_TEST_" + "POSTGRES_DSN"
        for directory, pattern in (("services", "*.go"), ("scripts", "*.py"), ("scripts", "*.sh")):
            for path in (ROOT / directory).rglob(pattern):
                self.assertNotIn(retired_key, path.read_text(), str(path.relative_to(ROOT)))

    def test_host_dependencies_keep_loopback_and_broker_advertisement(self):
        services = yaml.safe_load((ROOT / "docker-compose.yml").read_text())["services"]
        for name in ("postgres", "redis", "etcd", "rocketmq-namesrv", "rocketmq-broker"):
            self.assertTrue(all(port.startswith("127.0.0.1:") for port in services[name]["ports"]), name)
        broker = services["rocketmq-broker"]
        self.assertIn("ROCKETMQ_BROKER_IP1=${ROCKETMQ_BROKER_IP1:-}", broker["environment"])
        self.assertIn('"$${ROCKETMQ_BROKER_IP1}"', broker["command"][-1])
        self.assertIn("etcd_data:/etcd-data", services["etcd"]["volumes"])
        self.assertIn("ETCD_DATA_DIR=/etcd-data", services["etcd"]["environment"])

    def test_production_remote_database_startup_settings(self):
        settings = yaml.safe_load((ROOT / "deploy/environments/vps.yaml").read_text())
        self.assertEqual("external-data", settings["dataSecret"])
        self.assertFalse(settings["services"]["mall-rpc"]["config"]["Database"]["AutoMigrate"])
        for service in ("app", "admin"):
            config = settings["services"][service]["config"]
            self.assertEqual(10000, config["AuthRpc"]["Timeout"])
            self.assertEqual(15000, config["MallRpc"]["Timeout"])
            self.assertEqual(10000, config["SeckillRpc"]["Timeout"])


if __name__ == "__main__":
    unittest.main()
