# packnest — a runtime installer that just works on China's network

> **English** · [中文](README.md)

Command name: `pk` (the `packnest` alias points at the same binary). A single Go binary,
`CGO_ENABLED=0`, six platform targets, exactly two third-party libraries. It installs
**node / npm / pnpm / bun / go / python**, ships China mirror profiles with real network
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

# ⑤ Self-hosted (drop the dist/ from `make release` on any static host)
curl -fsSL https://your-host/install.sh | sh -s -- --base https://your-host/packnest
```

Outside China you can pull the very same artifacts from GitHub Releases (identical sha256):
`curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh -s -- --base https://github.com/nekoleamo/packnest/releases/download`

Installer switches: `--from gitee|base` · `--base URL` · `--version` · `--dir` · `--skip-verify` ·
`--dry-run` (`--mirror cn|default` is still accepted but both now mean gitee). The download verifies
the **`checksums.txt.sig` (sshsig)** first and then the **sha256**; a mismatch exits with code 4. The
PowerShell script uses `-Base` / `-Version` / `-Dir` / `-SkipVerify` / `-DryRun`.

`--base` / `-Base` is an **authoritative source**: it must serve a `latest` file (one line, the
version) or you must pass `--version`. Otherwise the installer fails loudly instead of silently
falling back to gitee's newest version (which points at a directory that does not exist on your host
→ download 404).

> The npm channel was retired on 2026-11-11: staged publishing needs 2FA, and archives built in the
> cloud differ byte-for-byte from local builds (two sets of sha256). Installs made earlier with
> `npm i -g packnest` keep working, and `pk uninstall --all` still recognises that channel.

## Quick start

```bash
pk install node@22          # install Node and swap in the newest compatible npm
pk list                     # installed tools (* = global default, with disk usage)
pk list node                # one tool: installed + installable versions (grouped grid when long)
pk list node --major 22     # every 22.x; --all for the whole list
pk list --available         # newest installable version per tool (24h cache, offline-readable)
pk which node               # which real binary a shim resolves to
pk use node@22.23.4         # switch the global default
pk use node@22.22.2 --project   # pin to the current project (pk.toml + pk.lock, global untouched)
pk env --shell zsh          # print PATH/registry snippet; --write puts it in the rc block
pk doctor                   # health check (PATH / ledger / mirrors / global packages / other managers)
pk gc --dry-run             # dry-run the reclamation of unreferenced versions and cache
pk upgrade --self --check   # check whether pk itself has a newer release (source picked by network)
pk upgrade --self           # upgrade pk itself: download → verify (sshsig + sha256) → atomic replace
```

Ten fixed commands: `install` · `uninstall` (`<tool>[@<version>]` / `--all`) · `use` ·
`list` (`[tool]` / `--available`) · `which` · `mirror` (list/probe/use/write) · `doctor` · `env` · `gc` ·
`upgrade` (upgrades installed tools; `--self` upgrades pk itself), plus `version` / `help`.

Global flags: `--json` (the only contract for GUIs and scripts) · `--offline` · `--yes` ·
`--root` (debug). Fixed exit codes: `0` ok / `1` generic / `2` usage / `3` network / `4` checksum /
`5` locked. On failure `--json` also emits one object **without** an `ev` field —
`{"schema":1,"error":{"code","args","tool","spec","msg","exit"}}` (since 1.8, additive only; the
human stderr line and the exit code are unchanged, `code` is omitted when no stable code exists).

## Desktop app (optional)

`pk-desktop` is a **visual supplement** to the CLI (Wails v3 + Vue3, a separate module): the CLI must be installed first,
and every action in the UI is a subprocess call to `pk <command> --json`. It has no ledger of its own: the CLI upgrades itself via
`pk upgrade --self`, while the desktop app swaps **its own** artifacts. The in-app **Updates** card shows both versions,
picks the download source from current network conditions (gitee / GitHub) and asks for a restart after upgrading.

**Install the CLI first**, then pick one row for your platform:

| Platform | Way | Command / file | Verification |
|---|---|---|---|
| **macOS** (arm64) | one-liner (recommended) | `curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install-gui.sh \| sh`<br>options: `--version` / `--dir` (default `~/Applications`) / `--dry-run` | `checksums.txt.sig` (sshsig) first, then sha256 |
| | manual | `packnest-desktop_<version>_darwin_arm64.app.zip` → unzip → `open packnest.app` | check the manifest yourself |
| **Windows** (amd64 / **arm64**) | PowerShell one-liner (recommended) | `iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install-gui.ps1 -UseB \| iex` | same as macOS (sshsig + sha256) |
| | Scoop | `scoop bucket add nekoleamo https://github.com/nekoleamo/scoop-bucket`<br>`scoop install packnest-desktop` (`depends: packnest`, installs the CLI first) | sha256 from the manifest |
| | manual | `packnest-desktop_<version>_windows_{amd64,arm64}.zip` → unzip → `pk-desktop.exe` | check the manifest yourself |
| **Linux** (amd64) | manual (no script) | `packnest-desktop_<version>_linux_amd64.tar.gz` → unpack → `./pk-desktop`<br>needs `sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev` | check the manifest yourself |

