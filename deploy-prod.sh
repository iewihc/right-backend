#!/bin/bash

# Right-Backend 生產環境部署腳本
# Production Deployment Script for Right-Backend

set -e  # 出現錯誤時立即退出

# 顏色定義
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日誌函數
log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

# 檢查必要工具
check_requirements() {
    log "檢查部署環境..."
    
    command -v docker >/dev/null 2>&1 || error "Docker 未安裝"
    command -v docker-compose >/dev/null 2>&1 || error "Docker Compose 未安裝"
    command -v kubectl >/dev/null 2>&1 || error "kubectl 未安裝"
    command -v microk8s >/dev/null 2>&1 || error "microk8s 未安裝"
    
    success "所有必要工具已安裝"
}

# 創建必要的目錄結構
create_directories() {
    log "準備 Kubernetes 存儲..."
    
    # 確保 microk8s hostpath-provisioner 可用
    if ! microk8s kubectl get storageclass microk8s-hostpath >/dev/null 2>&1; then
        microk8s enable storage
        sleep 10
    fi
    
    success "Kubernetes 存儲準備完成"
}

# 部署基礎設施服務到 Kubernetes
deploy_infrastructure() {
    log "部署基礎設施服務到 Kubernetes (MongoDB, Redis, RabbitMQ)..."
    
    # 確保 namespace 存在
    kubectl create namespace right-backend --dry-run=client -o yaml | kubectl apply -f -
    
    # 部署基礎設施服務
    kubectl apply -f k8s/mongodb-k8s.yaml
    kubectl apply -f k8s/redis-k8s.yaml
    kubectl apply -f k8s/rabbitmq-k8s.yaml
    
    # 等待服務準備就緒
    log "等待基礎設施服務準備就緒..."
    kubectl wait --for=condition=ready pod -l app=mongodb -n right-backend --timeout=300s
    kubectl wait --for=condition=ready pod -l app=redis -n right-backend --timeout=180s
    kubectl wait --for=condition=ready pod -l app=rabbitmq -n right-backend --timeout=240s
    
    # 檢查服務狀態
    kubectl get pods -n right-backend -l 'app in (mongodb,redis,rabbitmq)'
    
    success "基礎設施服務部署完成"
}

# 構建並推送 Docker 鏡像
build_and_push_image() {
    log "構建 Docker 鏡像..."
    
    # 構建鏡像
    docker build -t localhost:32000/right-backend:latest .
    docker build -t localhost:32000/right-backend:$(date +%Y%m%d_%H%M%S) .
    
    # 推送到 microk8s registry
    docker push localhost:32000/right-backend:latest
    
    success "Docker 鏡像構建並推送完成"
}

# 部署到 Kubernetes
deploy_to_kubernetes() {
    log "部署到 Kubernetes..."
    
    # 確保 namespace 存在
    kubectl create namespace right-backend --dry-run=client -o yaml | kubectl apply -f -
    
    # 應用 ConfigMap
    kubectl apply -f k8s/configmap.yaml
    
    # 應用 Service
    kubectl apply -f k8s/service.yaml
    
    # 應用 Deployment (滾動更新)
    kubectl apply -f k8s/deployment.yaml
    
    # 等待部署完成
    log "等待 Kubernetes 部署完成..."
    kubectl rollout status deployment/right-backend-deployment -n right-backend --timeout=600s
    
    success "Kubernetes 部署完成"
}

# 驗證部署
verify_deployment() {
    log "驗證部署狀態..."
    
    echo ""
    echo "=== 基礎設施服務狀態 ==="
    docker-compose -f docker-compose.prod.yml ps
    
    echo ""
    echo "=== Kubernetes Pods 狀態 ==="
    kubectl get pods -n right-backend -o wide
    
    echo ""
    echo "=== Kubernetes Services 狀態 ==="
    kubectl get services -n right-backend
    
    echo ""
    echo "=== 服務端點信息 ==="
    NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
    NODE_PORT=$(kubectl get service right-backend-nodeport -n right-backend -o jsonpath='{.spec.ports[0].nodePort}')
    
    echo "Application URL: http://${NODE_IP}:${NODE_PORT}"
    echo "MongoDB: kubectl port-forward svc/mongodb-service 27017:27017 -n right-backend (admin/96787421)"
    echo "Redis: kubectl port-forward svc/redis-service 6379:6379 -n right-backend (password: 96787421)"
    echo "RabbitMQ Management: http://${NODE_IP}:30672 (admin/96787421)"
    echo "Seq Logs: https://seq.mr-chi-tech.com (xlXtEzkPCbaRLEQGCoxg)"
    
    success "部署驗證完成"
}

# 清理舊資源
cleanup_old_resources() {
    log "清理舊的 Docker 資源..."
    
    # 清理未使用的 Docker 資源
    docker system prune -f
    
    # 清理舊的 Kubernetes Pod (保留最新的3個)
    kubectl delete pods -n right-backend --field-selector=status.phase!=Running || true
    
    success "資源清理完成"
}

# 主要部署流程
main() {
    log "開始 Right-Backend 生產環境部署..."
    echo "========================================"
    
    case "${1:-all}" in
        "check")
            check_requirements
            ;;
        "infra")
            check_requirements
            create_directories
            deploy_infrastructure
            ;;
        "k8s")
            check_requirements
            build_and_push_image
            deploy_to_kubernetes
            verify_deployment
            ;;
        "all")
            check_requirements
            create_directories
            deploy_infrastructure
            build_and_push_image
            deploy_to_kubernetes
            verify_deployment
            cleanup_old_resources
            ;;
        "verify")
            verify_deployment
            ;;
        "cleanup")
            cleanup_old_resources
            ;;
        *)
            echo "用法: $0 [check|infra|k8s|all|verify|cleanup]"
            echo ""
            echo "  check   - 檢查部署環境"
            echo "  infra   - 僅部署基礎設施服務"
            echo "  k8s     - 僅部署 Kubernetes 應用"
            echo "  all     - 完整部署 (默認)"
            echo "  verify  - 驗證部署狀態"
            echo "  cleanup - 清理舊資源"
            exit 1
            ;;
    esac
    
    echo "========================================"
    success "部署完成！"
    
    if [[ "${1:-all}" == "all" || "${1}" == "verify" ]]; then
        echo ""
        echo "🎉 Right-Backend 已成功部署到生產環境！"
        echo ""
        echo "📊 監控和管理:"
        echo "   - Application: http://${NODE_IP:-localhost}:${NODE_PORT:-30082}"
        echo "   - Seq Logs: https://seq.mr-chi-tech.com"
        echo "   - RabbitMQ: http://localhost:15672"
        echo ""
        echo "📝 接下來的步驟:"
        echo "   1. 配置 Azure DevOps Pipeline 變數"
        echo "   2. 設置 SSL 證書和域名"
        echo "   3. 配置監控和告警"
        echo "   4. 進行端到端測試"
    fi
}

# 執行主函數
main "$@"