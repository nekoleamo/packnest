#!/bin/sh
# 安装脚本集成测试（完善路线 §E8b）：scripts/install.sh 与 scripts/install.ps1 的
# 「正常 / 产物被改 / 缺 .sig / --skip-verify / 清单签名被改」五条路径。
#
# 做法：**完全离线** —— 临时目录造 v<VERSION> 静态源（假产物 + checksums.txt + sshsig），
# 用 python 起本地 http.server 当 CDN，脚本副本里把内置公钥换成临时生成的公钥
# （仓库里的脚本一个字节都不改）。所以本机、CI、断网环境都能跑。
#
# 用法：
#   sh scripts/test-installers.sh            # 按当前平台自动选（macOS/Linux 跑 sh 四条，Windows 跑 ps1 五条）
#   PK_FORCE_SH=1 / PK_FORCE_PS1=1           # 强制只跑某一边（默认按 uname 判断）
#
# 依赖：sh、tar、curl、ssh-keygen（OpenSSH 8.2+ 支持 -Y sign）、python3/python（起 http.server 并造 zip）。
# 缺 ssh-keygen 时**跳过整轮并说明**，不是假绿：没有它测不了签名这一半。
#
# 注意：install.ps1 成功后会把自己追加进**用户 PATH**（脚本设计如此）。CI 是短命机无所谓，
# 本地在 Windows 上跑请自己清掉那条 `%TEMP%\pk-it-*` 路径。
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
VERSION="1.2.3"
SIGNER="release@packnest.dev"
NS="pk-release"

say() { printf '%s\n' "$*"; }
ok() { PASS=$((PASS + 1)); say "  ✔ $*"; }
bad() { FAIL=$((FAIL + 1)); say "  ✘ $*"; }

PASS=0
FAIL=0

# 前置检查：缺什么就直接说清楚，别跑出"看起来通过"的假结果。
# python 在 Windows 的 Git Bash 里通常只有 python.exe（没有 python3）：两条路径都认
PY=$(command -v python3 || command -v python || true)
for bin in curl tar ssh-keygen; do
  command -v "$bin" >/dev/null 2>&1 || {
    say "跳过：缺 ${bin}（签名一半测不了，不算通过）"
    exit 0
  }
done
[ -n "$PY" ] || {
  say "跳过：缺 python（要它起本地 http.server 并造 zip）"
  exit 0
}

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64) ARCH=amd64 ;;
esac

WANT_SH=0
WANT_PS1=0
case "$OS" in
  darwin|linux) WANT_SH=1 ;;
  mingw*|msys*|cygwin*) WANT_PS1=1 ;;
esac
[ "${PK_FORCE_SH:-0}" = "1" ] && WANT_SH=1
[ "${PK_FORCE_PS1:-0}" = "1" ] && WANT_PS1=1
if [ "$WANT_PS1" = "1" ] && ! command -v pwsh >/dev/null 2>&1; then
  # 强制指定却没装 pwsh：直接红（否则 CI 会"看起来跑了"，其实一条 ps1 用例都没测）
  [ "${PK_FORCE_PS1:-0}" = "1" ] && { say "✘ PK_FORCE_PS1=1 但本机没有 pwsh"; exit 1; }
  WANT_PS1=0
fi
if [ "$WANT_SH" = "0" ] && [ "$WANT_PS1" = "0" ]; then
  say "跳过：当前平台（${OS}）既跑不了 install.sh 也没有 pwsh 跑 install.ps1"
  exit 0
fi

WORK=$(mktemp -d 2>/dev/null || mktemp -d -t pk-it)
SRV="$WORK/srv"
VDIR="$SRV/v$VERSION"
mkdir -p "$VDIR"

