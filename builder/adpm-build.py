#!/usr/bin/env python3
"""
ADPM Builder - After Dark Systems Package Manager Builder

Creates compressed newc CPIO .adpm packages with platform-specific binaries.
Homage to Todd Bennett III, unixeng.
"""

import argparse
import filecmp
import hashlib
import json
import os
import platform
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import List


class ADPMBuilder:
    """Build ADPM packages from dependencies."""

    SUPPORTED_PLATFORMS = [
        "darwin-arm64",
        "darwin-x86_64",
        "linux-x86_64",
        "linux-aarch64",
    ]

    def __init__(self, name: str, version: str, output_dir: str = "dist"):
        self.name = name
        self.version = version
        self.output_dir = Path(output_dir)
        self.output_dir.mkdir(parents=True, exist_ok=True)
        self.temp_dir = None
        self.metadata = {
            "name": name,
            "version": version,
            "packager": "After Dark Systems Package Manager",
            "platforms": [],
            "dependencies": {},
            "install": {
                "requires_root": False,
                "install_prefix": "~/.local",
                "post_install": []
            }
        }

    def detect_platform(self) -> str:
        """Detect current platform."""
        system = platform.system().lower()
        machine = platform.machine().lower()

        if system == "darwin":
            if machine in ["arm64", "aarch64"]:
                return "darwin-arm64"
            return "darwin-x86_64"
        elif system == "linux":
            if machine in ["arm64", "aarch64"]:
                return "linux-aarch64"
            return "linux-x86_64"

        raise ValueError(f"Unsupported platform: {system}-{machine}")

    def create_staging_dir(self) -> Path:
        """Create temporary staging directory."""
        self.temp_dir = Path(tempfile.mkdtemp(prefix="adpm_build_"))
        (self.temp_dir / "bin").mkdir(exist_ok=True)
        (self.temp_dir / "lib").mkdir(exist_ok=True)
        (self.temp_dir / "python").mkdir(exist_ok=True)
        state_tool = Path(__file__).resolve().parent.parent / "installer" / "adpm_state.py"
        shutil.copyfile(state_tool, self.temp_dir / ".ADPM_STATE.py")
        return self.temp_dir

    @staticmethod
    def copy_payload(src: Path, dst: Path):
        """Copy content and portable permission bits without platform flags/xattrs."""
        if dst.exists() and not filecmp.cmp(src, dst, shallow=False):
            raise ValueError(f"Payload filename collision: {src} and {dst}")
        shutil.copyfile(src, dst)
        dst.chmod(src.stat().st_mode & 0o777)

    def add_binaries(self, binary_paths: List[str], target_platform: str):
        """Add binary executables to package."""
        platform_bin = self.temp_dir / "bin" / target_platform
        platform_bin.mkdir(parents=True, exist_ok=True)

        for binary_path in binary_paths:
            src = Path(binary_path).expanduser()
            if src.is_file():
                self.copy_payload(src, platform_bin / src.name)
                print(f"  Added binary: {src.name}")
            elif src.is_dir():
                # Copy all executables from directory
                for exe in src.glob("*"):
                    if exe.is_file() and os.access(exe, os.X_OK):
                        self.copy_payload(exe, platform_bin / exe.name)
                        print(f"  Added binary: {exe.name}")

    def add_libraries(self, library_paths: List[str], target_platform: str):
        """Add shared libraries to package."""
        platform_lib = self.temp_dir / "lib" / target_platform
        platform_lib.mkdir(parents=True, exist_ok=True)

        for lib_path in library_paths:
            src = Path(lib_path).expanduser()
            if src.is_file():
                self.copy_payload(src, platform_lib / src.name)
                print(f"  Added library: {src.name}")
            elif src.is_dir():
                # Copy all .so/.dylib files
                for pattern in ["*.so*", "*.dylib"]:
                    for lib in src.glob(pattern):
                        if lib.is_file():
                            self.copy_payload(lib, platform_lib / lib.name)
                            print(f"  Added library: {lib.name}")
                            
    def generate_sbom(self):
        """Generate a basic SBOM and embed it in metadata."""
        print("  Generating Software Bill of Materials (SBOM)...")
        sbom = {
            "format": "cyclonedx-adpm-basic",
            "version": "1.0",
            "components": []
        }
        
        # We perform a basic scan of the staging lib/ and python/ dirs
        if (self.temp_dir / "lib").exists():
            for platform_dir in (self.temp_dir / "lib").iterdir():
                if platform_dir.is_dir():
                    for lib in platform_dir.glob("*"):
                        if lib.is_file():
                            sha256 = hashlib.sha256(lib.read_bytes()).hexdigest()
                            sbom["components"].append({
                                "type": "library",
                                "name": lib.name,
                                "purl": f"pkg:generic/{lib.name}",
                                "hashes": [{"alg": "SHA-256", "content": sha256}]
                            })
                            
        if (self.temp_dir / "python").exists():
            for whl in (self.temp_dir / "python").glob("*.whl"):
                sha256 = hashlib.sha256(whl.read_bytes()).hexdigest()
                sbom["components"].append({
                    "type": "library",
                    "name": whl.name,
                    "purl": f"pkg:pypi/{whl.name}",
                    "hashes": [{"alg": "SHA-256", "content": sha256}]
                })
                
        self.metadata["sbom"] = sbom

    def add_python_packages(self, package_names: List[str]):
        """Download and add Python packages (wheels)."""
        python_dir = self.temp_dir / "python"

        for package in package_names:
            print(f"  Downloading Python package: {package}")
            try:
                subprocess.run(
                    ["pip", "download", "--dest", str(python_dir), "--no-deps", package],
                    check=True,
                    capture_output=True
                )
                self.metadata["dependencies"][package] = {
                    "type": "python",
                    "version": "latest"
                }
            except subprocess.CalledProcessError as e:
                print(f"    Warning: Failed to download {package}: {e}")

    def add_dependency_metadata(self, dep_name: str, version: str, platforms: List[str]):
        """Add dependency metadata."""
        self.metadata["dependencies"][dep_name] = {
            "version": version,
            "platforms": platforms
        }

    @staticmethod
    def _git_value(source_dir: Path, arguments: List[str]):
        try:
            result = subprocess.run(["git", "-C", str(source_dir)] + arguments,
                                    check=True, capture_output=True, text=True)
            return result.stdout.strip()
        except (FileNotFoundError, subprocess.CalledProcessError):
            return ""

    def build_provenance(self, binaries=None, libraries=None, source_dir=None,
                         source_url=None, source_ref=None):
        """Describe where the packaged inputs came from without claiming compilation."""
        source_path = Path(source_dir).expanduser().resolve() if source_dir else None
        if source_path and not source_path.is_dir():
            raise ValueError(f"Source directory does not exist: {source_path}")
        detected_url = self._git_value(source_path, ["remote", "get-url", "origin"]) if source_path else ""
        detected_ref = self._git_value(source_path, ["rev-parse", "HEAD"]) if source_path else ""
        dirty = bool(self._git_value(source_path, ["status", "--porcelain"])) if source_path else False
        return {
            "method": "adpm-build",
            "source_kind": "source" if source_path else "local-files",
            "source_dir": str(source_path) if source_path else "",
            "source_url": source_url or detected_url,
            "source_ref": source_ref or detected_ref,
            "source_dirty": dirty,
            "inputs": {
                "binaries": [str(Path(value).expanduser().resolve()) for value in binaries or []],
                "libraries": [str(Path(value).expanduser().resolve()) for value in libraries or []],
            },
            "command": shlex.join(sys.argv),
            "built_at": datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
            "host": {"os": platform.system(), "architecture": platform.machine(), "python": platform.python_version()},
        }

    @staticmethod
    def sha256_file(path: Path):
        digest = hashlib.sha256()
        with Path(path).open("rb") as source:
            for block in iter(lambda: source.read(1024 * 1024), b""):
                digest.update(block)
        return digest.hexdigest()

    def record_build_state(self, archive_path: Path):
        state_tool = Path(__file__).resolve().parents[1] / "installer" / "adpm_state.py"
        database = Path(os.environ.get("ADPM_DB", "~/.local/share/adpm")).expanduser()
        record = {
            "name": self.name,
            "version": self.version,
            "archive_path": str(Path(archive_path).resolve()),
            "sha256": self.sha256_file(archive_path),
            "size": Path(archive_path).stat().st_size,
            "built_at": self.metadata.get("provenance", {}).get("built_at", ""),
            "provenance": self.metadata.get("provenance", {}),
            "metadata": self.metadata,
        }
        subprocess.run([sys.executable, str(state_tool), "build", "--db", str(database),
                        "--record", json.dumps(record)], check=True)

    @staticmethod
    def parse_ldd_output(output: str):
        """Return resolved paths and unresolved sonames from ldd output."""
        resolved, unresolved = [], []
        for raw_line in output.splitlines():
            line = raw_line.strip()
            if not line or line.startswith("linux-vdso"):
                continue
            if "=> not found" in line:
                unresolved.append(line.split("=>", 1)[0].strip())
                continue
            candidate = (line.split("=>", 1)[1].strip().split(" ", 1)[0]
                         if "=>" in line else line.split(" ", 1)[0])
            if candidate.startswith("/"):
                resolved.append(Path(candidate))
        return resolved, unresolved

    @staticmethod
    def parse_otool_output(output: str):
        """Return absolute dylibs and unresolved loader-relative references."""
        resolved, unresolved = [], []
        for raw_line in output.splitlines()[1:]:
            line = raw_line.strip()
            if not line:
                continue
            reference = line.split(" (", 1)[0]
            if reference.startswith("/"):
                resolved.append(Path(reference))
            elif reference.startswith(("@rpath/", "@loader_path/", "@executable_path/")):
                unresolved.append(reference)
        return resolved, unresolved

    @staticmethod
    def parse_readelf_output(output: str):
        return re.findall(r"Shared library: \[([^]]+)\]", output)

    @staticmethod
    def is_system_library(path: Path, target_platform: str) -> bool:
        value = str(path)
        if target_platform.startswith("darwin-"):
            return value.startswith(("/usr/lib/", "/System/Library/"))
        return value.startswith(("/lib/", "/lib64/", "/usr/lib/", "/usr/lib64/"))

    @staticmethod
    def _library_search_paths(origin: Path):
        paths = [origin]
        paths.extend(Path(value) for value in os.environ.get("LD_LIBRARY_PATH", "").split(os.pathsep) if value)
        paths.extend(Path(value) for value in ["/usr/local/lib", "/lib", "/lib64", "/usr/lib", "/usr/lib64"])
        for base in [Path("/lib"), Path("/usr/lib")]:
            if base.exists():
                paths.extend(path for path in base.glob("*-linux-gnu") if path.is_dir())
        return paths

    def _resolve_soname(self, soname: str, origin: Path):
        for directory in self._library_search_paths(origin):
            candidate = directory / soname
            if candidate.exists():
                return candidate
        return None

    def inspect_native_file(self, path: Path, target_platform: str):
        """Inspect one Mach-O or ELF file with the available loader tooling."""
        path = Path(path)
        if target_platform.startswith("darwin-"):
            otool = shutil.which("otool")
            if not otool:
                raise RuntimeError("otool is required for macOS dependency detection")
            result = subprocess.run([otool, "-L", str(path)], check=True, capture_output=True, text=True)
            resolved, references = self.parse_otool_output(result.stdout)
            load_commands = subprocess.run([otool, "-l", str(path)], check=True, capture_output=True, text=True)
            rpaths = re.findall(r"\n\s*path\s+(.+?)\s+\(offset", load_commands.stdout)
            unresolved = []
            for reference in references:
                candidates = []
                if reference.startswith("@rpath/"):
                    suffix = reference[len("@rpath/"):]
                    candidates = [Path(value.replace("@loader_path", str(path.parent)).replace("@executable_path", str(path.parent))) / suffix for value in rpaths]
                elif reference.startswith("@loader_path/"):
                    candidates = [path.parent / reference[len("@loader_path/"):]]
                elif reference.startswith("@executable_path/"):
                    candidates = [path.parent / reference[len("@executable_path/"):]]
                match = next((candidate for candidate in candidates if candidate.exists()), None)
                if match:
                    resolved.append(match)
                else:
                    unresolved.append(reference)
            return resolved, unresolved

        ldd = shutil.which("ldd")
        if ldd:
            result = subprocess.run([ldd, str(path)], check=False, capture_output=True, text=True)
            if result.stdout.strip():
                return self.parse_ldd_output(result.stdout + "\n" + result.stderr)
        readelf = shutil.which("readelf")
        if not readelf:
            raise RuntimeError("ldd or readelf is required for Linux dependency detection")
        result = subprocess.run([readelf, "-d", str(path)], check=True, capture_output=True, text=True)
        resolved, unresolved = [], []
        for soname in self.parse_readelf_output(result.stdout):
            candidate = self._resolve_soname(soname, path.parent)
            if candidate:
                resolved.append(candidate)
            else:
                unresolved.append(soname)
        return resolved, unresolved

    @staticmethod
    def _native_roots(paths: List[str]):
        roots = []
        for value in paths or []:
            path = Path(value).expanduser()
            if path.is_file() and ADPMBuilder.is_native_file(path):
                roots.append(path)
            elif path.is_dir():
                roots.extend(item for item in path.rglob("*")
                             if item.is_file() and ADPMBuilder.is_native_file(item))
        return roots

    @staticmethod
    def is_native_file(path: Path) -> bool:
        try:
            with path.open("rb") as source:
                magic = source.read(4)
        except OSError:
            return False
        return magic == b"\x7fELF" or magic in {
            b"\xfe\xed\xfa\xce", b"\xfe\xed\xfa\xcf", b"\xce\xfa\xed\xfe",
            b"\xcf\xfa\xed\xfe", b"\xca\xfe\xba\xbe", b"\xca\xfe\xba\xbf",
            b"\xbe\xba\xfe\xca", b"\xbf\xba\xfe\xca",
        }

    def detect_native_dependencies(self, roots: List[str], target_platform: str):
        """Recursively discover bundleable non-system native libraries."""
        initial = self._native_roots(roots)
        queue = list(initial)
        inspected, bundled, excluded, unresolved = set(), set(), set(), set()
        while queue:
            current = Path(queue.pop(0))
            identity = str(current.resolve()) if current.exists() else str(current)
            if identity in inspected:
                continue
            inspected.add(identity)
            resolved, missing = self.inspect_native_file(current, target_platform)
            unresolved.update(missing)
            for dependency in resolved:
                dependency = Path(dependency)
                if self.is_system_library(dependency, target_platform):
                    excluded.add(str(dependency))
                elif dependency.exists():
                    if str(dependency) not in bundled:
                        bundled.add(str(dependency))
                        queue.append(dependency)
                else:
                    unresolved.add(str(dependency))
        return {
            "roots": sorted(str(path) for path in initial),
            "bundled": sorted(bundled),
            "excluded": sorted(excluded),
            "unresolved": sorted(unresolved),
        }

    def relocate_native_files(self, binaries: List[Path], libraries: List[Path], target_platform: str):
        """Rewrite loader paths on staged files without modifying source files."""
        binaries = [Path(path) for path in binaries]
        libraries = [Path(path) for path in libraries]
        if target_platform.startswith("linux-"):
            patchelf = shutil.which("patchelf")
            if not patchelf:
                raise RuntimeError("patchelf is required for Linux relocation")
            for binary in binaries:
                subprocess.run([patchelf, "--set-rpath", "$ORIGIN/../lib", str(binary)], check=True)
            for library in libraries:
                subprocess.run([patchelf, "--set-rpath", "$ORIGIN", str(library)], check=True)
            return

        install_name_tool = shutil.which("install_name_tool")
        otool = shutil.which("otool")
        if not install_name_tool or not otool:
            raise RuntimeError("install_name_tool and otool are required for macOS relocation")
        bundled_names = {path.name for path in libraries}
        for current in binaries + libraries:
            result = subprocess.run([otool, "-L", str(current)], check=True, capture_output=True, text=True)
            references = [line.strip().split(" (", 1)[0] for line in result.stdout.splitlines()[1:] if line.strip()]
            is_library = current in libraries
            for reference in references:
                name = Path(reference).name
                if name not in bundled_names:
                    continue
                replacement = f"@loader_path/{name}" if is_library else f"@loader_path/../lib/{name}"
                subprocess.run([install_name_tool, "-change", reference, replacement, str(current)], check=True)
            if is_library:
                subprocess.run([install_name_tool, "-id", f"@rpath/{current.name}", str(current)], check=True)

    def create_install_script(self):
        """Create a post-install hook; the ADPM installer owns file copying."""
        install_script = '''#!/bin/bash
# ADPM post-install hook
# Homage to Todd Bennett III, unixeng

set -e

PACKAGE_NAME="{{ NAME }}"
PACKAGE_VERSION="{{ VERSION }}"

# Add package-specific post-install actions below. Files and bundled wheels are
# installed by `adpm install` before this hook runs.
exit 0
'''
        install_script = install_script.replace('"{{ NAME }}"', shlex.quote(self.name))
        install_script = install_script.replace('"{{ VERSION }}"', shlex.quote(self.version))

        install_path = self.temp_dir / "INSTALL.sh"
        install_path.write_text(install_script)
        install_path.chmod(0o755)

    def write_metadata(self):
        """Write META.json file."""
        meta_path = self.temp_dir / "META.json"
        with open(meta_path, 'w') as f:
            json.dump(self.metadata, f, indent=2)

    def strip_files(self, target_platform: str):
        """Strip debug symbols from binaries and libraries."""
        for d in ["bin", "lib"]:
            target_dir = self.temp_dir / d / target_platform
            if not target_dir.exists():
                continue
            for f in target_dir.glob("*"):
                if f.is_file():
                    try:
                        subprocess.run(["strip", str(f)], check=False, capture_output=True)
                    except Exception as e:
                        print(f"    Warning: Failed to strip {f.name}: {e}")

    def build_archive(self, compress: str = "bzip2") -> Path:
        """Build the final .adpm archive."""
        output_file = self.output_dir / f"{self.name}-{self.version}.adpm"

        print(f"\nBuilding archive: {output_file}")

        # Keep output outside staging so the archive cannot include itself.
        cpio_fd, cpio_name = tempfile.mkstemp(prefix="adpm_archive_", suffix=".cpio")
        os.close(cpio_fd)
        cpio_file = Path(cpio_name)
        find_proc = subprocess.Popen(["find", ".", "-print"], stdout=subprocess.PIPE, cwd=self.temp_dir)
        with open(cpio_file, "wb") as f_out:
            subprocess.run(["cpio", "-o", "--format", "newc"], stdin=find_proc.stdout, stdout=f_out, cwd=self.temp_dir, check=True)
        find_proc.wait()

        compressors = {
            "bzip2": ["bzip2", "-9", "-c"],
            "gzip": ["gzip", "-9", "-c"],
            "xz": ["xz", "-9", "-c"],
            "zstd": ["zstd", "-19", "-q", "-c"],
        }
        if compress not in compressors:
            raise ValueError(f"Unsupported compression: {compress}")

        # .adpm is the public format extension; readers detect compression
        # from magic bytes.
        final_dest = output_file
        try:
            with open(final_dest, "wb") as compressed:
                subprocess.run(compressors[compress] + [str(cpio_file)], stdout=compressed, check=True)
        finally:
            cpio_file.unlink(missing_ok=True)

        print(f"✓ Package created: {final_dest}")
        print(f"  Size: {final_dest.stat().st_size / 1024 / 1024:.2f} MB")

        return final_dest
        
    def sign_archive(self, archive_path: Path, key: str = None):
        """Sign the archive with GPG and generate a SHA256 sum."""
        print("  Signing package and generating checksums...")
        
        # SHA256 Sum
        sha256 = hashlib.sha256(archive_path.read_bytes()).hexdigest()
        sha_path = archive_path.with_suffix(archive_path.suffix + ".sha256")
        sha_path.write_text(f"{sha256}  {archive_path.name}\n")
        print(f"  Generated SHA256: {sha_path.name}")
        
        # GPG Signature
        gpg_args = ["gpg", "--detach-sign", "--armor"]
        if key:
            gpg_args.extend(["--default-key", key])
        gpg_args.append(str(archive_path))
        
        try:
            subprocess.run(gpg_args, check=True, capture_output=True)
            print(f"  Generated GPG signature: {archive_path.name}.asc")
        except subprocess.CalledProcessError as e:
            print(f"    Warning: Failed to sign package. Is GPG configured? Error: {e.stderr.decode()}")
            
    def cleanup(self):
        """Clean up temporary directory."""
        if self.temp_dir and self.temp_dir.exists():
            shutil.rmtree(self.temp_dir)

    def build(self, binaries: List[str] = None, libraries: List[str] = None,
              python_packages: List[str] = None, target_platform: str = None,
              strip: bool = False, compress: str = "bzip2",
              sign: bool = False, key: str = None, generate_sbom: bool = False,
              dependencies: List[str] = None, detect_dependencies: bool = False,
              relocate: bool = False, source_dir: str = None,
              source_url: str = None, source_ref: str = None,
              record_build: bool = True):
        """Build complete package."""
        try:
            if not target_platform:
                target_platform = self.detect_platform()

            print(f"Building ADPM package: {self.name} v{self.version}")
            print(f"Target platform: {target_platform}")

            self.create_staging_dir()
            self.metadata["platforms"].append(target_platform)
            self.metadata["provenance"] = self.build_provenance(
                binaries, libraries, source_dir, source_url, source_ref)

            for dependency in dependencies or []:
                name, separator, constraint = dependency.partition("@")
                if not separator:
                    name, separator, constraint = dependency.partition("=")
                    if separator:
                        constraint = "=" + constraint
                name = name.strip()
                if not name:
                    raise ValueError(f"Invalid dependency: {dependency}")
                self.metadata["dependencies"][name] = {
                    "type": "adpm",
                    "version": constraint.strip() or "*"
                }

            if binaries:
                print("Adding binaries...")
                self.add_binaries(binaries, target_platform)

            if libraries:
                print("Adding libraries...")
                self.add_libraries(libraries, target_platform)

            if relocate:
                detect_dependencies = True
            if detect_dependencies:
                print("Detecting native dependencies...")
                report = self.detect_native_dependencies((binaries or []) + (libraries or []), target_platform)
                self.metadata["native_dependencies"] = report
                if report["bundled"]:
                    self.add_libraries(report["bundled"], target_platform)
                for missing in report["unresolved"]:
                    print(f"    Warning: unresolved native dependency: {missing}")

            if relocate:
                print("Rewriting native loader paths...")
                staged_binaries = [path for path in (self.temp_dir / "bin" / target_platform).glob("*")
                                   if self.is_native_file(path)]
                staged_libraries = [path for path in (self.temp_dir / "lib" / target_platform).glob("*")
                                   if self.is_native_file(path)]
                self.relocate_native_files(staged_binaries, staged_libraries, target_platform)
                self.metadata.setdefault("native_dependencies", {})["relocated"] = True

            if python_packages:
                print("Adding Python packages...")
                self.add_python_packages(python_packages)

            if strip:
                print("Stripping debug symbols...")
                self.strip_files(target_platform)

            if generate_sbom:
                self.generate_sbom()

            self.create_install_script()
            self.write_metadata()

            archive_path = self.build_archive(compress)
            if sign:
                self.sign_archive(archive_path, key)
            if record_build:
                self.record_build_state(archive_path)
                
            return archive_path

        finally:
            self.cleanup()


