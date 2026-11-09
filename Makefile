# packnest · make 门禁（方案 §9.5）
#
# 一条命令 `make check` 跑完 7 项：风格/静态 → 竞态测试 → 覆盖率 →
# 6 平台交叉编译 → fuzz → 性能 → --json 契约。全绿才可提交。
#
# `make accept` 只跑验收用例（TestAcceptance_*）——"验收是跑测试，不是人工敲命令"。

SHELL := /bin/sh
MODULE := github.com/packnest/packnest
BIN := bin/pk
DIST := dist
BUILD := build

# 覆盖率门禁（已实现包；新增包要在 COVER_PKGS 语境下自然纳入，
# 尚未实现的包（archive/verify/companion/toolchain）等里程碑补实现时再纳入统计）。
COVER_INTERNAL_MIN := 85
COVER_CMD_MIN := 70

# fuzz 目标（每个都是 parser/路径类红线）。
FUZZ_TARGETS := FuzzSpec FuzzSHASUMS FuzzArchivePath
FUZZ_TIME := 30s

# 有 tag 时用 tag（去掉前导 v），否则用默认开发版本号。
VERSION ?= $(shell v=$$(git describe --tags --always --dirty 2>/dev/null | sed -n 's/^v\?\([0-9].*\)/\1/p'); echo "$${v:-0.3.0-M2}")
LDFLAGS := -s -w -X main.Version=$(VERSION)
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: help build test race check fmt vet cover cross fuzz bench golden accept clean dist list-providers

help:
	@echo "make build     构建 ./bin/pk（CGO_ENABLED=0）"
	@echo "make test      零外网单测"
	@echo "make check     7 项门禁（提交前必跑）"
	@echo "make accept    只跑验收用例 TestAcceptance_*"
	@echo "make cross     6 平台交叉编译到 $(DIST)/"
	@echo "make cover     只看覆盖率明细"
	@echo "make bench     只看性能预算"
	@echo "make clean     清理产物"

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/pk
	@echo "已构建 $(BIN)（$(VERSION)）"

test:
	CGO_ENABLED=0 go test ./...

race:
	CGO_ENABLED=0 go test -race -count=1 ./...

fmt:
	@out="$$(gofmt -l . | grep -v '^$$' || true)"; \
	if [ -n "$$out" ]; then echo "gofmt 未通过:"; echo "$$out"; exit 1; fi
	@echo "gofmt 通过"

vet:
	go vet ./...
	@echo "go vet 通过"

cover:
	@mkdir -p $(BUILD)
	@CGO_ENABLED=0 go test -count=1 -cover ./... 2>&1 | tee $(BUILD)/cover.log >/dev/null || true
	@awk -v IMIN=$(COVER_INTERNAL_MIN) -v CMIN=$(COVER_CMD_MIN) -f tools/covercheck.awk $(BUILD)/cover.log

bench:
	@mkdir -p $(BUILD)
	@CGO_ENABLED=0 go test -run='^$$' -bench='BenchmarkSpecParse|BenchmarkShimResolve' -benchtime=200x ./internal/spec ./internal/shim \
		| tee $(BUILD)/bench.log >/dev/null
	@awk -f tools/benchcheck.awk $(BUILD)/bench.log

golden:
	CGO_ENABLED=0 go test -count=1 -run 'Golden|NDJSON' ./internal/jsonout
	@echo "--json 契约 golden 通过（改契约请先改 testdata 再跑 -update）"

cross:
	@mkdir -p $(DIST)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		out=$(DIST)/packnest_$(VERSION)_$${os}_$${arch}; \
		if [ "$$os" = "windows" ]; then out=$$out.exe; fi; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$out ./cmd/pk || exit 1; \
		printf '  %-46s %s\n' "$$out" "$$(du -h $$out | cut -f1)"; \
	done
	@echo "6 平台交叉编译通过（CGO 未泄漏）"

fuzz:
	@for t in $(FUZZ_TARGETS); do \
		pkg=$$(grep -rl "func $$t" internal | head -1 | xargs dirname); \
		echo "== $$t ($$pkg, $(FUZZ_TIME))"; \
		CGO_ENABLED=0 go test -run='^$$' -fuzz="^$$t$$" -fuzztime=$(FUZZ_TIME) ./$$pkg || exit 1; \
	done

accept:
	CGO_ENABLED=0 go test -count=1 -run 'TestAcceptance_' -v ./acceptance/...

# ⭐ 7 项门禁，顺序即"越便宜越先"。
check: fmt vet race cover cross fuzz bench golden
	@echo "===== make check 全部通过（7 项）====="

list-providers:
	@go run ./cmd/pk mirror list

clean:
	rm -rf $(BUILD) $(DIST) $(BIN) coverage.out
