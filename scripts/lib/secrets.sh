#!/bin/sh
# 凭据文件解析（被 release-npm.sh / set-npm-token.sh 共用）。
#
# 容忍常见手写差异，避免"文件明明有却找不到"：
#   前导 export / 键与等号之间空格 / 值两侧引号 / Windows CRLF / 行首缩进。
# 只输出值；找不到输出空串。**从不把整文件内容带出去。**

# secret_from_file FILE KEY1 [KEY2 ...]
secret_from_file() {
  _file="$1"; shift
  [ -f "$_file" ] || return 0
  for _key in "$@"; do
    _value=$(awk -v k="$_key" '
      { line = $0
        sub(/\r$/, "", line)
        sub(/^[ \t]*export[ \t]+/, "", line)
        sub(/^[ \t]+/, "", line)
        if (line ~ "^" k "[ \t]*=") {
          sub("^" k "[ \t]*=[ \t]*", "", line)
          sub(/[ \t]+$/, "", line)
          if (line ~ /^".*"$/ || line ~ /^\x27.*\x27$/) { line = substr(line, 2, length(line) - 2) }
          print line
          exit
        } }' "$_file" 2>/dev/null)
    if [ -n "$_value" ]; then printf '%s' "$_value"; return 0; fi
  done
  return 0
}

# secret_keys FILE —— 只列键名（用于诊断，不泄露值）
secret_keys() {
  [ -f "$1" ] || return 0
  awk '{ line = $0; sub(/\r$/, "", line); sub(/^[ \t]*export[ \t]+/, "", line)
         if (line ~ /^[ \t]*[A-Za-z_][A-Za-z0-9_]*[ \t]*=/) {
           sub(/[ \t]*=.*/, "", line); sub(/^[ \t]+/, "", line); print line } }' "$1" 2>/dev/null | sort -u
}

# secret_diagnose FILE —— 说明"为什么读不到"，不含任何值
secret_diagnose() {
  _f="$1"
  if [ ! -e "$_f" ]; then echo "文件不存在：$_f"; return 0; fi
  if [ ! -r "$_f" ]; then echo "文件存在但当前用户读不了（检查权限/属主）：$_f"; return 0; fi
  _n=$(wc -l < "$_f" 2>/dev/null | tr -d ' ')
  printf '文件存在且可读（%s 行）；里面的键名：%s\n' "${_n:-?}" "$(secret_keys "$_f" | tr '\n' ' ')"
  if LC_ALL=C grep -q "$(printf '\r')" "$_f" 2>/dev/null; then
    echo "提示：文件含 CRLF 行尾（Windows 编辑器保存过），旧版解析会读不到 —— 当前版本已兼容"
  fi
}