def main():
    parser = argparse.ArgumentParser(
        description="ADPM Builder - Create After Dark Systems Package Manager packages"
    )
    parser.add_argument("--name", required=True, help="Package name")
    parser.add_argument("--version", required=True, help="Package version")
    parser.add_argument("--platform", help="Target platform (auto-detected if not specified)")
    parser.add_argument("--binaries", nargs="+", help="Binary files or directories to include")
    parser.add_argument("--libraries", nargs="+", help="Library files or directories to include")
    parser.add_argument("--python", nargs="+", help="Python packages to include")
    parser.add_argument("--output", default="dist", help="Output directory")
    parser.add_argument("--strip", action="store_true", help="Strip debug symbols from binaries and libraries")
    parser.add_argument("--compress", choices=["bzip2", "gzip", "xz", "zstd"], default="bzip2", help="Compression algorithm")
    parser.add_argument("--sign", action="store_true", help="GPG sign the resulting archive (creates .asc format detach signature)")
    parser.add_argument("--key", help="GPG key ID to use for signing")
    parser.add_argument("--generate-sbom", action="store_true", help="Generate and embed SBOM in package metadata")
    parser.add_argument("--dependency", action="append", default=[], help="ADPM dependency as NAME, NAME@CONSTRAINT, or NAME=VERSION")
    parser.add_argument("--detect-dependencies", action="store_true", help="Recursively detect and bundle non-system native libraries")
    parser.add_argument("--relocate", action="store_true", help="Rewrite loader paths for bundled libraries (implies --detect-dependencies)")
    parser.add_argument("--source-dir", help="Source tree used to produce the packaged files (captures Git provenance)")
    parser.add_argument("--source-url", help="Override source repository URL in build provenance")
    parser.add_argument("--source-ref", help="Override source revision in build provenance")
    parser.add_argument("--no-record-build", action="store_true", help="Do not record this archive in the local ADPM build ledger")

    args = parser.parse_args()

    builder = ADPMBuilder(args.name, args.version, args.output)
    builder.build(
        binaries=args.binaries,
        libraries=args.libraries,
        python_packages=args.python,
        target_platform=args.platform,
        strip=args.strip,
        compress=args.compress,
        sign=args.sign,
        key=args.key,
        generate_sbom=args.generate_sbom,
        dependencies=args.dependency,
        detect_dependencies=args.detect_dependencies,
        relocate=args.relocate,
        source_dir=args.source_dir,
        source_url=args.source_url,
        source_ref=args.source_ref,
        record_build=not args.no_record_build
    )


if __name__ == "__main__":
    main()
