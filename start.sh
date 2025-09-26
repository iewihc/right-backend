#!/bin/bash

echo "🚀 啟動 Right-Backend 服務..."

# 檢查 Docker 是否運行
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker 未運行，請先啟動 Docker"
    exit 1
fi

# 檢查 docker-compose 是否存在
if ! command -v docker-compose > /dev/null 2>&1; then
    echo "❌ docker-compose 未安裝"
    exit 1
fi

# 停止現有服務
echo "🛑 停止現有服務..."
docker-compose down

# 建構並啟動所有服務
echo "🐳 建構並啟動所有服務..."
docker-compose up --build -d

# 等待服務啟動
echo "⏳ 等待服務啟動..."
sleep 30

# 檢查服務狀態
echo "📊 檢查服務狀態..."
docker-compose ps

echo ""
echo "✅ 服務已啟動完成！"
echo ""
echo "📱 服務連結："
echo "   Right Backend:  http://localhost:8090"
echo "   MongoDB:        localhost:27019 (admin/96787421)"
echo "   Redis:          localhost:6379 (password: 96787421)"  
echo "   RabbitMQ Web:   http://localhost:15672 (admin/96787421)"
echo ""
echo "📋 查看日誌: docker-compose logs -f"
echo "🛑 停止服務: docker-compose down"