# 性能门禁（方案 §9.5 第 6 项）：按 ns/op 与阈值比较。
# 阈值来自方案：BenchmarkShimResolve < 15ms、BenchmarkSpecParse < 50µs。
BEGIN {
  max["BenchmarkShimResolve"] = 15000000;
  max["BenchmarkSpecParse"] = 50000;
  bad = 0; seen = 0;
}
/^Benchmark/ {
  name = $1; sub(/-[0-9]+$/, "", name);
  for (i = 2; i <= NF; i++) {
    if ($i == "ns/op") {
      v = $(i - 1) + 0;
      limit = max[name];
      if (limit <= 0) next;
      seen++;
      if (v > limit) {
        printf "FAIL %-24s %12.0f ns/op > %d\n", name, v, limit;
        bad = 1;
      } else {
        printf "ok   %-24s %12.0f ns/op (上限 %d)\n", name, v, limit;
      }
    }
  }
}
END {
  if (seen == 0) { print "FAIL: 没有跑出基准数据"; exit 1 }
  if (bad) { print "\n性能预算被突破"; exit 1 }
  print "\n性能达标"
}
