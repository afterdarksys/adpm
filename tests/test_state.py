import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("adpm_state", ROOT / "installer" / "adpm_state.py")
state = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(state)


class LifecycleStateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="adpm-state-test-")
        self.addCleanup(self.temp.cleanup)
        self.db = Path(self.temp.name)

    def test_build_record_and_event_are_persisted(self):
        record = {"name": "demo", "version": "1.0.0", "sha256": "abc", "archive_path": "/tmp/demo.adpm"}
        path = state.record_build(self.db, record)
        self.assertTrue(path.is_file())
        self.assertEqual(json.loads(path.read_text())["sha256"], "abc")
        self.assertEqual(state.read_history(self.db)[0]["action"], "build")

    def test_install_claims_ownership_and_remove_preserves_history(self):
        files = ["/prefix/bin/demo", "/prefix/lib/libdemo.so"]
        record = {"name": "demo", "version": "1.0.0", "files": files}
        state.record_install(self.db, record)
        owners = state.read_owners(self.db)
        self.assertEqual(owners["files"][files[0]]["package"], "demo")
        removed = state.record_remove(self.db, "demo")
        self.assertEqual(removed["version"], "1.0.0")
        self.assertFalse((self.db / "installed" / "demo.json").exists())
        self.assertEqual([event["action"] for event in state.read_history(self.db)], ["install", "remove"])
        self.assertEqual(state.read_owners(self.db)["files"], {})

    def test_cross_package_ownership_conflict_is_rejected(self):
        path = "/prefix/bin/shared"
        state.record_install(self.db, {"name": "first", "version": "1", "files": [path]})
        with self.assertRaisesRegex(state.OwnershipConflict, "first"):
            state.check_ownership(self.db, "second", [path])
        self.assertEqual(state.read_owners(self.db)["files"][path]["package"], "first")

    def test_same_package_can_replace_its_own_files(self):
        path = "/prefix/bin/demo"
        state.record_install(self.db, {"name": "demo", "version": "1", "files": [path]})
        state.check_ownership(self.db, "demo", [path])
        state.record_install(self.db, {"name": "demo", "version": "2", "files": [path]}, action="upgrade")
        self.assertEqual(state.read_owners(self.db)["files"][path]["version"], "2")

    def test_legacy_installed_record_remains_readable(self):
        installed = self.db / "installed"
        installed.mkdir(parents=True)
        (installed / "legacy.json").write_text('{"name":"legacy","version":"1","files":[]}')
        self.assertEqual(state.read_installed(self.db, "legacy")["version"], "1")

    def test_rollback_event_is_append_only(self):
        state.append_event(self.db, "rollback", "demo", "1.0.0", {"from_version": "2.0.0"})
        event = state.read_history(self.db)[0]
        self.assertEqual(event["action"], "rollback")
        self.assertEqual(event["from_version"], "2.0.0")


if __name__ == "__main__":
    unittest.main()

