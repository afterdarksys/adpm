import importlib.util
import json
import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("adpm_builder", ROOT / "builder" / "adpm-build.py")
builder_module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(builder_module)
ADPMBuilder = builder_module.ADPMBuilder


class BuilderUnitTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="adpm-builder-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.builder = ADPMBuilder("fixture", "1.2.3", str(self.root / "dist"))
        self.builder.create_staging_dir()
        self.addCleanup(self.builder.cleanup)

    def executable(self, name="app"):
        path = self.root / name
        path.write_bytes(b"\x7fELFbinary")
        path.chmod(0o755)
        return path

    def test_copy_payload_preserves_portable_mode(self):
        source = self.executable()
        destination = self.root / "copied"
        ADPMBuilder.copy_payload(source, destination)
        self.assertEqual(destination.read_bytes(), b"\x7fELFbinary")
        self.assertTrue(destination.stat().st_mode & stat.S_IXUSR)

    def test_dependency_metadata_parses_constraints(self):
        with mock.patch.object(self.builder, "build_archive", return_value=self.root / "fixture.adpm"):
            self.builder.build(target_platform="linux-x86_64", dependencies=["base@>=2.0", "exact=1.4"])
        dependencies = self.builder.metadata["dependencies"]
        self.assertEqual(dependencies["base"]["version"], ">=2.0")
        self.assertEqual(dependencies["exact"]["version"], "=1.4")

    def test_linux_dependency_parser(self):
        output = """libfoo.so.1 => /opt/vendor/lib/libfoo.so.1 (0x1)\nlibmissing.so => not found\n/lib64/ld-linux-x86-64.so.2 (0x2)\n"""
        resolved, unresolved = self.builder.parse_ldd_output(output)
        self.assertEqual(resolved, [Path("/opt/vendor/lib/libfoo.so.1"), Path("/lib64/ld-linux-x86-64.so.2")])
        self.assertEqual(unresolved, ["libmissing.so"])

    def test_macos_dependency_parser(self):
        output = """app:\n\t/opt/vendor/lib/libfoo.dylib (compatibility version 1.0.0)\n\t@rpath/libbar.dylib (compatibility version 1.0.0)\n\t/usr/lib/libSystem.B.dylib (compatibility version 1.0.0)\n"""
        resolved, unresolved = self.builder.parse_otool_output(output)
        self.assertIn(Path("/opt/vendor/lib/libfoo.dylib"), resolved)
        self.assertIn("@rpath/libbar.dylib", unresolved)

    def test_system_library_classification(self):
        self.assertTrue(self.builder.is_system_library(Path("/lib/x86_64-linux-gnu/libc.so.6"), "linux-x86_64"))
        self.assertTrue(self.builder.is_system_library(Path("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"), "darwin-arm64"))
        self.assertFalse(self.builder.is_system_library(Path("/opt/vendor/lib/libfoo.so"), "linux-x86_64"))

    def test_recursive_dependency_detection_excludes_system_libraries(self):
        app = self.executable("app")
        vendor = self.executable("libvendor.so")
        system = Path("/usr/lib/libSystem.B.dylib")
        responses = {
            app: ([vendor, system], ["libmissing.so"]),
            vendor: ([], []),
        }
        with mock.patch.object(self.builder, "inspect_native_file", side_effect=lambda path, _: responses[Path(path)]):
            report = self.builder.detect_native_dependencies([str(app)], "linux-x86_64")
        self.assertEqual(report["bundled"], [str(vendor)])
        self.assertIn(str(system), report["excluded"])
        self.assertEqual(report["unresolved"], ["libmissing.so"])

    def test_linux_relocation_commands(self):
        binary = self.executable("app")
        library = self.executable("libfoo.so")
        with mock.patch("shutil.which", return_value="/usr/bin/patchelf"), mock.patch("subprocess.run") as run:
            self.builder.relocate_native_files([binary], [library], "linux-x86_64")
        commands = [call.args[0] for call in run.call_args_list]
        self.assertIn(["/usr/bin/patchelf", "--set-rpath", "$ORIGIN/../lib", str(binary)], commands)
        self.assertIn(["/usr/bin/patchelf", "--set-rpath", "$ORIGIN", str(library)], commands)

    def test_relocation_requires_platform_tool(self):
        with mock.patch("shutil.which", return_value=None):
            with self.assertRaisesRegex(RuntimeError, "patchelf"):
                self.builder.relocate_native_files([], [], "linux-x86_64")

    def test_macos_relocation_rewrites_bundled_references(self):
        binary = self.executable("app")
        library = self.executable("libfoo.dylib")
        otool_output = f"{binary}:\n\t/opt/vendor/lib/libfoo.dylib (compatibility version 1.0.0)\n"

        def which(name):
            return f"/usr/bin/{name}"

        def run(command, **kwargs):
            if command[0].endswith("otool"):
                return subprocess.CompletedProcess(command, 0, stdout=otool_output, stderr="")
            return subprocess.CompletedProcess(command, 0, stdout="", stderr="")

        with mock.patch("shutil.which", side_effect=which), mock.patch("subprocess.run", side_effect=run) as invoked:
            self.builder.relocate_native_files([binary], [library], "darwin-arm64")
        commands = [call.args[0] for call in invoked.call_args_list]
        self.assertIn(["/usr/bin/install_name_tool", "-change", "/opt/vendor/lib/libfoo.dylib",
                       "@loader_path/../lib/libfoo.dylib", str(binary)], commands)
        self.assertIn(["/usr/bin/install_name_tool", "-id", "@rpath/libfoo.dylib", str(library)], commands)

    def test_readelf_needed_parser(self):
        output = " 0x0000000000000001 (NEEDED) Shared library: [libssl.so.3]\n"
        self.assertEqual(self.builder.parse_readelf_output(output), ["libssl.so.3"])


if __name__ == "__main__":
    unittest.main()
