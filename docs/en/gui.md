# Desktop app `pk-desktop` (GUI)

> **One line**: a visual supplement to the CLI. **The CLI must be installed first** — every action in the UI is a
> subprocess call to `pk <command> --json`. It ships no ledger of its own and no second implementation of upgrading; uninstall and
> upgrade still go through the CLI.

- Contract: `pk --json` ([../json-schema.md](../json-schema.md))
- Build / package / release details: [../../cmd/pk-desktop/README.md](../../cmd/pk-desktop/README.md)

## 1. Download and install

Artifacts live under a **separate tag** `gui-v<version>` (the GUI version is independent of the CLI version: the CLI may
be `v1.3.1` while the GUI is `0.10.3`). Both hosts publish the same three files:

```
https://gitee.com/null_593_5354/packnest/releases/tag/gui-v<version>
https://github.com/nekoleamo/packnest/releases/tag/gui-v<version>
```

| Platform | File | How to run |
|---|---|---|
| macOS (arm64) | `packnest-desktop_<version>_darwin_arm64.app.zip` | unzip → `packnest.app`; on first launch allow it once in System Settings → Privacy & Security (ad-hoc signed, not notarized) |
| Windows (amd64 / **arm64**) | `packnest-desktop_<version>_windows_{amd64,arm64}.zip` | unzip and run `pk-desktop.exe`; needs the built-in WebView2 runtime (present on Win10/11). Use the `arm64` one on ARM machines (available since 0.11.2) |
| Linux (amd64) | `packnest-desktop_<version>_linux_amd64.tar.gz` | unzip and run `pk-desktop`; needs GTK4 + WebKitGTK 6.0 (`sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev`) |

macOS ships arm64 only (Apple Silicon); Intel Macs must build it themselves with `make gui-app`.
The one-time Gatekeeper approval **has been verified** (2026-11-15, GUI 0.11.2 + CLI 1.8.1): blocked → "Open Anyway" in Privacy & Security → the window starts and the CLI is picked up. See `docs/verify-manual.md` §3.

## 2. Install the CLI first

On start the app looks for `pk` on `PATH`, then in the usual install locations, and checks its contract version
(`schema ≥ 1` and a recent enough version). If it cannot find one, the header shows the install command for **your
platform** (PowerShell on Windows, `sh` elsewhere) with a copy button, and the main panels stay hidden. The app never
downloads a second runtime itself.

```bash
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh
# or: brew install nekoleamo/packnest/packnest
```

## 2.5 Check for updates (since 1.9)

The **Updates** card looks at both versions (they are decoupled):

- **source auto-picked**: it runs `pk upgrade --self --check --json`, and the CLI returns the sources it probed with
  latencies already sorted — the GUI keeps that order, so China usually lands on gitee and overseas on GitHub;
- **upgrade the CLI**: delegated to `pk upgrade --self` (channel rules, signature check and atomic replace live in one place);
- **upgrade the desktop app**: the GUI downloads its own artifact, verifies `checksums.txt.sig` with `ssh-keygen`,
  compares sha256, swaps the bundle/binary (whole `packnest.app` on macOS) and asks you to reopen;
- **channel-aware**: a scoop-installed GUI only gets `scoop update packnest-desktop`;
- without `ssh-keygen` (Windows needs the OpenSSH client feature) it **refuses** to self-update and points at the
  install script — no "sha256 only" downgrade.

## 3. Browser form (no desktop runtime needed)

```bash
pk-desktop --serve                                            # 127.0.0.1:7799
pk-desktop --serve --addr 0.0.0.0:7799 --token <random>        # non-loopback address requires a token
```

`http://127.0.0.1:7799/` serves the same UI (a plain static page plus `/api/*`). When served remotely, `/api/*` calls
need `?token=` or `Authorization: Bearer <token>`.

## 3.5 UI language and launch at login

- **Language**: the header has a `中文 / EN` switch; the default follows `navigator.language` and the choice is stored
  locally. The UI strings are the GUI's own; **CLI output (doctor findings, install logs) stays Chinese** — on purpose:
  `--json` is the machine interface, human prose is maintained once to avoid drift.
- **Launch at login**: a switch in Settings, **off by default**. Enabling writes a **user-level** entry only
  (macOS LaunchAgent, Windows `HKCU\...\Run`, Linux `~/.config/autostart`); disabling removes just that entry.

## 4. What calls what

