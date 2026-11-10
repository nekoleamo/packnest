# Troubleshooting (including coexistence with proto / nvm / mise)

> **English** · [中文](../troubleshooting.md)

Start with `pk doctor` (`--json` for scripts, `--globals` for global packages, `--probe` to send
HEAD requests at the mirrors). The complete issue-code table lives in
[json-schema.md](../json-schema.md).

### Windows: `iwr … | iex` fails with "the remote host forcibly closed the connection"

The ~3 MB zip download gets reset. The usual culprits are Windows PowerShell 5.1's old TLS default,
or a system proxy that dislikes long-lived connections to domestic hosts. Since 0.7.7 the installer
handles both:

- forces `SecurityProtocol = Tls12`
- each round tries **WebClient with the system proxy bypassed**, then WebClient with the proxy, then
  IWR; three rounds with a 1s/2s backoff
- if it still fails it **falls back to the npm platform package on npmmirror** (a different host,
  usually faster in China, verified by sha1)
- `.tgz` extraction uses `tar.exe` (bundled since Windows 10 1803) and falls back to `tar`

Manual fallbacks: `-Mirror cn`, or host the zip + checksums.txt locally and use `-Base <url>`, or simply
unzip the release asset into `%LOCALAPPDATA%\packnest\bin` as `packnest.exe` + `pk.exe`.

## 1. Installs keep failing

| Symptom | Diagnosis | Fix |
|---|---|---|
| `网络失败: 索引全部不可用` | wrong profile, or the proxy is not in effect | `pk mirror probe --tool node` to see which route answers; `pk mirror use cn`; make sure `HTTPS_PROXY` is exported |
| `校验失败: 拿不到…校验和` | every checksum source is unreachable (first install, air-gapped network) | point at an internal source with `PK_MIRROR_<TOOL>_CHECKSUM=<url>`, else confirm the artifact's origin and use `--insecure` (marked `unverified`) |
| `校验失败: 校验和源之间不一致` | the mirror is poisoned **or upstream re-published the artifact** (observed with deno v2.9.8) | the error states which source the artifact matches; confirm, then use `--insecure` — never add it blindly |
| `校验失败: sha256 与本地记录不一致` | the TOFU record differs from what was downloaded (upstream re-published, or the table went stale) | verify it is not a mirror problem, then `--force-rehash` re-evaluates and overwrites just that entry — no need to delete the whole table |
| `解压失败（产物可能被截断）` | the cached artifact is truncated | the cache entry is deleted automatically, retry; if it repeats, `pk gc --cache-max 1` empties the cache |
| `--frozen` reports a mismatch | `pk.toml` drifted from `pk.lock` | run `pk install` locally, commit the refreshed lock; CI never resolves versions |

### Windows: `pk env --write` done, new PowerShell still cannot find `node`

Two bugs, both fixed in 0.7.3 (workaround for older builds below):

1. **Wrong file**: PowerShell has two unrelated profiles — `Documents\WindowsPowerShell\…` (the
   built-in 5.1) and `Documents\PowerShell\…` (pwsh 7). Older builds only wrote the 7 one, so 5.1
   users saw nothing. Now the version is detected from the environment and, when it cannot be
   determined, **both are written**; a OneDrive-redirected `Documents` is handled too.
2. **Wrong syntax**: Windows sets no `$SHELL`, so the old build decided "bash" and wrote a POSIX
   block into `~/.bashrc`, which nothing on Windows reads. Windows now defaults to PowerShell
   (Git Bash still reports `$SHELL`, so it keeps getting bash syntax).

### Windows: profile exists, but every shell errors "running scripts is disabled"

`Restricted` / `AllSigned` refuses to load an **unsigned** profile script (ours is unsigned), so
each new window shows `… cannot be loaded because running scripts is disabled on this system`.

Since 0.7.4:

- `pk env --write` checks the policy first; when scripts are blocked it **does not write the
  profile** (a file that can never load) and removes any old block;
- PATH goes into the **user Path registry value** (`HKCU\Environment`) instead — unaffected by
  ExecutionPolicy, honoured by cmd / PowerShell / Git Bash, so `node` works immediately;
- the previous Path is backed up to `<PK_ROOT>\state\user-path.bak.txt`; `pk env --unset` reverts.

Want `npm_config_registry` / `GOPROXY` persisted too (those can only live in a profile)? Either:

```powershell
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned   # no admin; local scripts only
pk env --shell powershell | Invoke-Expression         # or per session
```

Self-check:

```powershell
pk env --shell powershell   # inspect the snippet first (PATH must contain ~\.packnest\bin)
pk env --write              # then write; the output lists every profile it touched
$PROFILE                    # which profile actually loads
```

## 2. Command not found / wrong version

```bash
pk which node          # where the shim actually points
pk list                # * marks the current global default
pk use node@22.22.2    # switch (only between installed versions)
pk env --shell bash    # inspect the PATH segments and registry variables
```