Artifacts live under a **separate tag** `gui-v<version>` (decoupled from the CLI version):
[gitee](https://gitee.com/null_593_5354/packnest/releases) · [GitHub Releases](https://github.com/nekoleamo/packnest/releases),
together with `checksums.txt` + `checksums.txt.sig` (same release key as the CLI).

- macOS builds are **ad-hoc signed** (not notarised): allow once in System Settings → Privacy & Security;
- there is no Intel Mac build (run `make gui-app` yourself) and **no Homebrew cask** (ad-hoc signing cannot pass Gatekeeper);
- no desktop runtime needed for `pk-desktop --serve` → `http://127.0.0.1:7799/`.

- installed-tools table with an `upgrade` hint (one click = `pk install <tool>@latest`), `weak` / unverified / npm badges, set default, uninstall, copy path
- multi-spec install queue with per-row progress (the NDJSON `progress` event)
- mirror profile switching with latency probing; doctor page (global packages in a table, `--probe` to really load native modules, **Fix now** = `pk doctor --fix`)
- reclaim page: preview or delete, `--keep N` keeps the newest N versions per tool
- read-only `which` / `env` views, light/dark toggle, Chinese/English UI, and launch at login (off by default)

If the CLI is missing, the app shows the install command for your platform with a copy button.
macOS builds are ad-hoc signed (allow once in System Settings → Privacy & Security); Linux needs GTK4 + WebKitGTK 6.0.
`sh scripts/install-gui.sh` (macOS) / `install-gui.ps1` (Windows) download, verify sshsig + sha256, and unpack into `~/Applications`.
No desktop runtime? Use `pk-desktop --serve` → `http://127.0.0.1:7799/`.

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
pk install node@22 --dry-run   # print version / artifact URL / checksum mode — writes nothing
```

## Global vs project scope

`install` takes its scope from the current directory; `use` has explicit flags:

| Action | Behaviour |
|---|---|
| `pk install` (**no** `pk.toml` here) | installs into `~/.packnest/tools/<tool>/<ver>`; becomes the global default if that tool has none |
| `pk install` (`package.json` only) | uses its `engines.node` as the node range (`pk.toml` wins when both exist) and does **not** write `pk.lock` |
| `pk install` (**with** `pk.toml`) | installs what is declared and writes/refreshes `pk.lock` (project pinning) |
| `pk use node@22.22.2` | pins the **global** default (config.toml + ledger reference + shim re-point) |
| `pk use node@22.22.2 --project` | pins the **project**: writes `pk.toml` and regenerates `pk.lock`, global default untouched. If the version is missing it asks first (`--yes` installs it; `--json` / non-TTY exits 1) |
| `pk use --lock` | only refreshes `pk.lock` from `pk.toml` (no switch, no install; use it after `latest`/`lts`) |
| `pk env --project` | puts the locked versions first on PATH inside the project (direnv) |

There is only ever one copy on disk; project scope means "pinning + PATH order", never a duplicate.
`pk.toml` is edited by textual merge, so your comments, ordering and grouping survive.

## Engineering constraints (enforced as gates)

- **Two** third-party dependencies: `Masterminds/semver/v3`, `BurntSushi/toml`
- Archives: **tar.gz / zip** only, extracted with the standard library (`archive/tar` + `compress/gzip` + `archive/zip`)
- Single `PK_ROOT` (default `~/.packnest`), **never sudo, never writes your shell rc** (except the managed block from `pk env --write`)
- Cache is the one exception: `PK_CACHE=<dir>` moves `archives/`, `alias/`, `meta/` and the corepack/npm caches elsewhere (CI volumes); ledger, shims and locks stay put
- Never modifies existing user configuration by default: no writes to `~/.npmrc` (use `pk mirror write`, which backs up and is additive)
- `go test ./...` is **offline** by default; real-network tests are `//go:build nettest` and stay out of the gate

## Downloads and verification

- Downloads: Range resume (`.part` + `.part.meta`), ETag/304, split timeouts (connect 5s / first byte 8s), at most 2 retries per source; **single connection** (CDN range sharding measured slower than one stream in China)
- Checksums: `SHASUMS256.txt` / per-artifact `.sha256sum` / registry integrity (sha1 or sha512 SRI) / inline sha256 from the Go index
- **TOFU + multi-source cross-check**: a first install requires ≥2 sources to agree (otherwise marked `weak`) and records the digest in `state/known.json`; afterwards that version verifies **fully offline**
- Sources disagree → hard failure that states which source the artifact matches; `--insecure` lets it through (marked unverified, never written to the TOFU table)

## External provider manifests (long-tail tools without waiting for a release)

The embedded TOML set covers 6 tools. For anything else (zig / uv / jdk…), drop a manifest in —
**manifests are pure data, nothing from them is ever executed**.

```bash
~/.packnest/providers/zig.toml     # filename must equal the id (zig.toml <-> id = "zig")
PK_PROVIDERS_DIR=/opt/corp/pk:/srv/more   # extra dirs (multiple, path-separator; http(s):// not allowed)
```

Load order: embedded → `$PK_ROOT/providers` → `PK_PROVIDERS_DIR`. **Embedded always wins**
(a duplicate id from outside is ignored; `pk doctor` reports `PROVIDER_EXTERNAL_CONFLICT`).

Safety rules for external manifests (a violation only rejects that one file —
`pk doctor` reports `PROVIDER_EXTERNAL_INVALID` with the filename and reason):

- URLs must be `https://`; internal `http` sources require an explicit `PK_ALLOW_HTTP=1`
  (`pk doctor` reports `PROVIDER_HTTP_URL`)
- `layout.single` / `bin_targets` must be relative and free of `..`; `strip` must not be negative
- `index.kind = "pbs"` is not allowed (python's directory parsing lives in the embedded logic)
- URL templates may only use `{version} {platform} {ext} {registry} {tool} {filename} {tag}`
- `checksum.urls` is required (except `registry-integrity` / `index-inline`, where the digest comes from the index);
  `{filename}` falls back to the artifact URL's basename on indexes without a file list (`github-releases` / `dirlist`)
- `[companions.<name>]` `registry` must point at an existing tool id; `mode = bundled|latest-compatible|pinned`,
  `target` is relative to the tool root (node→npm is the `latest-compatible` special case with engines lookup);
  `pinned` requires a `version`. A companion is part of its host: it is uninstalled/GC'd with the host and stays out of the ledger
- one manifest ≤ 256KB (so a stray binary cannot masquerade as a manifest)

- **Upstream re-publish** (the same version's artifact was replaced upstream): a TOFU mismatch no longer forces
  `--force-rehash` when the checksum source on the **artifact's own host** agrees with the new digest *and* a source on
  another host still reports the old one (cross-source split). It is accepted as `weak`, the record is rewritten, and the
  `verify` event's `msg` explains why; a same-origin claim alone is still a hard failure (that could be a swapped mirror).

**Verification strength follows the checksum source, not the artifact**: if the artifact *and* the
checksums come over http, the result caps at `weak`; artifact over http but ≥2 agreeing https
checksum sources is still `strong`. The ledger records `transport: "http"`.

`pk list` marks external tools with `*`, and `--json` adds `"source": "embedded" | "external"`
(additive only). A broken manifest **never stops the CLI from starting**: it is skipped and recorded.

## What the toolchain already ships (corepack / pip / `go install`)

- **corepack** (bundled with node ≤24): `pk install node@22 --corepack` installs `yarn`/`yarnpkg`/`pnpm`
  shims into `~/.packnest/global/corepack/bin` (PATH segment #2); `--no-corepack` turns it off
  (stored in `config.toml`). A provider-installed `pnpm` always wins. node 25+ dropped corepack, so
  `--corepack` there **fails loudly** and suggests `pk install pnpm`. First use fetches from the
  registry via `COREPACK_NPM_REGISTRY` (your mirror profile); cache goes to `~/.packnest/cache/corepack`.
- **pip**: `pip3` ships with python and `PIP_INDEX_URL` comes from `pk env`; **user-site installs
  (`~/.local/lib/pythonX`) are out of scope** (`PIP_USER_SITE_UNMANAGED`), use a venv.
- **`go install`**: lands in `GOBIN` (default `~/go/bin`) — **we never set GOBIN nor add a PATH segment**;
  `pk doctor` reports `GO_INSTALL_NOT_IN_PATH` when it is not on PATH.

## Global packages (`npm i -g`) and switching versions

- Shared prefix `~/.packnest/global` (`npm_config_prefix`), with `globalconfig` exported too → **switching Node loses neither global packages nor `npm config set -g`**
- Package cache stays at `~/.npm` (never migrated or copied); `global/bin` is a fixed directory, so new global packages **need no reshim**
- `pk doctor --globals` runs three checks: `GLOBAL_ENGINES_UNMET`, `GLOBAL_NATIVE_ABI` (N-API prebuilds are fine), `GLOBAL_SHADOWS_PROJECT`
- The managed block **does not inject** `npm_config_prefer_offline` (since 1.9.1): npm's offline-preference bypasses
  the staleness check on cached packuments, so `npm i -g x@latest` / `npx x@latest` keep resolving to an old version.
  Machines whose block was written by 1.9.0 or earlier should re-run `pk env --write` after upgrading
  (see [docs/en/troubleshooting.md](docs/en/troubleshooting.md) §2.7)
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

## English output

Chinese is the default. `--lang=en` (or `export PK_LANG=en`) switches CLI output and `--help`
to English; the `--json` structure never changes (`code`/`ev`/exit codes are the contract,
`msg` strings may stay Chinese). `LANG` is deliberately ignored so CI logs never flip language silently.

Releases are signed since 1.6: `checksums.txt.sig` (sshsig, namespace `pk-release`) is verified by
`install.sh` / `install.ps1` before the sha256 comparison. `brew`/`scoop` only check sha256.

## Development

```bash
make build      # CGO_ENABLED=0 local build → ./bin/pk
make test       # offline unit tests
make accept     # acceptance tests only (TestAcceptance_*)
make check      # the eleven gates (run before every commit)
make release    # six platform artifacts + provenance/SBOM + checksums.txt → dist/
make test-installers  # offline integration test for install.sh / install.ps1 / the GUI installers
make site       # render the website into site/dist/ (version injected from the git tag)
make npm-dry    # build the npm packages and validate manifests (no publish, no credentials)
```

## Documentation

| Document | Contents |
|---|---|
| [docs/en/cli.md](docs/en/cli.md) | **CLI reference** — all ten commands with examples |
| [docs/en/troubleshooting.md](docs/en/troubleshooting.md) | Troubleshooting, including coexistence with proto/nvm/mise |
| [docs/json-schema.md](docs/json-schema.md) | The `--json` v1 contract (fields and issue codes) |
| [docs/cli.md](docs/cli.md) | The Chinese manual (more narrative detail) |
| [docs/progress.md](docs/progress.md) | Milestone progress, per-round deliveries and pitfalls |

## Uninstalling everything

Two layers: **what pk installed** is removed by pk, **pk itself** by the channel that installed it.

```bash
pk uninstall --all --yes            # every runtime + shims + rc/Path blocks (cache kept for offline reinstall)
pk uninstall --all --yes --purge    # also delete ~/.packnest (tools, cache, ledger, config)
pk uninstall --all --yes --purge --keep-cache   # keep cache/ only
```

`--all` does four things: removes every tool directory from the ledger, deletes the shims in
`~/.packnest/bin`, strips the `# >>> packnest >>>` block from your shell rc (on Windows also the four
directories pk added to the **user Path registry value**), and finally prints the exact command to
remove the binary itself based on how it was installed (brew / npm / scoop / script).
Without `--yes` it only prints the plan.

| Channel | Command |
|---|---|
| Homebrew | `brew uninstall packnest && brew untap nekoleamo/packnest` |
| npm | `npm uninstall -g packnest` |
| Scoop | `scoop uninstall packnest && scoop bucket rm nekoleamo` |
| curl script (macOS/Linux) | `rm -f ~/.local/bin/pk ~/.local/bin/packnest` (default dir; use yours if you passed `--dir`) |
| iwr script (Windows) | `Remove-Item -Recurse -Force "$env:LOCALAPPDATA\packnest\bin"` |
| self-hosted / manual | delete that file |

The two script channels record where pk itself was installed (`state/install.json`), so one command
can remove everything including pk:

```bash
pk uninstall --all --yes --purge --self     # runtimes + cache + config + the pk binary itself
```

`--self` unlinks the running binary on macOS/Linux and removes the directory if it ends up empty;
on Windows a running exe cannot be deleted, so it prints the exact `Remove-Item` command instead.

pk only writes `~/.packnest` plus its own managed blocks, so nothing else is left behind —
`~/.npm`, `~/.npmrc` and other managers' directories are never touched.

## Known limitations

- The Python index only declares npmmirror (upstream GitHub has no directory listing); the Go index only declares the official `?mode=json&include=all` and falls back to a 24h stale cache
- pnpm ships as per-platform packages (`@pnpm/exe.<os>-<arch>`, only from 11.0.0-rc.3); `pk install pnpm@10` fails fast with a corepack / `npm i -g pnpm@10` suggestion
- Cache is content-addressed (`<sha256>.<ext>`) and deduped across versions; `alias/<algo>-<hex>` maps published digests to those files so a warm `PK_CACHE` is reusable on a fresh machine (no TOFU/ledger needed)
- Per-project override of the global version requires direnv (`pk env --project`); without direnv `node` still resolves to the global default
- The GUI is an optional supplement (Wails v3 + Vue3, current 0.11.2): it requires the CLI and drives it via `pk --json`; it is not a second state manager

## License

MIT, see [LICENSE](LICENSE).
