.PHONY: help build run init-db clean test docker-up docker-down deps

# 預設目標
help:
	@echo "計程車調度API系統 - 可用命令："
	@echo ""
	@echo "  build        - 編譯應用程式"
	@echo "  run          - 運行應用程式"
	@echo "  init-db      - 初始化 MongoDB 數據庫"
	@echo "  clean        - 清理編譯檔案"
	@echo "  test         - 運行測試"
	@echo "  deps         - 安裝/更新依賴項"
	@echo "  docker-up    - 啟動 Docker 服務 (MongoDB, Redis, RabbitMQ)"
	@echo "  docker-down  - 停止 Docker 服務"
	@echo "  docker-logs  - 查看 Docker 服務日誌"
	@echo "  dev          - 開發模式：啟動 Docker 服務並運行應用程式"
	@echo "  api-docs     - 在瀏覽器中打開 API 文檔"
	@echo "  release      - 創建發布版本"
	@echo ""

# 編譯應用程式
build:
	@echo "🔨 編譯應用程式..."
	go build -o bin/taxi-api .
	@echo "✅ 編譯完成：bin/taxi-api"

# 運行應用程式
run:
	@echo "🚀 啟動計程車調度API..."
	go run main.go

# 初始化數據庫
init-db:
	@echo "🔄 初始化 MongoDB 數據庫..."
	go run cmd/init/main.go

# 編譯初始化程序
build-init:
	@echo "🔨 編譯初始化程序..."
	go build -o bin/init-db cmd/init/main.go

# 運行測試
test:
	@echo "🧪 運行測試..."
	go test -v ./...

# 清理編譯檔案
clean:
	@echo "🧹 清理檔案..."
	rm -rf bin/
	go clean

# 安裝/更新依賴項
deps:
	@echo "📦 安裝依賴項..."
	go mod download
	go mod tidy

# 啟動所有 Docker 服務 (包含應用程式)
docker-up:
	@echo "🐳 啟動所有 Docker 服務..."
	docker-compose up -d
	@echo "✅ 所有服務已啟動："
	@echo "   Right Backend: http://localhost:8090"
	@echo "   MongoDB:       localhost:27019 (admin/96787421)"
	@echo "   Redis:         localhost:6379 (password: 96787421)"
	@echo "   RabbitMQ:      http://localhost:15672 (admin/96787421)"

# 只啟動基礎服務 (不包含應用程式)
docker-services:
	@echo "🐳 啟動基礎服務..."
	docker-compose up -d mongodb redis rabbitmq
	@echo "✅ 基礎服務已啟動，等待服務就緒..."
	@sleep 10

# 停止 Docker 服務
docker-down:
	@echo "🛑 停止 Docker 服務..."
	docker-compose down

# 查看 Docker 服務日誌
docker-logs:
	@echo "📋 查看服務日誌..."
	docker-compose logs -f

# 開發模式 (只啟動基礎服務，本機運行應用)
dev: docker-services
	@echo "🚀 啟動 API 服務..."
	go run main.go

# 在瀏覽器中打開 API 文檔
api-docs:
	@echo "📖 打開 API 文檔..."
	@command -v open >/dev/null 2>&1 && open http://localhost:8090/docs || \
	 command -v xdg-open >/dev/null 2>&1 && xdg-open http://localhost:8090/docs || \
	 echo "請在瀏覽器中打開：http://localhost:8090/docs"

# 創建發布版本
release: clean build build-init
	@echo "📦 創建發布版本..."
	mkdir -p release
	cp bin/taxi-api release/
	cp README.md release/
	cp docker-compose.yml release/
	cp init-mongo.js release/
	tar -czf release/taxi-api-$(shell date +%Y%m%d-%H%M%S).tar.gz -C release .
	@echo "✅ 發布包已創建在 release/ 目錄"

# 檢查程式碼品質
lint:
	@echo "🔍 檢查程式碼品質..."
	@command -v golangci-lint >/dev/null 2>&1 || (echo "請安裝 golangci-lint"; exit 1)
	golangci-lint run

# 格式化程式碼
fmt:
	@echo "🎨 格式化程式碼..."
	go fmt ./...

# 顯示項目資訊
info:
	@echo "📊 項目資訊："
	@echo "  Go 版本：    $(shell go version)"
	@echo "  模組名稱：   $(shell go list -m)"
	@echo "  文件數量：   $(shell find . -name '*.go' | wc -l)"
	@echo "  代碼行數：   $(shell find . -name '*.go' -exec wc -l {} + | tail -1)"

# 更新依賴
tidy:
	@echo "📦 更新依賴項..."
	go mod tidy

# 檢查依賴安全漏洞
audit:
	@echo "🔍 檢查依賴安全漏洞..."
	go list -json -deps | nancy sleuth 