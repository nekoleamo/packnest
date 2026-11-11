# pk CLI reference (English)

> **English** · [中文](../cli.md)
>
> Applies to **packnest v0.7.x**. Every example can be copy-pasted; lines starting with `$` are
> commands, everything else is sample output. The Chinese version ([docs/cli.md](../cli.md)) carries
> the same material with more narrative detail — this page is the practical reference.
>
> The command surface is fixed at 9: `install` `uninstall` `use` `list` `which` `mirror` `doctor`
> `env` `gc`, plus `version` and `help`.

## Contents

- [0. One-minute start](#0-one-minute-start)
- [1. install](#1-install)
- [2. list](#2-list)
- [3. which](#3-which)
- [4. use](#4-use)
- [5. uninstall](#5-uninstall)
- [6. mirror](#6-mirror)
- [7. doctor](#7-doctor)
- [8. env](#8-env)
- [9. gc](#9-gc)
- [10. version / help](#10-version--help)
- [11. Global flags and exit codes](#11-global-flags-and-exit-codes)
- [12. Common recipes](#12-common-recipes)

---

## 0. One-minute start

```bash
# install packnest itself (recommended: one line from gitee, no GitHub access)
$ curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh
# Windows: iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB | iex
# Already managing toolchains with npm? npm i -g packnest works too.

# pick a mirror profile (once; afterwards `auto` decides by itself)
$ pk mirror use cn

# install Node — the newest compatible npm comes with it
$ pk install node@22

# make the shim directory visible to your shell
$ pk env --write && source ~/.zshrc
$ node -v && npm -v
```

`pk env --write` is the only command that touches your shell rc, and it only rewrites its own
`# >>> packnest >>>` block. Without it you can use `eval "$(pk env --shell bash)"` per session.

## 1. install

```
pk install [工具[@版本]]... [--frozen] [--insecure] [--force] [--force-rehash] [--quiet] [--dry-run]
```

Scope is decided by the current directory, not by a flag:

| Directory | Behaviour |
|---|---|
| has `pk.toml` | installs what the file declares and writes/refreshes `pk.lock` |
| no `pk.toml` | installs into the shared root and **sets it as the global default** if that tool has none |

### 1.1 Version syntax

| Spec | Meaning |
|---|---|
| `node@22` | newest patch of 22.x |
| `node@22.22` | newest patch of 22.22.x |
| `node@22.22.2` | exact version |
| `node@latest` | newest stable |
| `node@lts` | newest LTS |
| `node@^20.17` / `node@>=20` | semver range, passed to the constraint solver |

```bash
$ pk install node@22            # newest 22.x
$ pk install node@22.22.2       # exact; still fetches checksums
$ pk install node@22 --dry-run  # resolve only: version + artifact URL + checksum source
$ pk install                    # install everything declared in ./pk.toml
```

### 1.2 Tools

`node` · `npm` · `pnpm` · `bun` · `go` · `python`

`npm` is not installed on its own: it is a companion of Node, paired by exact Node version. `pnpm`
requires Node to be present first — runtimes are never installed implicitly.

### 1.3 Offline install

If the artifact is already cached, the install performs **zero network requests** and verifies
against the local TOFU table:

```bash
$ pk --offline install node@22.22.2
```

### 1.4 CI: strict lock file

```bash
$ pk install --frozen
```

`--frozen` resolves nothing: it installs exactly what `pk.lock` says and exits non-zero if
`pk.toml` and the lock disagree (floating aliases such as `latest`/`lts` are rejected — pin them with
`pk use --lock` first).

### 1.5 Other flags

| Flag | Effect |
|---|---|
| `--insecure` | allow an install whose checksums cannot be cross-verified; marked `unverified` and **not** written to the TOFU table |
| `--force-rehash` | ignore the local TOFU record, re-evaluate against the checksum sources and overwrite that entry |
| `--force` | reinstall a version that already exists |
| `--force-rehash` | ignore the local TOFU record, re-evaluate against the checksum sources and overwrite that entry (use when upstream re-published an artifact, or the table went stale) |
| `--quiet` | errors only, no progress |
| `--json` | NDJSON event stream: `resolve` / `mirror` / `plan` / `cache` / `download` / `verify` / `companion` / `done` |

## 2. list

```
pk list [--available [--refresh]] [--json]
```

```bash
$ pk list
Installed:
  * node   22.23.4   177.5 MB (npm 12.2.2, bundled 10.9.9)
    pnpm   12.19.0    49.4 MB

$ pk list node                 # one tool: installed versions + installable versions
node: 877 versions installable (index latest 26.13.0, lts 26.13.0)
  installed  * 22.23.4
  available  (latest 20 shown, oldest → newest, newest at the bottom)
             26.11.0
             26.11.1
             26.12.0
             26.12.1
             26.13.0  latest · lts: Lithium
             …872 older ones above (pk list node --all, or --limit N)
  install one: pk install node@<version>

$ pk list node --all           # all 877 → switches to the grouped grid
node: 877 versions installable (index latest 26.13.0, lts 26.13.0)
  installed  (nothing yet)
  available  (all 877, oldest → newest, newest at the bottom)
   22.x  22.0.0     22.1.0     …  22.11.0l  22.11.1l …
   26.x  26.0.0     …         26.13.0 *
         legend: * latest · l LTS · + installed · @ current default
  install one: pk install node@<version>

$ pk list node --limit 5       # just the newest 5
$ pk list node --major 22      # every 22.x
$ pk list node --major 0.10    # 0.x groups by major.minor

$ pk list --available          # one line per tool: latest + 5 recent (24h cache, offline-readable)
$ pk list --available --refresh  # ignore the cache and go to the network
$ pk list node --json          # {tool, latest, lts, total, installed[], available[]}
```

Layout follows nvm/pyenv/fnm: **one version per line, ascending, newest at the bottom**, right-aligned
into a column with a tag column after it (`latest`, `lts: <codename>`, `已装` / `已装 · 当前默认`). Index fetches for npm/pnpm use the registry's **abbreviated
metadata** (~2 MB instead of ~25 MB) and every index fetch has a 45 s total timeout, so a slow
mirror reports "index fetch timed out" instead of hanging forever.

`*` marks the global default. `--json` returns `{schema, tools:[{name, version, active, path, npm,
requires, verified, size_bytes}]}`.

## 3. which

```
pk which <command> [--json]
```

```bash
$ pk which node
/Users/you/.packnest/tools/node/22.23.4/bin/node
```

`pk which pk` and `pk which packnest` must resolve to the same file — that is the dual-name
consistency check.

## 4. use

```
pk use <tool>@<version> [--project]
pk use --lock
```

| Form | Effect |
|---|---|
| `pk use node@22.22.2` | switch the **global** default: config.toml + ledger reference + shim re-point |
| `pk use node@22.22.2 --project` | pin to the **current project**: write `pk.toml` and regenerate `pk.lock`, global default untouched |
| `pk use --lock` | only regenerate `pk.lock` from the existing `pk.toml` (no switch, no install) |

`use` only switches between **already installed** versions. `pk.toml` is edited by textual merge, so
your comments, ordering and grouping survive.

> After using a floating alias (`latest` / `lts`), run `pk use --lock` to pin the concrete version —
> otherwise `pk install --frozen` in CI will reject the alias.

## 5. uninstall

```
pk uninstall <tool>@<version> [--force]
pk uninstall --all [--purge] [--keep-cache] [--yes]
```

`--self` also removes the `pk` binary itself (the installer scripts record their target directory in
`state/install.json`): on macOS/Linux it unlinks the running binary; on Windows it prints the
`Remove-Item` command, because a running exe cannot be deleted.

`--all` removes everything pk installed: every tool directory, the shims in `~/.packnest/bin`, the
managed block in your shell rc (on Windows also the user Path registry entries), and prints the
command that removes the `pk` binary itself for your install channel. Without `--yes` it only prints
the plan. `--purge` also deletes `~/.packnest`; add `--keep-cache` to keep `cache/` for offline
reinstalls.

```
pk uninstall <tool>@<version> [--force]
```

Ledger-driven: only directories recorded in `state/installs.json` are removed, and referenced
versions (global default / project lock / required by another tool) need `--force`. The artifact
cache is kept, so reinstalling offline still works. Empty `tools/<tool>/` directories are cleaned up.

## 6. mirror

```
pk mirror list|probe|use <profile>|write
```

| Sub-command | Effect |
|---|---|
| `list` | built-in profiles (`default` / `cn` / `tuna` / `aliyun` / `ustc` / `auto`) and the current one |
| `probe` | probe index sources in parallel (results cached 6h); `--tool node` probes just one |
| `use` | switch profile (writes `config.toml`) |
| `write` | write the current profile's registry into `~/.npmrc` (backs up first, additive only) |

`auto` probes the official index once (500ms HEAD) on the first install: reachable means direct with
no extra request abroad, otherwise it falls back to a domestic mirror. The result is cached for 6h
and shows up in `pk mirror list`.

## 7. doctor

```
pk doctor [--json] [--globals] [--probe] [--foreign]
```

Issue codes are a stable contract (GUIs and scripts depend on them):

| Code | Meaning |
|---|---|
| `PATH_ORDER` / `PATH_MISSING` | the shim directory is not first on PATH / not on PATH at all |
| `LEDGER_BROKEN` / `LEDGER_RECOVERED` | ledger corrupt / recovered from `.bak` |
| `CONFIG_BROKEN` / `MIRROR_PROFILE_INVALID` | config unreadable / unknown profile |
| `MIRROR_UNREACHABLE` | index source unreachable (`--probe` only) |
| `GO_TOOLCHAIN_NOT_LOCAL` | Go is installed but `GOTOOLCHAIN != local` |
| `GO_INSTALL_NOT_IN_PATH` | `go install` output dir (`GOBIN`, default `~/go/bin`) is not in PATH | add it to PATH or set `GOBIN` (we never set it for you) |
| `PIP_USER_SITE_UNMANAGED` | pip user-site installs (`~/.local/lib/pythonX`) are outside pk's scope | use a venv |
| `PROVIDERS_DIRS` | effective external provider manifest dirs (info) | absent ⇒ no external manifests |
| `PROVIDER_EXTERNAL_INVALID` | external manifest rejected (http not opted in / absolute path / `..` / pbs / unknown placeholder / >256KB / id ≠ filename) | fix the TOML; `pk doctor` names the file and reason |
| `PROVIDER_EXTERNAL_CONFLICT` | external manifest duplicates an existing id and is ignored | embedded always wins; pick a new id |
| `PROVIDER_HTTP_URL` | external manifest has `http://` sources let in by `PK_ALLOW_HTTP=1` | verification caps at `weak`; prefer https |
| `GLOBAL_ENGINES_UNMET` / `GLOBAL_NATIVE_ABI` / `GLOBAL_SHADOWS_PROJECT` | global package checks (`--globals`) |
| `FOREIGN_MANAGER` | proto / nvm / volta / asdf / mise / fnm / nodenv detected (read-only) |
| `FOREIGN_SHIM_PRIORITY` | another manager's shims come before `~/.packnest/bin` and will win |

Exit code is 1 when any `error`-severity issue is present, 0 otherwise.

## 8. env

```
pk env [--shell bash|zsh|sh|powershell] [--write] [--unset] [--completion SHELL] [--project] [--rc PATH]
```

```bash
$ pk env --shell zsh               # print only, nothing on disk
$ pk env --write                   # write the managed block into your rc (idempotent)
                                   # Windows: PATH goes to the user Path registry value (immune to
                                   # ExecutionPolicy); the other vars go to the PowerShell profile
$ pk env --unset                   # remove that block again
$ pk env --completion zsh          # shell completion: eval "$(pk env --completion zsh)"
$ pk env --project                 # put the locked project versions first on PATH (direnv)
```

The snippet contains the five PATH segments (`bin` → `global/corepack/bin` → `global/bin` →
`global/pnpm` → `global/bun/bin`) plus registry/shared-prefix variables
(`npm_config_prefix`, `npm_config_cache`, `PNPM_HOME`, `BUN_INSTALL`, `COREPACK_HOME`,
`COREPACK_NPM_REGISTRY`, `GOPROXY`, `PIP_INDEX_URL`, `GOTOOLCHAIN=local`).

## 9. gc

```
pk gc [--dry-run] [--cache-max 5GB] [--json]
```

Reclaims unreferenced versions (referenced ones are listed under `KeptRef`) and prunes the artifact
cache by LRU down to `cache_max` (default from `config.toml`). `--dry-run` reports without touching
the disk.

## 10. version / help

```bash
$ pk version          # packnest v0.7.x (go1.27.3, darwin/arm64)
$ pk version --json   # {schema, name, version, go, os, arch}
$ pk help install     # per-command help
```

## 11. Global flags and exit codes

| Flag | Effect |
|---|---|
| `--json` | machine-readable output — the only contract for GUIs and scripts (`schema: 1`, additive only) |
| `--offline` | forbid every network request |
| `--yes` | no interactive confirmation |
| `--root DIR` | override `PK_ROOT` (debugging / testing) |

| Exit code | Meaning |
|---|---|
| `0` | success |
| `1` | generic failure |
| `2` | usage error |
| `3` | network failure |
| `4` | checksum failure |
| `5` | another `pk` holds the lock |

## 12. Common recipes

```bash
# project-scoped pin (writes pk.toml + pk.lock, global default untouched)
$ pk use node@22.22.2 --project

# what would gc reclaim?
$ pk gc --dry-run

# which mirror actually answers right now?
$ pk mirror probe --tool node

# point every tool at an internal mirror
$ export PK_MIRROR_REGISTRY=https://npm.internal.example.com
$ export PK_MIRROR_NODE_ARTIFACT=https://mirror.internal/node/v{version}/node-v{version}-{platform}.{ext}

# verify a checksum mismatch story before overriding anything
$ pk install node@22.22.2 --dry-run      # shows artifact URL + checksum mode
```

Environment overrides: `PK_MIRROR_REGISTRY`, `PK_MIRROR_<TOOL>_INDEX`, `PK_MIRROR_<TOOL>_ARTIFACT`,
`PK_MIRROR_<TOOL>_CHECKSUM`, `PK_ROOT`, `PK_GITEE_REPO`, `PK_NPM_REGISTRY`, `PK_INSTALL_BASE`.
