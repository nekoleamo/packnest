#!/bin/sh
# 发布签名公钥必须六处一致：pk 内置、install.sh、install.ps1、install-gui.sh、install-gui.ps1、GUI 自更新器。
# 漏一处就是"静默降级成只比 sha256"——所以做成门禁，而不是靠记忆。
#
#   用法：make keys-sync / sh tools/keys-sync.sh
#   未配公钥（自己从源码构建）时：打印 warn，不失败。
set -eu
cd "$(dirname "$0")/.."

pick_pk() {   # 从文本里挑出 ssh-ed25519 <base64> 对，排序去重
  sed -n 's/.*\(ssh-ed25519[[:space:]][A-Za-z0-9+/=]\{20,\}\).*/\1/p' "$1" 2>/dev/null | sort -u
}

a=$(pick_pk internal/verify/keys/allowed_signers)
b=$(pick_pk scripts/install.sh)
c=$(pick_pk scripts/install.ps1)
d=$(pick_pk scripts/install-gui.sh)
e=$(pick_pk scripts/install-gui.ps1)
f=$(pick_pk cmd/pk-desktop/internal/update/allowed_signers)

if [ -z "$a" ] && [ -z "$b" ] && [ -z "$c" ] && [ -z "$d" ] && [ -z "$e" ] && [ -z "$f" ]; then
  echo "keys-sync: 六处都没有公钥（源码构建场景，跳过）"
  exit 0
fi

fail=0
same() { [ "$1" = "$2" ] || { echo "✘ $3 不一致"; fail=1; }; }
same "$a" "$b" "internal/verify/keys/allowed_signers 与 scripts/install.sh"
same "$a" "$c" "internal/verify/keys/allowed_signers 与 scripts/install.ps1"
same "$a" "$d" "internal/verify/keys/allowed_signers 与 scripts/install-gui.sh"
same "$a" "$e" "internal/verify/keys/allowed_signers 与 scripts/install-gui.ps1"
same "$a" "$f" "internal/verify/keys/allowed_signers 与 cmd/pk-desktop/internal/update/allowed_signers"
if [ "$fail" -ne 0 ]; then
  echo "  把 scripts/sign-release.sh 打印的公钥同时写进这六处，再重跑。" >&2
  exit 1
fi
n=$(printf '%s\n' "$a" | sed '/^$/d' | wc -l | tr -d ' ')
echo "keys-sync: ok（$n 把公钥，六处一致）"
