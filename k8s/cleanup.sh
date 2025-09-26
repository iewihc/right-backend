#!/bin/bash

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

NAMESPACE="right-backend"
K8S_DIR="/home/mr-chi/prod/right/right-backend/k8s"

echo -e "${YELLOW}=== 清理 MicroK8s Right-Backend 部署 ===${NC}"

# 停止端口转发
echo -e "${BLUE}停止端口转发...${NC}"
if pgrep -f "kubectl.*port-forward.*8080:8080" > /dev/null; then
    pkill -f "kubectl.*port-forward.*8080:8080"
    echo -e "${GREEN}端口转发已停止${NC}"
else
    echo -e "${YELLOW}未找到端口转发进程${NC}"
fi

# 删除 Kubernetes 资源
echo -e "${BLUE}删除 Kubernetes 资源...${NC}"
cd "$K8S_DIR"

if microk8s kubectl get namespace "$NAMESPACE" &> /dev/null; then
    microk8s kubectl delete -f ingress.yaml --ignore-not-found=true
    microk8s kubectl delete -f service.yaml --ignore-not-found=true
    microk8s kubectl delete -f deployment.yaml --ignore-not-found=true
    microk8s kubectl delete -f secret.yaml --ignore-not-found=true
    microk8s kubectl delete -f configmap.yaml --ignore-not-found=true
    microk8s kubectl delete -f namespace.yaml --ignore-not-found=true
    
    echo -e "${GREEN}Kubernetes 资源已清理${NC}"
else
    echo -e "${YELLOW}命名空间 $NAMESPACE 不存在${NC}"
fi

# 可选：清理镜像
read -p "是否要删除 Docker 镜像? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${BLUE}删除 Docker 镜像...${NC}"
    
    if command -v podman &> /dev/null; then
        podman rmi localhost:32000/right-backend:latest --force || true
    else
        docker rmi localhost:32000/right-backend:latest --force || true
    fi
    
    echo -e "${GREEN}镜像已清理${NC}"
fi

echo -e "${GREEN}=== 清理完成 ===${NC}"