# ADPM Configuration Contract

Both configuration files use YAML syntax despite their `.cfg` extension. They
are data only; tags, executable expressions, and command interpolation are not
supported.

Precedence is: built-in defaults, `/etc/adpm/<file>`,
`$HOME/.adpm/<file>`, an environment override, then an explicit CLI path.
Later mappings merge recursively and later scalar/list values replace earlier
ones.

`adpm.cfg` controls the CLI and installer:

```yaml
database: ~/.local/share/adpm
prefix: ~/.local
trusted_keys: ~/.adpm/trustedkeys.gpg
defaults:
  build:
    compress: zstd
  install:
    verify: true
```

`adpm_auto.cfg` controls automation behavior and reusable answers:

```yaml
enabled: true
non_interactive: true
assume_yes: false
answers:
  replace_modified_file: false
defaults:
  install:
    verify: true
```

Automation answers never bypass signature, checksum, platform, dependency, or
file-ownership safety checks. Unknown keys are preserved so future automation
features can consume them without breaking older clients. When automation is
enabled, child processes receive the resolved policy as `ADPM_AUTO_ENABLED`,
`ADPM_NON_INTERACTIVE`, `ADPM_ASSUME_YES`, and JSON `ADPM_AUTO_ANSWERS`.
