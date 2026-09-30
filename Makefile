# ============================================================================
#  BunkrDownloader · Go / Gin build
#
#  Quick start:
#    make setup     install frontend + Go deps
#    make dev       build the SPA, then run the server on :8765
#    make test      go vet + unit + integration tests
#    make docker    build the container image
# ============================================================================

SHELL       := /bin/sh
MODULE      := github.com/chaunceyxie1/BunkrDownloader
BINARY      := bunkr-web
CMD         := ./cmd/bunkr-web
VERSION     ?= 1.0.0
LDFLAGS     := -s -w -X main.version=$(VERSION)
GOFILES     := $(shell find . -type f -name '*.go' -not -path './frontend/*')
EMBED_DIR   := internal/web/dist
FRONTEND    := frontend
DIST        := $(FRONTEND)/dist
GOOS        ?= $(shell go env GOOS)
GOARCH      ?= $(shell go env GOARCH)
DIST_DIR    := bin/$(GOOS)-$(GOARCH)
PKG         := $(DIST_DIR)/$(BINARY)

.DEFAULT_GOAL := help
.PHONY: help setup web build run dev test test-unit test-integration vet fmt \
        lint clean docker docker-run compose-up compose-down tidy version

help: ## 显示所有可用命令
	@echo "BunkrDownloader — available targets:"
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

setup: ## 安装前端与 Go 依赖
	cd $(FRONTEND) && npm install
	go mod download
	@echo "✔ dependencies ready"

web: ## 构建 Vue 前端并复制到 Go embed 目录
	cd $(FRONTEND) && npm install --silent && npm run build
	@mkdir -p $(EMBED_DIR)
	@rm -rf $(EMBED_DIR)/*
	@cp -r $(DIST)/. $(EMBED_DIR)/
	@echo "✔ frontend embedded into $(EMBED_DIR)"

build: web ## 构建包含前端的二进制
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(PKG) $(CMD)
	@echo "✔ built $(PKG)"

run: ## 构建并运行（默认 :8765）
	$(MAKE) build
	./$(PKG)

dev: ## 前端热更新 + 后端（两个终端）
	@echo "终端 1: cd $(FRONTEND) && npm run dev"
	@echo "终端 2: make build && ./$(PKG)"
	@$(MAKE) build
	./$(PKG)

# ------------------------------------------------------------------- quality

vet: ## go vet
	go vet ./...

fmt: ## 格式化
	gofmt -l -w $(GOFILES)

lint: fmt vet ## 格式化并静态检查
	@echo "✔ lint clean"

test-unit: ## 仅单元测试（不启动 aria2）
	go test ./internal/store/... ./internal/auth/... ./internal/bunkr/... \
	        ./internal/aria2/... ./internal/api/... ./internal/config/...

test-integration: ## 集成测试（需要 aria2c，会自动下载）
	go test ./internal/downloads/... -v -timeout 600s

test: vet test-unit test-integration ## 全部测试
	@echo "✔ all tests passed"

tidy: ## 整理 go.mod
	go mod tidy

# --------------------------------------------------------------------- docker

docker: ## 构建容器镜像
	docker build -t ghcr.io/chaunceyxie1/bunkrdownloader:$(VERSION) -t bunkrdownloader:latest .

docker-run: ## 运行容器
	docker run --rm -it -p 8765:8765 \
		-v bunkr-data:/data -v $$(pwd)/downloads:/downloads \
		bunkrdownloader:latest

compose-up: ## docker compose 启动
	docker compose up -d --build

compose-down: ## docker compose 停止
	docker compose down

# ---------------------------------------------------------------------- misc

clean: ## 清理构建产物
	rm -rf bin $(EMBED_DIR)/* $(DIST)
	@echo "✔ cleaned"

version: ## 打印版本
	@echo $(VERSION)
