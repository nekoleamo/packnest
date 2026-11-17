# GUI 覆盖率门禁：逐包对下限（只对"会替换二进制/会改用户环境"的四个包设下限）。
# 输入: `go test -cover ./...` 在 cmd/pk-desktop 下的输出。
# 根包（视图层）不设下限：首帧、对比度这类渲染断言比行覆盖率更能说明问题。
# packaging/genicon 是构建期小工具（没有测试），不参与。
BEGIN {
  mod = "github.com/packnest/packnest/cmd/pk-desktop";
  bad = 0; seen = 0;
  printf "%-28s %8s %6s\n", "package", "cover", "min";
}
{
  if ($0 !~ /coverage: [0-9.]+%/) next
  pkg = "";
  for (i = 1; i <= NF; i++) if ($i ~ /^github\.com\/packnest\/packnest/) { pkg = $i; break }
  if (pkg == "") next
  sub(mod, "", pkg);
  sub(/^\//, "", pkg);
  if (pkg == "") pkg = "(root)";
  if (pkg ~ /^packaging\//) next
  match($0, /coverage: [0-9.]+%/);
  pct = substr($0, RSTART + 10, RLENGTH - 11) + 0;
  min = "";
  if (pkg == "internal/update") min = U;
  else if (pkg == "internal/state") min = S;
  else if (pkg == "internal/pkcli") min = P;
  else if (pkg == "internal/autostart") min = A;
  if (min == "") {
    printf "%-28s %7.1f%% %6s\n", pkg, pct, "—";
    next;
  }
  seen++;
  if (pct + 0 < min + 0) {
    printf "%-28s %7.1f%% %5.0f%%  FAIL\n", pkg, pct, min;
    bad = 1;
  } else {
    printf "%-28s %7.1f%% %5.0f%%\n", pkg, pct, min;
  }
}
END {
  if (seen < 4) { print "FAIL: 没解析到预期的四个包（internal/{update,state,pkcli,autostart}）"; exit 1 }
  if (bad) { print "\nGUI 覆盖率未达标（口径见 pn-GUI-1.0-口径.md H3）"; exit 1 }
  print "\nGUI 覆盖率达标"
}
