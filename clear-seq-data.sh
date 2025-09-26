#!/bin/bash

echo "=== 清除 Seq 日誌資料腳本 ==="
echo "這個腳本會完全移除所有 Seq 相關的容器、數據卷和網路"
echo ""

# 停止並移除 Seq 相關容器
echo "1. 停止並移除 Seq 容器..."
docker-compose -f docker-compose-seq.yml down

# 強制移除容器（如果還存在）
echo "2. 強制移除 Seq 容器（如果存在）..."
docker rm -f right-seq-logserver 2>/dev/null || echo "   容器已不存在"

# 移除數據卷
echo "3. 移除 Seq 數據卷..."
docker volume rm right-backend_seq-data 2>/dev/null || echo "   seq-data 卷已不存在"
docker volume rm right-backend_seq-logs 2>/dev/null || echo "   seq-logs 卷已不存在"

# 移除網路
echo "4. 移除日誌網路..."
docker network rm right-backend_logging-network 2>/dev/null || echo "   logging-network 已不存在"

# 清理未使用的資源
echo "5. 清理未使用的 Docker 資源..."
docker system prune -f

# 顯示清理結果
echo ""
echo "=== 清理完成 ==="
echo "檢查剩餘的相關資源："

echo ""
echo "剩餘容器："
docker ps -a | grep seq || echo "無相關容器"

echo ""
echo "剩餘數據卷："
docker volume ls | grep seq || echo "無相關數據卷"

echo ""
echo "剩餘網路："
docker network ls | grep logging || echo "無相關網路"

echo ""
echo "✅ Seq 資料清除完成！"
echo "如需重新啟動 Seq，請執行："
echo "docker-compose -f docker-log-compose.yml up -d"