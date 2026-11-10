# packnest — a runtime installer that just works on China's network

> **English** · [中文](README.md)

Command name: `pk` (the `packnest` alias points at the same binary). A single Go binary,
`CGO_ENABLED=0`, six platform targets, exactly two third-party libraries. It installs
**node / npm / pnpm / bun / deno / go / python**, ships China mirror profiles with real network
probing, pairs the newest compatible npm after installing Node, and uninstalls back to zero residue.

```bash
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh && pk install node@22
```

> Website (single page, zh + en): <https://nekoleamo.github.io/packnest/en/>
> Source on [GitHub](https://github.com/nekoleamo/packnest) · mirror on [gitee](https://gitee.com/null_593_5354/packnest)
> Design notes and progress: [docs/progress.md](docs/progress.md)

## Install packnest itself

```bash
# ① curl one-liner (recommended; served by gitee, never touches GitHub)
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh

# ② Windows PowerShell one-liner
iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB | iex

# ③ Homebrew (own tap; formula is named packnest, the command is pk)
brew install nekoleamo/packnest/packnest

# ④ Scoop
scoop bucket add nekoleamo https://github.com/nekoleamo/scoop-bucket
scoop install packnest

# ⑤ npm (only if you already manage toolchains with npm: one extra Node dependency, easiest upgrades)
npm i -g packnest
npm i -g packnest --registry=https://registry.npmmirror.com   # force the npmmirror mirror

# ⑥ Self-hosted (drop the dist/ from `make release` on any static host)
curl -fsSL https://your-host/install.sh | sh -s -- --base https://your-host/packnest
```

Installer switches: `--mirror cn|default` · `--from npm|gitee|base` · `--base URL` · `--scope` ·
`--version` · `--dir` · `--skip-verify` · `--dry-run`. The download is **verified with sha256** (gitee and
self-hosted channels against `checksums.txt`, the npm channel against the registry `dist.shasum`); a
mismatch exits with code 4. The PowerShell script uses `-Mirror` / `-Base` / `-Version` / `-Dir` /
`-SkipVerify` / `-DryRun` (`-Mirror cn` pulls the npm platform package from npmmirror).

## Quick start

```bash
pk install node@22          # install Node and swap in the newest compatible npm
pk list                     # installed tools (* = global default, with disk usage)
pk list node                # one tool: installed + installable versions (one per line, newest last)
pk list --available         # newest installable version per tool (24h cache, offline-readable)
pk which node               # which real binary a shim resolves to
pk use node@22.23.4         # switch the global default
pk use node@22.22.2 --project   # pin to the current project (pk.toml + pk.lock, global untouched)
pk env --shell zsh          # print PATH/registry snippet; --write puts it in the rc block
pk doctor                   # health check (PATH / ledger / mirrors / global packages / other managers)
pk gc --dry-run             # dry-run the reclamation of unreferenced versions and cache
```

Nine fixed commands: `install` · `uninstall` · `use` · `list` · `which` · `mirror`
(list/probe/use/write) · `doctor` · `env` · `gc`, plus `version` / `help`
(`pk help install` documents each one).

Global flags: `--json` (the only contract for GUIs and scripts) · `--offline` · `--yes` ·
`--root` (debug). Fixed exit codes: `0` ok / `1` generic / `2` usage / `3` network / `4` checksum /
`5` locked.

## Differences from proto / mise / nvm

| Them | packnest |
|---|---|
| WASM plugins fetched from GitHub/OCI on first use | providers are **embedded TOML**, zero download; external providers are TOML only, never executable code |
| users write regexes to rewrite mirror URLs | built-in `cn`/`tuna`/`aliyun`/`ustc` profiles plus `auto` **real probing** (direct abroad without extra requests, mirror fallback in China, cached 6h) |
| Node and npm fully decoupled | **npm is Node's companion**, looked up by `engines.node` (pre-releases excluded, patch-level boundaries) |
| uninstall means deleting a directory | **ledger-driven**: atomic writes + automatic `.bak` recovery + reference counting + GC, only what it recorded |
| registry configuration ignored | registry environment generated per profile for npm/pnpm/bun/pip/go, **without touching** `~/.npmrc` by default |

## Picking a version

```bash
pk install node@22.22.2   # exact (skips index resolution, still verifies checksums)
pk install node@22        # newest 22.x patch
pk install node@latest    # newest stable
pk install node@lts       # newest LTS
pk install node@22 --dry-run   # print version / artifact URL / checksum mode, download nothing
```

## Global vs project scope

`install` takes its scope from the current directory; `use` has explicit flags:

| Action | Behaviour |
|---|---|
| `pk install` (**no** `pk.toml` here) | installs into `~/.packnest/tools/<tool>/<ver>`; becomes the global default if that tool has none |
| `pk install` (**with** `pk.toml`) | installs what is declared and writes/refreshes `pk.lock` (project pinning) |
| `pk use node@22.22.2` | pins the **global** default (config.toml + ledger reference + shim re-point) |
| `pk use node@22.22.2 --project` | pins the **project**: writes `pk.toml` and regenerates `pk.lock`, global default untouched |
| `pk use --lock` | only refreshes `pk.lock` from `pk.toml` (no switch, no install; use it after `latest`/`lts`) |
| `pk env --project` | puts the locked versions first on PATH inside the project (direnv) |

There is only ever one copy on disk; project scope means "pinning + PATH order", never a duplicate.
`pk.toml` is edited by textual merge, so your comments, ordering and grouping survive.

## Engineering constraints (enforced as gates)

- **Two** third-party dependencies: `Masterminds/semver/v3`, `BurntSushi/toml`
- Archives: **tar.gz / zip** only, extracted with the standard library (`archive/tar` + `compress/gzip` + `archive/zip`)
- Single `PK_ROOT` (default `~/.packnest`), **never sudo, never writes your shell rc** (except the managed block from `pk env --write`)
- Never modifies existing user configuration by default: no writes to `~/.npmrc` (use `pk mirror write`, which backs up and is additive)
- `go test ./...` is **offline** by default; real-network tests are `//go:build nettest` and stay out of the gate

## Downloads and verification

- Downloads: Range resume (`.part` + `.part.meta`), ETag/304, split timeouts (connect 5s / first byte 8s), at most 2 retries per source; **single connection by default** (CDN sharding is usually slower in China; `--jobs` is reserved)
- Checksums: `SHASUMS256.txt` / per-artifact `.sha256sum` / registry integrity (sha1 or sha512 SRI) / inline sha256 from the Go index
- **TOFU + multi-source cross-check**: a first install requires ≥2 sources to agree (otherwise marked `weak`) and records the digest in `state/known.json`; afterwards that version verifies **fully offline**
- Sources disagree → hard failure that states which source the artifact matches; `--insecure` lets it through (marked unverified, never written to the TOFU table)

## Global packages (`npm i -g`) and switching versions

- Shared prefix `~/.packnest/global` (`npm_config_prefix`), with `globalconfig` exported too → **switching Node loses neither global packages nor `npm config set -g`**
- Package cache stays at `~/.npm` (never migrated or copied); `global/bin` is a fixed directory, so new global packages **need no reshim**
- `pk doctor --globals` runs three checks: `GLOBAL_ENGINES_UNMET`, `GLOBAL_NATIVE_ABI` (N-API prebuilds are fine), `GLOBAL_SHADOWS_PROJECT`
- Want nvm-style behaviour: `global_mode = "per-version"`

## Coexisting with existing managers

`pk` writes only `~/.packnest` and **never modifies other managers' directories**. `pk doctor
--foreign` lists Node versions from proto/nvm/volta/asdf/mise/fnm read-only, and warns
(`FOREIGN_SHIM_PRIORITY`) when someone else's shims come before `~/.packnest/bin`.

Recommended: coexist first → `pk install node@22 && pk which node` to verify → then decide yourself
whether to delete the other manager's init line from your rc.

## Project lock and CI

```toml
# pk.toml (commit this)
[tools]
node = "22"
npm  = "auto"
```

`pk install` writes `pk.lock` (version/sha256/source only, no URLs → switching mirrors does not
invalidate the lock). Day-to-day version changes use `pk use node@X --project` (manifest and lock in
one command); lock-only refreshes use `pk use --lock`. In CI, `pk install --frozen` installs exactly
the lock and exits non-zero on any drift (aliases are rejected — pin them first).

## Development

```bash
make build      # CGO_ENABLED=0 local build → ./bin/pk
make test       # offline unit tests
make accept     # acceptance tests only (TestAcceptance_*)
make check      # the seven gates (run before every commit)
make release    # six platform artifacts + checksums.txt → dist/
make site       # render the website into site/dist/ (version injected from the git tag)
make npm-dry    # build the npm packages and validate manifests (no publish, no credentials)
```

## Documentation

| Document | Contents |
|---|---|
| [docs/en/cli.md](docs/en/cli.md) | **CLI reference** — all nine commands with examples |
| [docs/en/troubleshooting.md](docs/en/troubleshooting.md) | Troubleshooting, including coexistence with proto/nvm/mise |
| [docs/json-schema.md](docs/json-schema.md) | The `--json` v1 contract (fields and issue codes) |
| [docs/cli.md](docs/cli.md) | The Chinese manual (more narrative detail) |
| [docs/progress.md](docs/progress.md) | Milestone progress, per-round deliveries and pitfalls |

## Known limitations

- `--jobs` is reserved (downloads are single-connection today)
- The Python index only declares npmmirror (upstream GitHub has no directory listing); the Go index only declares the official `?mode=json&include=all` and falls back to a 24h stale cache
- pnpm 12's registry tarball is only a launcher — the first run downloads the platform binary itself (upstream behaviour)
- The cache is named `<tool>-<version>-<platform>.<ext>` (no content-addressed dedup yet)
- Per-project override of the global version requires direnv (`pk env --project`); without direnv `node` still resolves to the global default
- The GUI (Wails v3.1.0) has not started — it comes after the CLI freeze

## License

MIT, see [LICENSE](LICENSE).
