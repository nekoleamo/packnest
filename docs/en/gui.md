# Desktop app `pk-desktop` (GUI)

> **One line**: a visual supplement to the CLI. **The CLI must be installed first** — every action in the UI is a
> subprocess call to `pk <command> --json`. It ships no downloader, ledger or self-updater of its own; uninstall and
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
| Windows (amd64) | `packnest-desktop_<version>_windows_amd64.zip` | unzip and run `pk-desktop.exe`; needs the built-in WebView2 runtime (present on Win10/11) |
| Linux (amd64) | `packnest-desktop_<version>_linux_amd64.tar.gz` | unzip and run `pk-desktop`; needs GTK4 + WebKitGTK 6.0 (`sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev`) |

macOS ships arm64 only (Apple Silicon); Intel Macs must build it themselves with `make gui-app`.

## 2. Install the CLI first

On start the app looks for `pk` on `PATH`, then in the usual install locations, and checks its contract version
(`schema ≥ 1` and a recent enough version). If it cannot find one, the header shows the install command for **your
platform** (PowerShell on Windows, `sh` elsewhere) with a copy button, and the main panels stay hidden. The app never
downloads a second runtime itself.

```bash
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh
# or: brew install nekoleamo/packnest/packnest
```

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
| Linux reports missing WebKit | install `libgtk-4-dev libwebkitgtk-6.0-dev` (package names vary per distro) |
| UI shows an old version | the desktop shell embeds its frontend: reinstall the GUI to upgrade it; the CLI version is shown at the top |

## 6. Uninstall

The GUI is a single executable — delete it. Everything it installed lives in `~/.packnest` and is managed by the CLI:

```bash
pk uninstall --all --yes            # remove runtimes, keep pk
pk uninstall --all --yes --purge    # also remove ~/.packnest
```
