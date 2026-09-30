# ============================================================================
#  BunkrDownloader (Wails v3 desktop) · build
#
#  Quick start:
#    make setup    install the wails3 CLI + frontend deps
#    make dev      run the desktop app in development mode (HMR)
#    make build    produce a production binary in bin/
#    make test     go vet + unit + integration tests
#
#  NOTE: Wails v3 reads every build option from the Taskfiles
#  (Taskfile.yml + build/<os>/Taskfile.yml) — `wails3 build` accepts no
#  -o/-ldflags/-clean flags. Version, tags and output name are therefore
#  configured in build/Taskfile.yml, and VERSION is passed through the
#  environment.
# ============================================================================

SHELL   := /bin/sh
BINARY  := BunkrDownloader
VERSION ?= 1.0.0
export VERSION
WAILS   := wails3
GOOS    ?= $(shell go env GOOS)
GOARCH  ?= $(shell go env GOARCH)
DIST    := bin/$(GOOS)-$(GOARCH)
PKG     := $(DIST)/$(BINARY)
FRONTEND:= frontend

.DEFAULT_GOAL := help
.PHONY: help setup bindings web build dev run test test-unit test-integration \
        vet fmt lint clean package install-cli tidy aria2

help: ## 显示可用命令
	@echo "BunkrDownloader Desktop — available targets:"
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

install-cli: ## 安装 wails3 CLI
	go install github.com/wailsapp/wails/v3/cmd/wails3@latest

setup: ## 安装前端与 Go 依赖
	cd $(FRONTEND) && npm install
	go mod download
	@echo "✔ dependencies ready"

bindings: ## 重新生成 Wails 前端绑定
	$(WAILS) generate bindings
	@echo "✔ bindings written to $(FRONTEND)/bindings"

web: ## 仅构建前端
	cd $(FRONTEND) && npm run build
	@echo "✔ SPA built"

# Build options live in the Taskfiles, so this is a plain `wails3 build`
# (which runs the OS-specific `build` task and embeds frontend/dist).
build: ## 构建桌面应用（含前端）
	$(WAILS) build -nocolour
	@mkdir -p $(DIST)
	@cp bin/$(BINARY).* $(PKG).* 2>/dev/null || true
	@echo "✔ built $(DIST)"

package: ## 打包安装程序（NSIS / dmg / deb）
	$(WAILS) package -nocolour
	@echo "✔ installer written to build/bin"

dev: ## 开发模式（前端热更新）
	$(WAILS) dev -config ./build/config.yml -loglevel Debug

run: build ## 构建并运行
	$(WAILS) task run

aria2: ## 报告 aria2c 位置（桌面端会自动探测）
	@command -v aria2c >/dev/null 2>&1 \
		&& aria2c --version | head -1 \
		|| echo "aria2c not on PATH — the app will try to download it, or set BUNKR_ARIA2_BIN"

# ------------------------------------------------------------------- quality

vet: ## go vet
	go vet ./...

fmt: ## 格式化
	gofmt -l -w $(shell find . -type f -name '*.go' -not -path './frontend/*')

lint: fmt vet ## 格式化并静态检查
	@echo "✔ lint clean"

test-unit: ## 单元测试
	go test ./internal/store/... ./internal/auth/... ./internal/bunkr/... \
	        ./internal/aria2/... ./internal/hub/... ./internal/bunkrtest/... \
	        ./internal/config/... ./internal/services/... -count=1

test-integration: ## 集成测试（会拉起真实 aria2c）
	go test ./internal/downloads/... -v -timeout 600s -count=1

test: vet test-unit test-integration ## 全部测试
	@echo "✔ all tests passed"

tidy: ## 整理 go.mod
	go mod tidy

clean: ## 清理构建产物
	rm -rf bin frontend/dist
	@echo "✔ cleaned"
