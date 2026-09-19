"""Offline checks for the disposable-storage launcher; no real stores are opened."""

import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import signal
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("acceptance", Path(__file__).with_name("agent-storage-acceptance.py"))
acceptance = importlib.util.module_from_spec(spec)
spec.loader.exec_module(acceptance)


class LauncherTests(unittest.TestCase):
    def test_environment_does_not_inherit_deployment_or_db_overrides(self):
        with patch.dict(os.environ, {"PGHOST": "business", "RAG_TEST_PG_DSN": "secret", "MODEL_API_KEY": "secret",
                                     "AGENT_STORAGE_ACCEPTANCE_RUN_ID": "old", "LD_LIBRARY_PATH": "/untrusted"}):
            env = acceptance.clean_environment()
        for key in ("PGHOST", "RAG_TEST_PG_DSN", "MODEL_API_KEY", "AGENT_STORAGE_ACCEPTANCE_RUN_ID", "LD_LIBRARY_PATH"):
            self.assertNotIn(key, env)
        self.assertEqual("2", env["GOMAXPROCS"])

    def test_grant_is_required_before_creating_anything(self):
        with patch("sys.argv", ["runner", "--pg-bin", "/missing", "--redis-server", "/missing"]), \
                patch.object(acceptance, "Acceptance") as constructor, contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as error:
                acceptance.main()
            self.assertEqual(2, error.exception.code)
            constructor.assert_not_called()

    def test_real_test_skip_or_missing_test_is_not_success(self):
        for events, should_pass in [
            ([{"Action": "pass", "Test": "TestReal"}], True),
            ([{"Action": "skip", "Test": "TestReal"}, {"Action": "pass"}], False),
            ([{"Action": "pass", "Test": "DifferentTest"}], False),
            ([{"Action": "fail", "Test": "TestReal"}], False),
        ]:
            with self.subTest(events=events), tempfile.TemporaryDirectory(prefix="agent-launcher-unit-") as root:
                runner = object.__new__(acceptance.Acceptance)
                runner.root, runner.config_path = Path(root), Path(root) / "acceptance.json"
                runner.run_id, runner.env, runner.report = "test", {}, {"stages": []}
                runner.args = SimpleNamespace(go="unused")
                def fake_execute(*_args, **_kwargs):
                    (runner.root / "matrix.log").write_text("\n".join(json.dumps(e) for e in events))
                with patch.object(runner, "execute", side_effect=fake_execute), contextlib.redirect_stdout(io.StringIO()):
                    if should_pass:
                        runner.go_tests("matrix", "TestReal")
                    else:
                        with self.assertRaises(RuntimeError):
                            runner.go_tests("matrix", "TestReal")

    def test_stop_signals_only_the_owned_handle_and_skips_exited_process(self):
        class Owned:
            def __init__(self):
                self.signals, self.done = [], False
            def poll(self):
                return 0 if self.done else None
            def send_signal(self, sig):
                self.signals.append(sig)
            def wait(self, timeout):
                self.done = True
        proc = Owned()
        acceptance.Acceptance.stop(proc, signal.SIGINT)
        acceptance.Acceptance.stop(proc)
        self.assertEqual([signal.SIGINT], proc.signals)


if __name__ == "__main__":
    unittest.main()
