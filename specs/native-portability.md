# Native Portability Specification

## Acceptance criteria

1. `adpm build --detect-dependencies` inspects every supplied binary and library with the platform loader tooling.
2. Dependency discovery recursively follows non-system shared libraries and records unresolved loader references.
3. Linux packages exclude standard system libraries and use `patchelf` to set `$ORIGIN/../lib` for executables and `$ORIGIN` for libraries.
4. macOS packages exclude operating-system frameworks/libraries, copy non-system dylibs, and use `install_name_tool` to rewrite copied references to `@loader_path`-relative names.
5. `adpm build --relocate` implies dependency detection and fails clearly when required relocation tooling is unavailable.
6. Builder metadata records detected, bundled, excluded, and unresolved native dependencies.
7. `adpm validate` returns an error when metadata reports unresolved native dependencies.
8. Existing package builds remain unchanged unless dependency detection or relocation is requested.

