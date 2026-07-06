# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

ADPM (AfterDark Package Manager) — a new package manager (listed as "adspm" in PRODUCTS.txt) for bundling complex dependencies, especially C libraries, with Python projects. Instead of fighting pip/brew/apt, it ships pre-compiled binaries in a platform-aware archive. A `.adpm` package is a **cpio.bz2 archive** containing `META.json`, `INSTALL.sh`, per-platform `bin/` and `lib/` trees (darwin-arm64, darwin-x86_64, linux-x86_64, linux-aarch64), and Python wheels under `python/`. The full format spec is in `ADPM_SPEC.md`; `README.md` walks through the icloud-cli/libimobiledevice motivating example.

## Commands

```bash
# Build the Go CLI (produces ./adpm_cli; named to avoid clashing with the adpm/ directory)
./build.sh
./adpm_cli --help

# Or manually
go build -o adpm_cli cmd/adpm/main.go
```

There are no Go test files in the repo (`go test ./...` finds nothing to run), and no lint config.

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
   - `scan` — vulnerability-scan a package via its SBOM
   - `repo generate` — build an `index.json` catalog for a directory of packages
   - `script <file.star>` — run Starlark systems scripts via the **sysscript engine**
2. **Shell/Python tooling** (`builder/`, `installer/`) — the original packaging scripts the CLI shells out to.

The largest subsystem is `internal/sysscript/`: a Starlark (go.starlark.net) systems-scripting engine exposing host management builtins (accounts/cron, disk/network, firewall/SSL, HTTP, SSH, packages/containers, process inspection via gopsutil) plus config-file generators driven by templates in `internal/sysscript/templates/` (bind, nsd, unbound, postfix, etc.). Example package definitions live in `packages/` and `examples/`.

`ADPM_ENHANCEMENTS.md` and `TODO.md` track planned work; check them before adding features.
