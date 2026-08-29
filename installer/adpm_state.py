#!/usr/bin/env python3
"""Durable lifecycle, build, and file-ownership state for ADPM."""

import argparse
import json
import os
import re
import tempfile
import uuid
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path

try:
    import fcntl
except ImportError:  # pragma: no cover - Windows is not an installer target yet.
    fcntl = None


SCHEMA_VERSION = 1
IDENTITY = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._+:-]*$")


class OwnershipConflict(RuntimeError):
    pass


def utc_now():
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def ensure_identity(value, label):
    if not isinstance(value, str) or not IDENTITY.match(value):
        raise ValueError(f"invalid {label}: {value!r}")
    return value


def atomic_write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=str(path.parent))
    try:
        with os.fdopen(descriptor, "w") as output:
            json.dump(value, output, indent=2, sort_keys=True)
            output.write("\n")
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


@contextmanager
def state_lock(db):
    db = Path(db)
    db.mkdir(parents=True, exist_ok=True)
    with (db / ".state.lock").open("a+") as lock:
        if fcntl:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
        try:
            yield
        finally:
            if fcntl:
                fcntl.flock(lock.fileno(), fcntl.LOCK_UN)


def append_event(db, action, package, version="", details=None):
    ensure_identity(action, "action")
    ensure_identity(package, "package")
    event = {
        "schema_version": SCHEMA_VERSION,
        "event_id": str(uuid.uuid4()),
        "timestamp": utc_now(),
        "action": action,
        "package": package,
        "version": str(version or ""),
    }
    event.update(details or {})
    history = Path(db) / "history.jsonl"
    history.parent.mkdir(parents=True, exist_ok=True)
    with history.open("a") as output:
        if fcntl:
            fcntl.flock(output.fileno(), fcntl.LOCK_EX)
        output.write(json.dumps(event, sort_keys=True, separators=(",", ":")) + "\n")
        output.flush()
        os.fsync(output.fileno())
        if fcntl:
            fcntl.flock(output.fileno(), fcntl.LOCK_UN)
    return event


def read_history(db, package=None):
    history = Path(db) / "history.jsonl"
    if not history.exists():
        return []
    events = [json.loads(line) for line in history.read_text().splitlines() if line.strip()]
    return [event for event in events if package is None or event.get("package") == package]


def read_owners(db):
    path = Path(db) / "owners.json"
    if not path.exists():
        return {"schema_version": SCHEMA_VERSION, "files": {}}
    value = json.loads(path.read_text())
    value.setdefault("schema_version", SCHEMA_VERSION)
    value.setdefault("files", {})
    return value


def read_installed(db, package):
    ensure_identity(package, "package")
    path = Path(db) / "installed" / f"{package}.json"
    return json.loads(path.read_text()) if path.exists() else None


def check_ownership(db, package, files):
    ensure_identity(package, "package")
    owners = read_owners(db)["files"]
    conflicts = [(path, owners[path]) for path in sorted(set(files))
                 if path in owners and owners[path].get("package") != package]
    if conflicts:
        path, owner = conflicts[0]
        raise OwnershipConflict(f"{path} is owned by {owner.get('package')} {owner.get('version', '')}".strip())


def record_install(db, record, action="install", details=None):
    package = ensure_identity(record.get("name"), "package")
    version = ensure_identity(str(record.get("version", "")), "version")
    files = sorted(set(str(path) for path in record.get("files", [])))
    record = dict(record)
    record.update({"schema_version": SCHEMA_VERSION, "name": package, "version": version, "files": files})
    with state_lock(db):
        check_ownership(db, package, files)
        owners = read_owners(db)
        owners["files"] = {path: owner for path, owner in owners["files"].items()
                           if owner.get("package") != package or path in files}
        for path in files:
            owners["files"][path] = {"package": package, "version": version}
        atomic_write_json(Path(db) / "owners.json", owners)
        atomic_write_json(Path(db) / "installed" / f"{package}.json", record)
        append_event(db, action, package, version, details)
    return record


def record_remove(db, package, details=None):
    package = ensure_identity(package, "package")
    with state_lock(db):
        record = read_installed(db, package)
        if record is None:
            raise KeyError(f"package is not installed: {package}")
        owners = read_owners(db)
        owners["files"] = {path: owner for path, owner in owners["files"].items()
                           if owner.get("package") != package}
        atomic_write_json(Path(db) / "owners.json", owners)
        (Path(db) / "installed" / f"{package}.json").unlink()
        append_event(db, "remove", package, record.get("version", ""), details)
    return record


def record_build(db, record):
    package = ensure_identity(record.get("name"), "package")
    version = ensure_identity(str(record.get("version", "")), "version")
    checksum = str(record.get("sha256", ""))
    if not re.fullmatch(r"[0-9a-fA-F]{3,64}", checksum):
        raise ValueError("build record requires a hexadecimal SHA-256")
    record = dict(record)
    record.update({"schema_version": SCHEMA_VERSION, "name": package, "version": version})
    path = Path(db) / "builds" / package / version / f"{checksum}.json"
    with state_lock(db):
        atomic_write_json(path, record)
        append_event(db, "build", package, version, {
            "sha256": checksum,
            "archive_path": record.get("archive_path", ""),
        })
    return path


def load_json_argument(value):
    if value == "-":
        return json.load(os.sys.stdin)
    return json.loads(value)


def main(argv=None):
    parser = argparse.ArgumentParser(description="ADPM lifecycle state manager")
    subparsers = parser.add_subparsers(dest="command", required=True)

    check = subparsers.add_parser("check-ownership")
    check.add_argument("--db", required=True); check.add_argument("--package", required=True); check.add_argument("--files", required=True)
    install = subparsers.add_parser("install")
    install.add_argument("--db", required=True); install.add_argument("--record", required=True); install.add_argument("--action", default="install")
    remove = subparsers.add_parser("remove")
    remove.add_argument("--db", required=True); remove.add_argument("--package", required=True)
    build = subparsers.add_parser("build")
    build.add_argument("--db", required=True); build.add_argument("--record", required=True)
    event = subparsers.add_parser("event")
    event.add_argument("--db", required=True); event.add_argument("--action", required=True); event.add_argument("--package", required=True); event.add_argument("--version", default=""); event.add_argument("--details", default="{}")
    history = subparsers.add_parser("history")
    history.add_argument("--db", required=True); history.add_argument("--package")
    status = subparsers.add_parser("status")
    status.add_argument("--db", required=True); status.add_argument("--package")
    args = parser.parse_args(argv)

    if args.command == "check-ownership":
        check_ownership(args.db, args.package, load_json_argument(args.files))
    elif args.command == "install":
        record_install(args.db, load_json_argument(args.record), args.action)
    elif args.command == "remove":
        record_remove(args.db, args.package)
    elif args.command == "build":
        print(record_build(args.db, load_json_argument(args.record)))
    elif args.command == "event":
        append_event(args.db, args.action, args.package, args.version, load_json_argument(args.details))
    elif args.command == "history":
        print(json.dumps(read_history(args.db, args.package), indent=2))
    elif args.command == "status":
        if args.package:
            value = read_installed(args.db, args.package)
        else:
            installed = Path(args.db) / "installed"
            value = [json.loads(path.read_text()) for path in sorted(installed.glob("*.json"))] if installed.exists() else []
        print(json.dumps(value, indent=2))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (ValueError, KeyError, OwnershipConflict) as error:
        print(f"adpm state: {error}", file=os.sys.stderr)
        raise SystemExit(1)
