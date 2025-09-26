#!/bin/bash

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Podman 与 MicroK8s 集成设置 ===${NC}"

# 检查 Podman 是否安装
check_podman() {
    if ! command -v podman &> /dev/null; then
        echo -e "${RED}错误: Podman 未安装${NC}"
        echo -e "${YELLOW}请先安装 Podman:${NC}"
        echo "  sudo apt update"
        echo "  sudo apt install podman"
        exit 1
    fi
    echo -e "${GREEN}Podman 已安装${NC}"
}

# 配置 Podman 与 MicroK8s 集成
configure_podman_microk8s() {
    echo -e "${BLUE}配置 Podman 与 MicroK8s 集成...${NC}"
    
    # 配置 Podman 使用 MicroK8s 的 registry
    echo -e "${YELLOW}配置 Podman registries...${NC}"
    
    mkdir -p ~/.config/containers
    
    cat > ~/.config/containers/registries.conf << EOF
unqualified-search-registries = ["docker.io"]

[[registry]]
location = "localhost:32000"
insecure = true
protocol = "http"

[[registry]]
location = "docker.io"
EOF
    
    echo -e "${GREEN}Podman registries 配置完成${NC}"
}

# 测试 Podman 与 MicroK8s registry 连接
test_registry_connection() {
    echo -e "${BLUE}测试 MicroK8s registry 连接...${NC}"
    
    # 确保 MicroK8s registry 正在运行
    if ! microk8s status | grep -q "registry: enabled"; then
        echo -e "${YELLOW}启用 MicroK8s registry...${NC}"
        microk8s enable registry
        sleep 10
    fi
    
    # 测试推送一个简单镜像
    echo -e "${YELLOW}测试镜像推送...${NC}"
    podman pull hello-world:latest
    podman tag hello-world:latest localhost:32000/hello-world:test
    
    if podman push localhost:32000/hello-world:test; then
        echo -e "${GREEN}Registry 连接成功${NC}"
        podman rmi localhost:32000/hello-world:test
    else
        echo -e "${RED}Registry 连接失败${NC}"
        exit 1
    fi
}

# 创建 Podman 构建脚本
create_podman_build_script() {
    echo -e "${BLUE}创建 Podman 构建脚本...${NC}"
    
    cat > /home/mr-chi/prod/right/right-backend/build-with-podman.sh << 'EOF'
#!/bin/bash

set -e

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

IMAGE_NAME="localhost:32000/right-backend:latest"
BUILD_DIR="/home/mr-chi/prod/right/right-backend"

echo -e "${BLUE}使用 Podman 构建 Right-Backend 镜像...${NC}"

cd "$BUILD_DIR"

# 构建镜像
podman build -t "$IMAGE_NAME" .

# 推送到 MicroK8s registry
podman push "$IMAGE_NAME"

echo -e "${GREEN}镜像构建和推送完成: $IMAGE_NAME${NC}"
EOF
    
    chmod +x /home/mr-chi/prod/right/right-backend/build-with-podman.sh
    echo -e "${GREEN}Podman 构建脚本已创建${NC}"
}

# 显示 Podman 相关命令
show_podman_commands() {
    echo -e "${BLUE}=== Podman 常用命令 ===${NC}"
    echo -e "${YELLOW}构建镜像:${NC}"
    echo "  cd /home/mr-chi/prod/right/right-backend"
    echo "  ./build-with-podman.sh"
    echo
    echo -e "${YELLOW}查看镜像:${NC}"
    echo "  podman images | grep right-backend"
    echo
    echo -e "${YELLOW}查看容器:${NC}"
    echo "  podman ps -a"
    echo
    echo -e "${YELLOW}清理镜像:${NC}"
    echo "  podman rmi localhost:32000/right-backend:latest"
    echo
    echo -e "${YELLOW}检查 registry:${NC}"
    echo "  curl http://localhost:32000/v2/_catalog"
}

# 主函数
main() {
    check_podman
    configure_podman_microk8s
    test_registry_connection
    create_podman_build_script
    show_podman_commands
    
    echo -e "\n${GREEN}=== Podman 集成设置完成 ===${NC}"
    echo -e "${BLUE}现在可以使用 Podman 构建和推送镜像到 MicroK8s registry${NC}"
}

main