- `PATH_ORDER`: `~/.packnest/bin` must come **first** on PATH, otherwise the system Node or another
  manager wins.
- `SHIM_MISSING`: any `pk install` / `pk use` recreates both `pk` and `packnest` entries.
- Installed but `which` finds nothing: make sure `eval "$(pk env --shell bash)"` is active
  (`pk env --write` puts it into the managed rc block).

## 3. The three global-package warnings

| Code | Meaning | Fix |
|---|---|---|
| `GLOBAL_ENGINES_UNMET` | the package's `engines.node` does not accept the current Node | switch Node, or upgrade/downgrade that package |
| `GLOBAL_NATIVE_ABI` | native modules without N-API prebuilds → broken when switching Node | switch back, or `npm rebuild <pkg>` (offline, served from cache) |
| `GLOBAL_SHADOWS_PROJECT` | a global command shadows the project's `node_modules/.bin` | `npm run` is unaffected; in an interactive shell use `./node_modules/.bin/<cmd>` |

Global packages live under `~/.packnest/global` (`npm_config_prefix`), and `npm_config_globalconfig`
points there too, so **switching Node neither loses global packages nor `npm config set -g`**. The
package cache stays at `~/.npm` (never migrated, never copied).

## 4. Coexisting with proto / nvm / mise

`pk` only writes `~/.packnest` and **never touches other managers' directories**. All of them inject
shims into PATH, so order decides who wins:

```bash
echo $PATH | tr ':' '\n' | head -5      # who is first?
pk doctor | grep FOREIGN_MANAGER        # lists the other managers found
```

Three options (`pk doctor` suggests the same):

1. **Let pk own everything**: remove the other manager's init line from your shell rc
   (proto's `~/.proto/env`, nvm's `nvm.sh`).
2. **Let pk own Node only**: put `~/.packnest/bin` ahead of the other manager and keep the rest.
3. **Split duties explicitly**: e.g. Node via pk, Rust/Go via proto — note that two managers both
   claiming Go will fight over `GOROOT`/PATH.

`pk doctor`'s proto hints are read-only: nothing under `~/.proto` is deleted or modified.
Migrating global packages: check `pk doctor --globals` for engines/ABI first, then
`npm i -g` them again in the new prefix (cache hits, no network).

## 5. After a crash or kill -9

- Crash residue (`tools/*/*.tmp.*`, `*.old`, `cache/archives/*.part`) is swept **before the next
  write** — only entries older than 24h, so another running process is never robbed.
- Corrupt ledger: `pk list` restores from `installs.json.bak` automatically (`LEDGER_RECOVERED`).
  Only when both are broken do you get `LEDGER_BROKEN`; then delete `state/installs.json*` and
  reinstall. Directories under `tools/` are **not** deleted automatically — clean them by hand
  after checking.
- `another pk is running` (exit code 5): another process holds the lock. Wait for it; once you are
  sure no stale process exists you may delete `state/lock`.

### Windows: `pk.exe` is still there after `pk uninstall --all --self`

Windows refuses to delete a running executable, so `--self` schedules the delete instead of doing it
inline: a hidden detached `cmd.exe` waits ~3 seconds (pk has exited by then, the file handle is
released), deletes `pk.exe` / `packnest.exe` in the install directory, and removes the directory if it
ends up empty. It deletes **only those two files — never `rd /s` the whole directory**, because you may
have installed with `-Dir` into a shared folder.

**Why 0.7.8 still left it behind**: the old build handed the whole
`ping … & del "path" & rd "path"` string to `cmd.exe /c`. Go quotes arguments that contain spaces
and escapes embedded quotes as `\"…\"`, which cmd.exe does not understand — the command line was
mangled and cmd.exe did nothing (with the error swallowed by `>nul`). Since 0.7.9 the commands are
written to `%TEMP%\packnest-self-remove-*.cmd` and only the **file path** is passed to cmd.exe.
Check `%TEMP%\packnest-self-remove.log` for a REMOVED/FAILED line.

If it is still there after a few seconds (rare — something still holds the file):

```powershell
Get-Process pk, packnest -ErrorAction SilentlyContinue
Remove-Item -Force "$env:LOCALAPPDATA\packnest\bin\pk.exe","$env:LOCALAPPDATA\packnest\bin\packnest.exe"
Remove-Item "$env:LOCALAPPDATA\packnest\bin" -ErrorAction SilentlyContinue
```

Note the installer script adds the **install directory** to the user Path, which is not the same thing as
the four `~/.packnest` segments written by `pk env --write`. Since 0.7.7 `uninstall --all` removes both
sets from the user Path registry value, so no dangling entry is left behind.

## 6. Full rollback

```bash
rm -rf ~/.packnest      # back to "never installed" — the strongest form of lossless
```

Uninstalling a single tool is `pk uninstall node@22.22.2` (add `--force` when referenced); the cache
is kept, so an offline reinstall still works.
