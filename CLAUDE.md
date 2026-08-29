# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

ADPM (After Dark Systems Package Manager) bundles complex dependencies, especially C libraries, with Python and Go projects. Instead of fighting pip/brew/apt, it ships pre-compiled binaries in a platform-aware archive. A `.adpm` package is a compressed CPIO archive containing `META.json`, `INSTALL.sh`, per-platform `bin/` and `lib/` trees, optional preserved payloads, and Python wheels under `python/`.

## Commands

```bash
# Build the Go CLI (produces ./adpm)
./build.sh
./adpm --help

# Or manually
go build -o adpm cmd/adpm/main.go
```

Run the complete test suite with `tests/run.sh` (Python builder units, Go
archive/conversion tests, and installer lifecycle integration tests).

Legacy/script tooling (predates the Go CLI, still functional):

```bash
./builder/adpm-build.py --name X --version Y --libraries ... --python ...   # build a .adpm package
./installer/adpm-install.sh package.adpm                                    # install a package
./builder/make-self-extracting.sh package.adpm my-installer                 # self-extracting installer
```

## Architecture

Two layers that share the `.adpm` format:

1. **Go CLI** (`cmd/adpm/main.go` → `internal/cmd/`, cobra-based). Subcommands:
   - `build` — wraps `builder/adpm-build.py`
   - `install` — install/uninstall/list packages
   - `convert` — convert other package formats (e.g. rpm) to adpm (`internal/converter`)
   - `inspect` / `validate` / `merge` — package introspection, safety validation, and platform merging
   - `scan` — vulnerability-scan a package via its SBOM
   - `repo generate` — build an `index.json` catalog for a directory of packages
   - `script <file.star>` — run Starlark systems scripts via the **sysscript engine**
2. **Shell/Python tooling** (`builder/`, `installer/`) — the original packaging scripts the CLI shells out to.

The largest subsystem is `internal/sysscript/`: a Starlark (go.starlark.net) systems-scripting engine exposing host management builtins (accounts/cron, disk/network, firewall/SSL, HTTP, SSH, packages/containers, process inspection via gopsutil) plus config-file generators driven by templates in `internal/sysscript/templates/` (bind, nsd, unbound, postfix, etc.). Example package definitions live in `packages/` and `examples/`.

`ADPM_ENHANCEMENTS.md` and `TODO.md` track planned work; check them before adding features.
