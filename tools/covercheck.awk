# 覆盖率门禁：internal/* ≥ IMIN，cmd/* ≥ CMIN，个别包可单独定更高门槛（IMIN_<包名>，见 Makefile）。
# 输入: `go test -cover ./...` 的输出。
# testkit（纯测试夹具）与 acceptance（用例本身）不计入。
BEGIN {
  mod = "github.com/packnest/packnest/";
  bad = 0; seen = 0;
  printf "%-30s %8s %8s\n", "package", "cover", "min";
}
{
  if ($0 !~ /coverage: [0-9.]+%/) next
  pkg = $2; sub(mod, "", pkg);
  if (pkg == "internal/testkit" || pkg ~ /^acceptance/) next
  match($0, /coverage: [0-9.]+%/);
  pct = substr($0, RSTART + 10, RLENGTH - 11) + 0;
  # 单独门槛：内部包默认 IMIN，个别包（编排层 internal/install）用 IMIN_INSTALL
  if (pkg ~ /^internal\//) {
    min = IMIN;
    if (pkg == "internal/install" && IMIN_INSTALL != "") min = IMIN_INSTALL;
  }
  else if (pkg ~ /^cmd\//) { min = CMIN }
  else next
  seen++
  if (pct + 0 < min + 0) {
    printf "%-30s %7.1f%% %7.0f%%  FAIL\n", pkg, pct, min;
    bad = 1;
  } else {
    printf "%-30s %7.1f%% %7.0f%%\n", pkg, pct, min;
  }
}
END {
  if (seen == 0) { print "FAIL: 没有解析到覆盖率数据"; exit 1 }
  if (bad) { print "\n覆盖率未达标"; exit 1 }
  print "\n覆盖率达标"
}
