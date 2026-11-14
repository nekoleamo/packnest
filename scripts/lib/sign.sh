#!/bin/sh
# 发布签名的唯一实现（sshsig，namespace pk-release）：给某个目录的 checksums.txt 打 .sig 并自验。
#
# 用法两种：
#   . scripts/lib/sign.sh                             # 当库用：调 sign_checksums <dir>
#   sh scripts/lib/sign.sh --dir dist/v1.7.0          # 当命令用
#
# 为什么签名要抽出来：CLI 与 GUI 是两组产物、两个 release tag，但**只能有一个发布者、
# 一把密钥**。签名逻辑写两遍就会漂（一边忘了自验，一边漏了 umask 077）。
#
# 私钥只放在离线目录（PK_SIGNING_KEY 指过去）；签名不在 `make release` 里自动做 ——
# 私钥一旦常驻发布机，"发布机被接管"就能签出恶意产物，签名等于零增益
# （见 ~/Documents/Plan/17-包管理器/pn-1.7-产物签名-方案.md §二）。
set -eu

# sign_checksums <目录>：对该目录的 checksums.txt 生成 checksums.txt.sig（覆盖）。
# 成功时打印公钥（要同时写进三处：pk 内置 / install.sh / install.ps1）。
sign_checksums() {
  _dir="$1"
  _sums="$_dir/checksums.txt"
  _key="${PK_SIGNING_KEY:-}"

  [ -n "$_key" ] || {
    echo "需要 PK_SIGNING_KEY 指向离线私钥（如 /Volumes/usb/pk-release-2026）" >&2
    return 2
  }
  [ -f "$_key" ] || {
    echo "私钥不存在: $_key" >&2
    return 2
  }
  [ -f "$_sums" ] || {
    echo "找不到 $_sums（先构建产物并生成 checksums.txt）" >&2
    return 1
  }
  # 先确认私钥能读：设了口令的私钥在非交互环境会卡在提示上，提前给出明确说明。
  if ! ssh-keygen -y -f "$_key" >/dev/null 2>&1; then
    echo "读不出私钥 ${_key}：多半是设了口令（非交互环境没法输入）。" >&2
    echo "用无口令的离线密钥，或在交互终端里手动跑 ssh-keygen -Y sign。" >&2
    return 2
  fi

  umask 077
  rm -f "$_sums.sig"
  ssh-keygen -Y sign -f "$_key" -n pk-release "$_sums" >/dev/null 2>&1
  [ -f "$_sums.sig" ] || {
    echo "签名没生成（ssh-keygen 失败？）" >&2
    return 1
  }

  # 自验：从私钥推导公钥，确认签名可验（防止用错密钥发出）
  _tmp=$(mktemp -d)
  ssh-keygen -y -f "$_key" > "$_tmp/pub" 2>/dev/null || {
    echo "读不出公钥（私钥损坏？）" >&2
    rm -rf "$_tmp"
    return 1
  }
  printf 'release@packnest.dev %s\n' "$(cat "$_tmp/pub")" > "$_tmp/allowed_signers"
  if ! ssh-keygen -Y verify -f "$_tmp/allowed_signers" -I release@packnest.dev -n pk-release \
       -s "$_sums.sig" < "$_sums" >/dev/null; then
    rm -rf "$_tmp"
    echo "自验失败：签名与私钥不匹配" >&2
    return 1
  fi
  _pub=$(cat "$_tmp/pub")
  rm -rf "$_tmp"

  echo "已签名：$_sums → $_sums.sig"
  echo "这把公钥要同时写进三处（否则 install.sh / install.ps1 / pk doctor 认不出）："
  echo "  internal/verify/keys/allowed_signers"
  echo "  scripts/install.sh 的 ALLOWED_SIGNERS"
  echo "  scripts/install.ps1 的 \$AllowedSigners"
  echo "$_pub"
}

# 当命令用：sh scripts/lib/sign.sh --dir <目录>
if [ "${0##*/}" = "sign.sh" ]; then
  DIR=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --dir) DIR="${2%/}"; shift 2 ;;
      -h|--help) sed -n '2,16p' "$0"; exit 0 ;;
      *) echo "未知参数: $1" >&2; exit 2 ;;
    esac
  done
  [ -n "$DIR" ] || { echo "用法: sh scripts/lib/sign.sh --dir <目录>" >&2; exit 2; }
  sign_checksums "$DIR"
fi