SERVER_PID=""
stop_server() {
  if [ -n "$SERVER_PID" ]; then kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; fi
}
cleanup() { stop_server; rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

# ── 假产物：内容由代码造（不存二进制）；`packnest --version` 会被 install.sh 调用
printf '#!/bin/sh\necho "packnest %s"\n' "$VERSION" > "$WORK/packnest"
chmod +x "$WORK/packnest"
printf 'fake packnest.exe %s\n' "$VERSION" > "$WORK/packnest.exe"

tar -czf "$VDIR/packnest_${VERSION}_${OS}_${ARCH}.tar.gz" -C "$WORK" packnest

# zip 用 python 造（CI 的 windows 镜像不保证有 zip 命令，python 反正要用）
"$PY" - "$WORK" "$VDIR" "$VERSION" <<'PY'
import os, sys, zipfile
work, vdir, ver = sys.argv[1], sys.argv[2], sys.argv[3]
for arch in ("amd64", "arm64"):
    name = os.path.join(vdir, f"packnest_{ver}_windows_{arch}.zip")
    with zipfile.ZipFile(name, "w", zipfile.ZIP_STORED) as z:
        z.write(os.path.join(work, "packnest.exe"), "packnest.exe")
PY

printf '%s\n' "$VERSION" > "$SRV/latest"

# 临时发布密钥 + 对 checksums.txt 的 sshsig（namespace 与发布签名一致）
ssh-keygen -q -t ed25519 -N '' -C "pk-it-$(date +%s)" -f "$WORK/key"
make_checksums() {
  ( cd "$VDIR" && shasum -a 256 packnest_*.tar.gz packnest_*.zip 2>/dev/null ||
      sha256sum packnest_*.tar.gz packnest_*.zip ) | sort -k2 > "$VDIR/checksums.txt"
  # -Y sign 把签名写到 checksums.txt.sig（就地覆盖）
  rm -f "$VDIR/checksums.txt.sig"
  ssh-keygen -Y sign -f "$WORK/key" -n "$NS" "$VDIR/checksums.txt" >/dev/null 2>&1
}
make_checksums
ALLOWED="$SIGNER $(cut -d' ' -f1,2 "$WORK/key.pub") pk-release-it pk-release-it"

# 脚本副本：只把内置公钥换成临时公钥，其余原样
inject_key() { # $1=源脚本 $2=输出
  "$PY" - "$1" "$2" "$ALLOWED" <<'PY'
import sys
src, dst, line = sys.argv[1], sys.argv[2], sys.argv[3]
out = []
for ln in open(src, encoding="utf-8"):
    if ln.startswith("ALLOWED_SIGNERS="):
        ln = 'ALLOWED_SIGNERS="%s"\n' % line
    elif ln.startswith("$AllowedSigners = "):
        ln = "$AllowedSigners = '%s'\n" % line
    out.append(ln)
open(dst, "w", encoding="utf-8").write("".join(out))
PY
}
inject_key "$ROOT/scripts/install.sh" "$WORK/install-it.sh"
inject_key "$ROOT/scripts/install.ps1" "$WORK/install-it.ps1"

# 起本地静态源
PORT=$("$PY" -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')
"$PY" -m http.server "$PORT" --bind 127.0.0.1 --directory "$SRV" >"$WORK/server.log" 2>&1 &
SERVER_PID=$!
BASE="http://127.0.0.1:$PORT"
i=0
while [ $i -lt 50 ]; do
  curl -fsS --max-time 2 "$BASE/latest" >/dev/null 2>&1 && break
  i=$((i + 1))
  sleep 0.1
done
curl -fsS --max-time 2 "$BASE/latest" >/dev/null 2>&1 || {
  say "✘ 本地静态源起不来（${BASE}）"; cat "$WORK/server.log" >&2; exit 1
}

# 现场复位 + 两种篡改
restore() { make_checksums; }
tamper_artifact() { # 产物被改（清单与签名仍是真的）→ 只能被 sha256 抓到
  printf 'x' >> "$VDIR/packnest_${VERSION}_${OS}_${ARCH}.tar.gz"
  printf 'x' >> "$VDIR/packnest_${VERSION}_windows_${ARCH}.zip"
}
tamper_manifest() { # 清单被改（签名不再匹配）→ 只能被 sshsig 抓到
  printf 'deadbeef  packnest_0.0.0_linux_amd64.tar.gz\n' >> "$VDIR/checksums.txt"
}
tamper_sig() { # 改签名体里的 base64（末尾追加垃圾 ssh-keygen 会忽略，测不出问题）
  "$PY" - "$VDIR/checksums.txt.sig" <<'PY2'
import sys
p = sys.argv[1]
lines = open(p).read().split("\n")
i = next(i for i, l in enumerate(lines) if not l.startswith("-----") and l.strip())
ch = lines[i][0]
lines[i] = ("A" if ch != "A" else "B") + lines[i][1:]
open(p, "w").write("\n".join(lines))
PY2
}
drop_sig() { rm -f "$VDIR/checksums.txt.sig"; }

# 断言助手
expect_exit() { # $1=描述 $2=期望 $3=实际
  if [ "$2" = "$3" ]; then ok "$1（exit $3）"; else bad "$1：期望 exit $2，实际 $3"; fi
}
expect_in() { # $1=描述 $2=日志 $3=子串 [$4=不该出现的子串]
  if grep -qF -- "$3" "$2"; then ok "$1"; else bad "$1：日志里没有「$3」"; fi
  if [ $# -ge 4 ] && [ -n "$4" ]; then
    if grep -qF -- "$4" "$2"; then bad "$1：不该出现「$4」"; else ok "$1：没有「$4」"; fi
  fi
}

# ─────────────────────────── install.sh（sh）
run_sh() { # $1=名字 [$2...=额外参数]；输出写 $WORK/sh.$1.log，退出码回显
  name=$1
  shift
  set +e
  PK_ROOT="$WORK/root-sh-$name" sh "$WORK/install-it.sh" --base "$BASE" --dir "$WORK/bin-sh-$name" "$@" \
    > "$WORK/sh.$name.log" 2>&1
  code=$?
  set -e
  printf '%s' "$code"
}

if [ "$WANT_SH" = "1" ]; then
  say ""
  say "== install.sh（${OS}-${ARCH}，本地源 ${BASE}）"

  restore
  c=$(run_sh normal)
  expect_exit "正常安装" 0 "$c"
  expect_in "正常安装：验签通过" "$WORK/sh.normal.log" "sshsig OK"
  expect_in "正常安装：哈希通过" "$WORK/sh.normal.log" "sha256 OK"
  if [ -x "$WORK/bin-sh-normal/pk" ]; then ok "正常安装：产物落到 --dir"; else bad "正常安装：--dir 里没有 pk"; fi

  restore
  tamper_artifact
  c=$(run_sh tampered)
  expect_exit "产物被改" 4 "$c"
  # 清单签名仍然有效（改的是产物），所以这一条的日志里**会有** sshsig OK，只断言哈希失败
  expect_in "产物被改：报 sha256 且给建议" "$WORK/sh.tampered.log" "sha256 校验失败"

  restore
  tamper_manifest
  c=$(run_sh manifest)
  expect_exit "清单被改" 4 "$c"
  expect_in "清单被改：报签名失败" "$WORK/sh.manifest.log" "签名校验失败"

  restore
  tamper_sig
  c=$(run_sh sig)
  expect_exit "签名被改一字节" 4 "$c"
  expect_in "签名被改：报签名失败" "$WORK/sh.sig.log" "签名校验失败"

  restore
  drop_sig
  c=$(run_sh nosig)
  expect_exit "缺 .sig" 0 "$c"
  expect_in "缺 .sig：提示后继续" "$WORK/sh.nosig.log" "没有发布签名"
  expect_in "缺 .sig：退回 sha256" "$WORK/sh.nosig.log" "sha256 OK"

  restore
  tamper_artifact
  c=$(run_sh skip --skip-verify)
  expect_exit "--skip-verify 放行被改产物" 0 "$c"
  expect_in "--skip-verify：不做校验" "$WORK/sh.skip.log" "装好了" "sha256 校验失败"
fi

# ─────────────────────────── install.ps1（pwsh）
W() { # Git Bash 路径 → Windows 路径（pwsh 不认 /tmp/...）
  if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s' "$1"; fi
}
run_ps() { # $1=名字 [$2...=额外参数]
  name=$1
  shift
  set +e
  PK_ROOT="$(W "$WORK/root-ps-$name")" pwsh -NoProfile -NonInteractive -File "$(W "$WORK/install-it.ps1")" \
    -Base "$BASE" -Dir "$(W "$WORK/bin-ps-$name")" "$@" > "$WORK/ps.$name.log" 2>&1
  code=$?
  set -e
  printf '%s' "$code"
}

if [ "$WANT_PS1" = "1" ]; then
  say ""
  say "== install.ps1（${OS}-${ARCH}，本地源 ${BASE}，pwsh $(pwsh -NoProfile -Command '$PSVersionTable.PSVersion.ToString()'))"

  # ssh-keygen 在 Windows 上是可选功能：没有时 ps1 只告诉用户没验签，此时不能断言 sshsig OK
  HAVE_SSHKEYGEN=0
  command -v ssh-keygen >/dev/null 2>&1 && HAVE_SSHKEYGEN=1

  # 退出码只在 Windows 上断言：install.ps1 成功后会写「用户 PATH」，
  # 这一步在非 Windows 的 pwsh 上不支持（PlatformNotSupported），会让 exit 变成非 0。
  # 所以正向用例一律按**日志证据 + 产物落盘**判定，退出码只对负向用例（必须失败）断言。
  ON_WINDOWS=0
  case "$OS" in mingw*|msys*|cygwin*) ON_WINDOWS=1 ;; esac

  restore
  c=$(run_ps normal)
  expect_in "正常安装：哈希通过" "$WORK/ps.normal.log" "verify: sha256 OK"
  if [ "$HAVE_SSHKEYGEN" = "1" ]; then
    expect_in "正常安装：验签通过" "$WORK/ps.normal.log" "sshsig OK"
  else
    say "  · 跳过验签断言：本机没有 ssh-keygen（Windows 可选功能）"
  fi
  if [ -f "$WORK/bin-ps-normal/pk.exe" ]; then ok "正常安装：产物落到 -Dir"; else bad "正常安装：-Dir 里没有 pk.exe"; fi
  if [ "$ON_WINDOWS" = "1" ]; then expect_exit "正常安装" 0 "$c"; fi

  restore
  tamper_artifact
  c=$(run_ps tampered)
  if [ "$c" = "0" ]; then bad "产物被改：应该失败，实际 exit 0"; else ok "产物被改（exit ${c}）"; fi
  expect_in "产物被改：报 mismatch" "$WORK/ps.tampered.log" "SHA256 mismatch"

  restore
  tamper_sig
  c=$(run_ps sig)
  if [ "$HAVE_SSHKEYGEN" = "1" ]; then
    if [ "$c" = "0" ]; then bad "签名被改一字节：应该失败，实际 exit 0"; else ok "签名被改一字节（exit ${c}）"; fi
    expect_in "签名被改：报签名失败" "$WORK/ps.sig.log" "Signature verification failed"
  else
    say "  · 跳过签名篡改断言：本机没有 ssh-keygen"
  fi

  restore
  drop_sig
  c=$(run_ps nosig)
  expect_in "缺 .sig：退回 sha256 且不报错" "$WORK/ps.nosig.log" "verify: sha256 OK"
  if grep -qF "Signature verification failed" "$WORK/ps.nosig.log"; then bad "缺 .sig：不该报签名失败"; else ok "缺 .sig：不报签名失败"; fi
  if [ "$ON_WINDOWS" = "1" ]; then expect_exit "缺 .sig" 0 "$c"; fi

  restore
  tamper_artifact
  c=$(run_ps skip -SkipVerify)
  expect_in "-SkipVerify：照装不校验" "$WORK/ps.skip.log" "Installed:" "SHA256 mismatch"
  if [ "$ON_WINDOWS" = "1" ]; then expect_exit "-SkipVerify 放行被改产物" 0 "$c"; fi
fi

say ""
if [ "$FAIL" = "0" ]; then
  say "安装脚本集成测试通过：${PASS} 条断言（本地离线，源 ${BASE}）"
  exit 0
fi
say "✘ 安装脚本集成测试失败：${FAIL} 条不通过 / 共 $((PASS + FAIL)) 条"
say ""
say "── 现场（失败的几次运行日志尾）"
for f in "$WORK"/*.log; do
  case "$f" in *server.log) continue ;; esac
  say "── $(basename "$f")"
  tail -n 8 "$f"
done
exit 1
