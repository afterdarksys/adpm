# Lifecycle State Specification

## Acceptance criteria

1. Every successful build records archive path, SHA-256, size, metadata, source provenance, and a `build` event.
2. Every successful install records archive SHA-256, package provenance, installed files, and an `install` event while preserving the existing installed-record path.
3. Remove, upgrade, and rollback operations append durable events; removal does not erase lifecycle history.
4. `owners.json` maps each managed file to exactly one package/version and rejects cross-package overwrite attempts before files are copied.
5. Uninstall removes only files currently owned by the package and releases their ownership entries.
6. State writes are atomic; history is append-only and serialized with a file lock where supported.
7. Existing installed records without schema/provenance fields remain readable.
8. `adpm history [package]` and `adpm status [package]` expose the ledger without requiring direct JSON inspection.

