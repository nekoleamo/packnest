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
VERSION ?= $(shell v=$$(git describe --tags --always --dirty 2>/dev/null | sed -n 's/^v\?\([0-9].*\)/\1/p'); echo "$${v:-0.7.0}")
LDFLAGS := -s -w -X main.Version=$(VERSION)
# 归档里所有文件的 mtime（可重复构建的前提；改它会改变所有产物的 sha256）
SOURCE_DATE := 202001010000
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: gitee-release help build test race check fmt vet cover cross fuzz bench golden accept clean dist list-providers release checksums npm-packages npm-dry publish publish-stage stage-list promote site verify-release publish-manifests

help:
	@echo "make build     构建 ./bin/pk（CGO_ENABLED=0）"
	@echo "make test      零外网单测"
	@echo "make check     7 项门禁（提交前必跑）"
	@echo "make accept    只跑验收用例 TestAcceptance_*"
	@echo "make cross     6 平台交叉编译到 $(DIST)/"
	@echo "make cover     只看覆盖率明细"
	@echo "make bench     只看性能预算"
	@echo "make release   6 平台发布包 + checksums.txt 到 dist/"
	@echo "make npm-dry   只打包 npm 包并校验 manifest（不发、不需要凭据）"
	@echo "make publish   stage + promote（推荐，兼容 2027 新规）"
	@echo "make publish-stage  只 stage（无需 2FA，CI 可跑）"
	@echo "make publish-direct 直接发布（2027-01 前有效，已弃用）"
	@echo "make gitee-release  上传产物到 gitee release（二进制主源）"
	@echo "make site      渲染官网到 site/dist（只注入版本号，无前端工具链）"
	@echo "make verify-release  核对已发布产物与 checksums.txt（发布后必跑）"
	@echo "make publish-manifests  verify → 生成并推送 brew/scoop/winget 清单"
	@echo "make clean     清理产物"

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/pk
	@echo "已构建 $(BIN)（$(VERSION)）"

test:
	CGO_ENABLED=0 go test ./...

# -race 需要 cgo：单二进制承诺由 cross 目标（CGO_ENABLED=0）守，这里必须开 cgo
race:
	CGO_ENABLED=1 go test -race -count=1 ./...

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

# 官网：site/ → site/dist（版本号注入）。部署由 .github/workflows/pages.yml 负责。
site:
	@sh scripts/build-site.sh

# 把已发布产物拉下来和 checksums.txt 对：清单与产物对不上时立刻红。
verify-release:
	@sh scripts/verify-release.sh

# 渠道清单：必须先过 verify-release，否则会把错的 sha256 推进 tap/bucket。
publish-manifests: verify-release
	@sh scripts/gen-manifests.sh
	@sh scripts/release-taps.sh

list-providers:
	@go run ./cmd/pk mirror list

# 发布产物（方案 §13.3）：dist/v<version>/packnest_<version>_<os>_<arch>.{tar.gz,zip} + checksums.txt
# 目录结构故意与 gitee release 的 /download/v<version>/<file> 对齐，
# 这样把 dist/ 整个丢到任意静态 host，install.sh --base <url> 就能直接装（不走 gitee/GitHub）。
# 归档必须可重复：同一版本重跑构建要得到同一个 sha256（否则重跑一次 release 就会让
# 已发布的 tap/scoop 清单与产物对不上，0.7.0 就栽在这）。两条规则：
#   1. 打包前把产物 mtime 钉死（SOURCE_DATE）；
#   2. tar 走 `tar -cf - | gzip -n` 只此一种（gzip -n 不写文件名与时间戳），zip 用 -X。
release:
	@rm -rf $(DIST)
	@mkdir -p $(DIST)/v$(VERSION)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		stage="$(CURDIR)/$(BUILD)/pkg/$${os}_$${arch}"; \
		rm -rf $$stage; mkdir -p $$stage; \
		out="$(CURDIR)/$(DIST)/v$(VERSION)"; \
		if [ "$$os" = "windows" ]; then \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$stage/packnest.exe ./cmd/pk || exit 1; \
			touch -t $(SOURCE_DATE) $$stage/packnest.exe; \
			(cd $$stage && rm -f "$$out/packnest_$(VERSION)_$${os}_$${arch}.zip" && zip -q -X -o "$$out/packnest_$(VERSION)_$${os}_$${arch}.zip" packnest.exe); \
		else \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$stage/packnest ./cmd/pk || exit 1; \
			touch -t $(SOURCE_DATE) $$stage/packnest; \
			(tar -cf - -C $$stage packnest | gzip -n > "$$out/packnest_$(VERSION)_$${os}_$${arch}.tar.gz"); \
		fi; \
	done
	@$(MAKE) --no-print-directory checksums
	@printf '%s\n' "$(VERSION)" > $(DIST)/latest
	@cp scripts/install.sh $(DIST)/install.sh
	@echo "自托管基地址就绪：把 $(DIST)/ 整个目录丢到任意静态 host，即可 --base <url>"

checksums:
	@cd $(DIST)/v$(VERSION) && { sha256sum packnest_*.tar.gz packnest_*.zip 2>/dev/null || shasum -a 256 packnest_*.tar.gz packnest_*.zip; } > checksums.txt
	@cp $(DIST)/v$(VERSION)/checksums.txt $(DIST)/checksums.txt
	@echo "已生成 checksums.txt：" && cat $(DIST)/checksums.txt

# npm 包（本地打包，不发）
npm-packages:
	@sh scripts/release-npm.sh

# 只打包 + 校验 manifest，不发布（凭据也不需要）
npm-dry:
	@sh scripts/release-npm.sh --dry-run

# 发布正式版本（stage + 立刻 promote）——推荐路径。
# npm 从 2027-01 起不允许 granular token 直接发布新版本，只允许 stage；
# 所以 --stage 才是长期方案，promote 需要 2FA（本地 NPM_TOTP_SECRET 可自动出码）。
publish: publish-stage promote

# 只 stage（不需要 2FA，CI 里也可跑）
publish-stage: release
	@sh scripts/release-npm.sh --stage

# 提升 staged 版本（需要 2FA：本地 TOTP 或 --otp）
promote:
	@sh scripts/release-npm.sh --promote --list

# 仍然保留"直接发布"（在 2027-01 之前有效）
publish-direct: release
	@sh scripts/release-npm.sh --publish

# 上传到 gitee release（凭据走 ~/.pi/agent/local/secrets.env 的 GITEE_TOKEN）
gitee-release: release
	@sh scripts/release-gitee.sh

clean:
	rm -rf $(BUILD) $(DIST) $(BIN) coverage.out