| UI | CLI call | Notes |
|---|---|---|
| installed tools table | `pk list --json` | version, size, `weak`/unverified/npm badges, `source` (embedded/external manifest) |
| upgrade hint | `outdated[]` from `pk list --json` | **read from the 24h index cache only, never networked**; "Upgrade" = `pk install <tool>@latest` |
| set default / uninstall | `pk use` / `pk uninstall --yes` | the UI confirms first; exit code 5 (another pk running) is surfaced as "try again later" |
| available versions | `pk list --available --json`, `pk list <tool> --json [--all]` | full list plus local filtering |
| install queue | `pk install <spec> --json` | NDJSON events; per-row progress uses `progress.bytes_done/bytes_total` |
| mirror profiles | `pk mirror list/use/probe --json` | probe results cached 6h; the "write ~/.npmrc" button is `pk mirror write` (backs up, append-only) |
| doctor | `pk doctor --json [--globals] [--probe]` | `--probe` really loads `.node` files via node's `process.dlopen` (binary only, no package code) |
| fix now | `pk doctor --json --fix` | repairs only the PATH managed block and the pk/packnest entries; lists `fixed[]` |
| reclaim | `pk gc --json [--keep N]` | GET = preview, POST = delete; `--keep N` keeps the newest N versions per tool |
| `which` / `env` | `pk which --json`, `pk env --json [--project]` | read-only |

## 5. Troubleshooting

| Symptom | Cause / fix |
|---|---|
| "cannot find pk" | CLI not installed, or `pk` not on `PATH`: run `pk env --write` and reopen; installing both into the same bin dir also works |
| "another pk is running" | CLI exit code 5 (`state/lock` held): wait for the running install/uninstall |
| 404 / 401 | `--serve --addr` on a non-loopback address without `--token`: add one, or pass the token in the browser |
| macOS refuses to open the app | ad-hoc signature: System Settings → Privacy & Security → "Open Anyway" once |
| Linux tray icon missing | install `libayatana-appindicator3`; without a tray the window closes to exit (no crash) |
| UI shows an old version | the desktop shell embeds its frontend: reinstall the GUI to upgrade it; the CLI version is shown at the top |

## 5.5 Stability promise (GUI 1.0 contract)

Since 1.0.0 the four things below are a **release contract**: `1.0.x` only fixes bugs and never changes them; changing one means a major bump (or `schema:2` with a compatible read path).

| # | Contract | Value |
|---|---|---|
| C1 | Preferences file | `gui.json` (`~/Library/Application Support/packnest/` on macOS, `~/.config/packnest/` on Linux, `%AppData%\packnest\` on Windows) holding `schema`/`theme`/`lang`/`page`; **fields are only added, never renamed** |
| C2 | Single instance and browser form | occupies `--addr` (default `127.0.0.1:7799`); a second instance reports "a packnest window is already running **or the port is taken**" and suggests `--addr`; the same address is the `--serve` browser form, whose `/api/*` accepts `?token=` or `Authorization: Bearer` **and since 0.14.0 rejects cross-origin requests** (`Origin` must match the local host, `Sec-Fetch-Site: cross-site` gets 403; curl and scripts are unaffected) |
| C3 | Self-update path | version from `scripts/latest-gui` → download the artifact → `ssh-keygen -Y verify` against `checksums.txt.sig` → compare sha256 → replace the whole `packnest.app` on macOS (or the executable when it is not in a `.app`), the executable on Windows/Linux → tell the user "tray → Quit → reopen"; quitting mid-replace waits for the swap to finish; a scoop/brew install only prints the channel command; **only the five published platforms are offered an update** (a self-built Intel-mac binary is not, so nobody clicks into a 404) |
| C4 | Install locations | preferences only under the app config dir; launch-at-login only writes the user-level entry (`~/Library/LaunchAgents/packnest-desktop.plist`, `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, `~/.config/autostart/packnest-desktop.desktop`) and removes only its own entry |

`gui.json` version policy:

- an **old file** (no `schema` field, written before 1.0) is read as before and gets `"schema": 1` on the next save;
- a **higher `schema`** (you downgraded after running a newer GUI) → run on defaults, **do not write back this session**, leave the file untouched and say so in the log — a downgrade must never destroy a newer format.

The CLI-side contract the GUI relies on is `pk --json` with `schema ≥ 1` (see [json-schema.md](../json-schema.md)); both sides only add fields.

## 6. Uninstall

The GUI is a single executable — delete it. Everything it installed lives in `~/.packnest` and is managed by the CLI:

```bash
pk uninstall --all --yes            # remove runtimes, keep pk
pk uninstall --all --yes --purge    # also remove ~/.packnest
```
