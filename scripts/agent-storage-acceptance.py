#!/usr/bin/env python3
"""Own fresh loopback-only stores, run Agent acceptance, stop them, retain evidence.

No downloads, sudo, package installation, .env loading, existing-target options,
database drops or recursive cleanup. See docs/agent.md for scope and prerequisites.
"""

import argparse
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import subprocess
import tempfile
import time
from urllib.parse import quote


REPO = Path(__file__).resolve().parents[1]
PACKAGE = "./services/rpc/agent/internal/storageacceptance"


def clean_environment():
    # Preserve original toolchain lookup/home, never repurpose HOME. No PG*,
    # service tokens, legacy test DSNs, model config or loader overrides leak in.
    allowed = ("PATH", "HOME", "GOPATH", "GOTOOLCHAIN", "GOPROXY", "GOSUMDB")
    return {**{k: os.environ[k] for k in allowed if k in os.environ},
            "LANG": "C.UTF-8", "TZ": "UTC", "GOMAXPROCS": "2"}


def free_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


class Acceptance:
    def __init__(self, args):
        self.args = args
        self.root = Path(tempfile.mkdtemp(prefix="budgetmatch-agent-storage-"))
        self.env = clean_environment()
        self.env["GOCACHE"] = str(args.go_cache)
        self.pg_port, self.redis_port = free_port(), free_port()
        while self.redis_port == self.pg_port:
            self.redis_port = free_port()
        self.run_id = secrets.token_hex(16)
        self.admin_password = secrets.token_hex(32)
        self.config = dict(run_id=self.run_id, postgres_address=f"127.0.0.1:{self.pg_port}",
                           postgres_password=secrets.token_hex(32),
                           redis_address=f"127.0.0.1:{self.redis_port}",
                           redis_password=secrets.token_hex(32))
        self.config_path = self.root / "acceptance.json"
        self.config_path.write_text(json.dumps(self.config))
        self.config_path.chmod(0o600)
        self.database = "agent_m62_" + self.run_id
        self.pg = self.redis = None
        self.children = []
        self.handles = []
        self.report = {"run_id": self.run_id, "stages": [], "result": "incomplete", "stopped": False}
        print(f"artifacts={self.root} run_id={self.run_id}", flush=True)

    def stage(self, name):
        self.report["stages"].append(name)
        print(name, flush=True)

    def execute(self, args, name, *, env=None, data=None, timeout=60, capture=False):
        # Commands and stdin are not logged: stdin may hold a generated password.
        with (self.root / (name + ".log")).open("ab") as log:
            result = subprocess.run([str(a) for a in args], cwd=REPO, env=env or self.env,
                                    input=data, stdout=subprocess.PIPE if capture else log,
                                    stderr=log, timeout=timeout, check=False)
        if result.returncode:
            raise RuntimeError(f"{name} failed; inspect its private log")
        return result.stdout if capture else b""

    def pg_environment(self, admin=False):
        return {**self.env, "PGPASSWORD": self.admin_password if admin else self.config["postgres_password"],
                "PGSSLMODE": "disable", "PGCONNECT_TIMEOUT": "2", "PGPASSFILE": "/dev/null"}

    def sql(self, statement, *, admin=False, database=None, name="sql", timeout=10):
        return self.execute([self.args.pg_bin / "psql", "-X", "-w", "-A", "-t", "-v", "ON_ERROR_STOP=1",
                             "-h", "127.0.0.1", "-p", self.pg_port, "-U", "m62_owner" if admin else "agent_m62",
                             "-d", database or self.database], name, env=self.pg_environment(admin),
                            data=statement.encode(), timeout=timeout, capture=True).decode().strip()

    def spawn(self, command, name, env=None):
        log = (self.root / (name + ".log")).open("ab")
        self.handles.append(log)
        proc = subprocess.Popen([str(a) for a in command], cwd=self.root, env=env or self.env,
                                stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
        self.children.append(proc)
        return proc

    @staticmethod
    def wait_ready(proc, check):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if proc.poll() is not None:
                raise RuntimeError("owned storage process exited before readiness; inspect server log")
            try:
                if check():
                    return
            except (RuntimeError, subprocess.TimeoutExpired):
                pass
            time.sleep(0.1)
        raise RuntimeError("owned storage process did not become ready")

    def initialize_pg(self, name):
        self.pg_data = self.root / name
        pwfile = self.root / "bootstrap-password"
        pwfile.write_text(self.admin_password + "\n")
        self.execute([self.args.pg_bin / "initdb", "-D", self.pg_data, "-U", "m62_owner",
                      "--pwfile", pwfile, "--auth-local=scram-sha-256", "--auth-host=scram-sha-256",
                      "--encoding=UTF8", "--locale=C"], name + "-init")
        self.start_pg()
        self.sql(f"CREATE ROLE agent_m62 LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD '{self.config['postgres_password']}';\n"
                 f"CREATE DATABASE {self.database} OWNER agent_m62;\n", admin=True, database="postgres", name=name + "-role")
        self.sql("CREATE EXTENSION vector;", admin=True, name=name + "-vector")

    def start_pg(self):
        self.pg = self.spawn([self.args.pg_bin / "postgres", "-D", self.pg_data, "-h", "127.0.0.1",
                              "-p", self.pg_port, "-k", "", "-N", "20", "-B", "2048",
                              "-c", "fsync=on", "-c", "synchronous_commit=on", "-c", "full_page_writes=on",
                              "-c", "log_min_error_statement=panic"], self.pg_data.name + "-server")
        self.wait_ready(self.pg, lambda: self.sql("SHOW data_directory;", admin=True, database="postgres", name="pg-ready") == str(self.pg_data))

    def redis_environment(self):
        env = {**self.env, "REDISCLI_AUTH": self.config["redis_password"]}
        if self.args.redis_lib:
            env["LD_LIBRARY_PATH"] = str(self.args.redis_lib)
        return env

    def redis_command(self, *command):
        return self.execute([self.args.redis_server.parent / "redis-cli", "--raw", "-h", "127.0.0.1",
                             "-p", self.redis_port, *command], "redis-client", env=self.redis_environment(),
                            timeout=5, capture=True).decode().strip()

    def initialize_redis(self):
        data = self.root / "redis-data"
        data.mkdir()
        self.redis_config = self.root / "redis.conf"
        self.redis_config.write_text(f"bind 127.0.0.1\nport {self.redis_port}\nprotected-mode yes\ndaemonize no\n"
                                     f"dir {json.dumps(str(data))}\npidfile {json.dumps(str(self.root / 'redis.pid'))}\n"
                                     f"requirepass {self.config['redis_password']}\nappendonly yes\nappendfsync always\n"
                                     'save ""\nlogfile ""\n')
        self.start_redis()
        if self.redis_command("SET", "budgetmatch:agent:m62:owner", self.run_id) != "OK":
            raise RuntimeError("failed to initialize owned Redis marker")

    def start_redis(self):
        self.redis = self.spawn([self.args.redis_server, self.redis_config], "redis-server", self.redis_environment())
        self.wait_ready(self.redis, lambda: f"process_id:{self.redis.pid}" in self.redis_command("INFO", "server").splitlines())

    def go_tests(self, name, test, *, repeat=1, recovery=None):
        env = {**self.env, "AGENT_STORAGE_ACCEPTANCE_CONFIG": str(self.config_path),
               "AGENT_STORAGE_ACCEPTANCE_RUN_ID": self.run_id}
        if recovery:
            env["AGENT_STORAGE_ACCEPTANCE_RECOVERY"] = recovery
        self.stage(name)
        # Sequential Go jobs only; the outer timeout also bounds the tool itself.
        self.execute([self.args.go, "test", "-p", "1", "-race", "-count", repeat,
                      "-timeout=180s", "-json", "-run", "^" + test + "$", PACKAGE],
                     name, env=env, timeout=300)
        events = [json.loads(line) for line in (self.root / (name + ".log")).read_text().splitlines()]
        if any(e.get("Action") in ("fail", "skip") for e in events):
            raise RuntimeError(f"{name}: real acceptance cannot contain failures or skips")
        if sum(e.get("Action") == "pass" and e.get("Test") == test for e in events) != repeat:
            raise RuntimeError(f"{name}: requested test did not actually run")
        self.report.setdefault("tests", {})[name] = sum(e.get("Action") == "pass" and bool(e.get("Test")) for e in events)

    def legacy_integration_checks(self):
        # Legacy helpers drop vector tables / their synthetic schemas. Give them
        # a SEPARATE newly created database, never the recovery dataset or a DSN
        # inherited from the user's shell. Scope is Agent memory/index + Mall reads.
        name = "isolated-existing-integration-tests"
        self.stage(name)
        database = "agent_m62_" + secrets.token_hex(16)
        self.sql(f"CREATE DATABASE {database} OWNER agent_m62;", admin=True, database="postgres", name="legacy-create")
        self.sql("CREATE EXTENSION vector;", admin=True, database=database, name="legacy-vector")
        dsn = f"postgresql://agent_m62:{quote(self.config['postgres_password'], safe='')}@127.0.0.1:{self.pg_port}/{database}?sslmode=disable"
        env = {**self.env, **{key: dsn for key in ("AGENT_MEMORY_TEST_PG_DSN", "RAG_TEST_PG_DSN")}}
        tests = ["TestPostgresConversationTurnPersistence", "TestPgVectorSyncPublicationRollback", "TestPgVectorRoundTrip",
                 "TestPgVectorSessionExclusionAndLostOwner", "TestPostgresCatalogSnapshotConsistency",
                 "TestPostgresCandidateChecksObserveCommittedChanges"]
        self.execute([self.args.go, "test", "-p", "1", "-race", "-count=1", "-timeout=120s", "-json",
                      "-run", "^(" + "|".join(tests) + ")$", "./services/rpc/agent/internal/memory",
                      "./services/rpc/agent/model/product_vectors", "./services/rpc/mall/model/product_index"],
                     name, env=env, timeout=300)
        events = [json.loads(line) for line in (self.root / (name + ".log")).read_text().splitlines()]
        if any(e.get("Action") in ("fail", "skip") for e in events):
            raise RuntimeError("existing integration tests failed or skipped")
        for test in tests:
            if sum(e.get("Action") == "pass" and e.get("Test") == test for e in events) != 1:
                raise RuntimeError("an existing integration test did not run")
        self.report.setdefault("tests", {})[name] = sum(e.get("Action") == "pass" and bool(e.get("Test")) for e in events)

    @staticmethod
    def stop(proc, sig=signal.SIGTERM):
        if proc is not None and proc.poll() is None:
            proc.send_signal(sig)
            try:
                proc.wait(timeout=15)
            except subprocess.TimeoutExpired:
                # Only the new session owned by this Popen, never a global PID lookup.
                os.killpg(proc.pid, signal.SIGKILL)
                proc.wait(timeout=5)

    def run(self):
        self.stage("initialize-fresh-stores")
        self.initialize_pg("pgdata")
        self.sql("CREATE TABLE public.agent_storage_acceptance_guard (run_id text PRIMARY KEY);\n"
                 f"INSERT INTO public.agent_storage_acceptance_guard VALUES ('{self.run_id}');")
        self.initialize_redis()
        self.report["ports"] = {"postgres": self.pg_port, "redis": self.redis_port}
        self.go_tests("matrix", "TestRealStorageAcceptance", repeat=self.args.repeat)
        if self.args.matrix_only:
            self.report["result"] = "matrix_only_pass"
            return
        self.go_tests("recovery-seed", "TestRealStorageRecoverySeed", recovery="seed")
        self.stage("postgres-immediate-stop-and-redis-kill")
        # PostgreSQL immediate stop forces WAL recovery (not a graceful checkpoint).
        # Redis is killed without shutdown; appendfsync=always is explicit above.
        self.stop(self.pg, signal.SIGQUIT)
        self.stop(self.redis, signal.SIGKILL)
        self.start_pg()
        self.start_redis()  # NEVER recreate markers or seed data after restart.
        self.go_tests("after-crash-restart", "TestRealStorageRecoveryReplay", recovery="verify")
        self.stage("backup-and-restore-into-new-cluster")
        dump = self.root / "agent.dump"
        self.execute([self.args.pg_bin / "pg_dump", "-h", "127.0.0.1", "-p", self.pg_port,
                      "-U", "agent_m62", "-d", self.database, "-w", "--format=custom", "--no-owner",
                      "--no-acl", "--no-comments", "-f", dump], "backup", env=self.pg_environment())
        self.stop(self.pg, signal.SIGINT)
        self.initialize_pg("restored-pgdata")  # Fresh directory; original cluster is retained.
        self.execute([self.args.pg_bin / "pg_restore", "-h", "127.0.0.1", "-p", self.pg_port,
                      "-U", "agent_m62", "-d", self.database, "-w", "--no-owner", "--no-acl",
                      "--no-comments", "--exit-on-error", dump], "restore", env=self.pg_environment())
        self.go_tests("after-backup-restore", "TestRealStorageRecoveryReplay", recovery="verify")
        self.legacy_integration_checks()
        self.report["result"] = "pass"

    def close(self):
        try:
            self.stop(self.pg, signal.SIGINT)
        finally:
            self.stop(self.redis)
        self.report["stopped"] = all(p.poll() is not None for p in self.children)
        self.report["owned_pids"] = [p.pid for p in self.children]
        for handle in self.handles:
            handle.close()
        (self.root / "summary.json").write_text(json.dumps(self.report, indent=2) + "\n")
        print(f"result={self.report['result']} owned_processes_stopped={self.report['stopped']} artifacts={self.root}", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--allow-new-local-instances", action="store_true")
    parser.add_argument("--pg-bin", required=True, type=Path)
    parser.add_argument("--redis-server", required=True, type=Path)
    parser.add_argument("--redis-lib", type=Path)
    parser.add_argument("--go-cache", type=Path, default=Path("/tmp/budgetmatch-go-cache"))
    parser.add_argument("--repeat", type=int, choices=range(1, 11), default=3)
    parser.add_argument("--matrix-only", action="store_true", help="diagnostic run; does not certify restart/restore")
    args = parser.parse_args()
    if not args.allow_new_local_instances:
        parser.error("explicit --allow-new-local-instances required; no instances created")
    if os.name != "posix" or os.geteuid() == 0:
        parser.error("requires a non-root Unix user")
    args.pg_bin = args.pg_bin.absolute()
    args.redis_server = args.redis_server.absolute()
    args.go = shutil.which("go")
    required = [args.pg_bin / name for name in ("initdb", "postgres", "psql", "pg_dump", "pg_restore")]
    required += [args.redis_server, args.redis_server.parent / "redis-cli"]
    if not args.go or any(not os.access(p, os.X_OK) for p in required):
        parser.error("missing executable; provision prerequisites separately, nothing will be installed")
    os.umask(0o077)
    def interrupted(_signum, _frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupted)
    run = Acceptance(args)
    try:
        run.run()
    except (RuntimeError, subprocess.TimeoutExpired, KeyboardInterrupt) as error:
        # Do not print subprocess args/stdin or environment on failures.
        print(f"acceptance failed: {type(error).__name__}; inspect private artifacts", flush=True)
        return 1
    finally:
        run.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
