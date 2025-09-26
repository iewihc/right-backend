#!/bin/bash

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置变量
BACKEND_DIR="/home/mr-chi/prod/right/right-backend"
K8S_DIR="/home/mr-chi/prod/right/right-backend/k8s"
IMAGE_NAME="localhost:32000/right-backend:latest"
NAMESPACE="right-backend"

# 检查依赖
check_dependencies() {
    echo -e "${BLUE}检查依赖项...${NC}"
    
    if ! command -v microk8s &> /dev/null; then
        echo -e "${RED}错误: MicroK8s 未安装${NC}"
        exit 1
    fi
    
    if ! microk8s status --wait-ready &> /dev/null; then
        echo -e "${RED}错误: MicroK8s 未就绪${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}依赖项检查完成${NC}"
}

# 启用 MicroK8s 插件
enable_addons() {
    echo -e "${BLUE}启用 MicroK8s 插件...${NC}"
    
    microk8s enable registry
    microk8s enable dns
    microk8s enable ingress
    microk8s enable storage
    microk8s enable metallb:10.64.140.43-10.64.140.49
    
    echo -e "${GREEN}插件启用完成${NC}"
}

# 构建和推送镜像
build_and_push_image() {
    echo -e "${BLUE}构建和推送 Docker 镜像...${NC}"
    
    cd "$BACKEND_DIR"
    
    # 构建镜像
    if command -v podman &> /dev/null; then
        echo -e "${YELLOW}使用 Podman 构建镜像...${NC}"
        podman build -t "$IMAGE_NAME" .
        podman push "$IMAGE_NAME"
    else
        echo -e "${YELLOW}使用 Docker 构建镜像...${NC}"
        docker build -t "$IMAGE_NAME" .
        docker push "$IMAGE_NAME"
    fi
    
    echo -e "${GREEN}镜像构建和推送完成${NC}"
}

# 创建 Kubernetes Secret (不再需要Google Services)
create_secrets() {
    echo -e "${BLUE}跳过 Google Services Secret 创建（已使用 Expo 推送）...${NC}"
    echo -e "${GREEN}Secret 创建阶段完成${NC}"
}

# 部署 Kubernetes 资源
deploy_k8s_resources() {
    echo -e "${BLUE}部署 Kubernetes 资源...${NC}"
    
    cd "$K8S_DIR"
    
    # 按顺序部署资源
    microk8s kubectl apply -f namespace.yaml
    microk8s kubectl apply -f configmap.yaml
    # 不再需要 secret.yaml (Google Services)
    microk8s kubectl apply -f deployment.yaml
    microk8s kubectl apply -f service.yaml
    microk8s kubectl apply -f ingress.yaml
    
    echo -e "${GREEN}Kubernetes 资源部署完成${NC}"
}

# 等待部署就绪
wait_for_deployment() {
    echo -e "${BLUE}等待部署就绪...${NC}"
    
    microk8s kubectl wait --for=condition=available --timeout=300s deployment/right-backend-deployment -n "$NAMESPACE"
    
    echo -e "${GREEN}部署已就绪${NC}"
}

# 设置端口转发
setup_port_forward() {
    echo -e "${BLUE}设置端口转发到 localhost:8080...${NC}"
    
    # 检查是否已有端口转发进程
    if pgrep -f "kubectl.*port-forward.*8080:8080" > /dev/null; then
        echo -e "${YELLOW}端口转发已存在，停止现有进程...${NC}"
        pkill -f "kubectl.*port-forward.*8080:8080"
        sleep 2
    fi
    
    # 启动新的端口转发
    nohup microk8s kubectl port-forward --address 0.0.0.0 service/right-backend-service 8080:8080 -n "$NAMESPACE" > /tmp/k8s-port-forward.log 2>&1 &
    
    echo -e "${GREEN}端口转发已设置${NC}"
    echo -e "${BLUE}日志文件: /tmp/k8s-port-forward.log${NC}"
}

# 显示状态
show_status() {
    echo -e "${BLUE}=== 部署状态 ===${NC}"
    
    echo -e "${YELLOW}Pods 状态:${NC}"
    microk8s kubectl get pods -n "$NAMESPACE"
    
    echo -e "\n${YELLOW}Services 状态:${NC}"
    microk8s kubectl get services -n "$NAMESPACE"
    
    echo -e "\n${YELLOW}Ingress 状态:${NC}"
    microk8s kubectl get ingress -n "$NAMESPACE"
    
    echo -e "\n${YELLOW}端口转发进程:${NC}"
    pgrep -f "kubectl.*port-forward.*8080:8080" || echo "无端口转发进程运行"
    
    echo -e "\n${GREEN}=== 部署完成 ===${NC}"
    echo -e "${BLUE}API 可通过以下方式访问:${NC}"
    echo -e "  - 直接访问: http://localhost:8080"
    echo -e "  - Cloudflare: https://prod-right-api.mr-chi-tech.com"
    echo -e "  - NodePort: http://localhost:30080"
}

# 主函数
main() {
    echo -e "${GREEN}=== MicroK8s Right-Backend 部署脚本 ===${NC}"
    echo -e "${BLUE}开始部署具有 5 个副本的负载均衡集群...${NC}"
    
    check_dependencies
    enable_addons
    build_and_push_image
    create_secrets
    deploy_k8s_resources
    wait_for_deployment
    setup_port_forward
    
    sleep 5  # 等待端口转发稳定
    show_status
    
    echo -e "\n${GREEN}部署成功! 🎉${NC}"
}

# 处理命令行参数
case "${1:-deploy}" in
    "deploy")
        main
        ;;
    "status")
        show_status
        ;;
    "logs")
        echo -e "${BLUE}显示应用日志...${NC}"
        microk8s kubectl logs -l app=right-backend -n "$NAMESPACE" --tail=100 -f
        ;;
    "restart")
        echo -e "${BLUE}重启部署...${NC}"
        microk8s kubectl rollout restart deployment/right-backend-deployment -n "$NAMESPACE"
        wait_for_deployment
        ;;
    "scale")
        REPLICAS=${2:-5}
        echo -e "${BLUE}扩缩容到 $REPLICAS 个副本...${NC}"
        microk8s kubectl scale deployment/right-backend-deployment --replicas="$REPLICAS" -n "$NAMESPACE"
        wait_for_deployment
        ;;
    "cleanup")
        echo -e "${YELLOW}清理部署...${NC}"
        bash "$K8S_DIR/cleanup.sh"
        ;;
    *)
        echo "用法: $0 [deploy|status|logs|restart|scale|cleanup]"
        echo "  deploy  - 部署应用 (默认)"
        echo "  status  - 显示状态"
        echo "  logs    - 显示日志"
        echo "  restart - 重启部署"
        echo "  scale   - 扩缩容 (用法: $0 scale 3)"
        echo "  cleanup - 清理部署"
        exit 1
        ;;
esac