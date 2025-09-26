# Right-Backend MicroK8s 部署指南

这个目录包含了用于在 MicroK8s 上部署 Right-Backend 应用的所有 Kubernetes 配置文件和管理脚本。

## 🚀 快速开始

### 1. 环境准备

确保已安装以下软件：
- MicroK8s
- Docker 或 Podman
- kubectl (通过 MicroK8s 提供)

### 2. 一键部署

```bash
cd /home/mr-chi/prod/right/right-backend/k8s
chmod +x *.sh
./deploy.sh
```

这将：
- 启用必要的 MicroK8s 插件
- 构建和推送应用镜像
- 部署 5 个应用副本
- 配置负载均衡
- 设置端口转发到 localhost:8080

## 📁 文件结构

```
k8s/
├── namespace.yaml          # Kubernetes 命名空间
├── configmap.yaml         # 应用配置
├── secret.yaml            # 密钥配置 (Google Services)
├── deployment.yaml        # 应用部署 (5个副本)
├── service.yaml           # 服务和负载均衡配置
├── ingress.yaml           # Ingress 配置
├── deploy.sh              # 主部署脚本
├── cleanup.sh             # 清理脚本
├── podman-setup.sh        # Podman 集成设置
└── README.md              # 本文件
```

## 🔧 部署架构

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Cloudflare    │    │   localhost     │    │   MicroK8s      │
│     Tunnel      │────│     :8080       │────│    Cluster      │
│                 │    │                 │    │                 │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                        │
                                                        ▼
                                              ┌─────────────────┐
                                              │  LoadBalancer   │
                                              │    Service      │
                                              └─────────────────┘
                                                        │
                                    ┌───────┬───────┬──┴──┬───────┬───────┐
                                    ▼       ▼       ▼     ▼       ▼
                                  Pod-1   Pod-2   Pod-3  Pod-4   Pod-5
                                 (8080)  (8080)  (8080) (8080)  (8080)
```

## 🛠️ 管理命令

### 部署相关

```bash
# 完整部署
./deploy.sh deploy

# 查看状态
./deploy.sh status

# 查看日志
./deploy.sh logs

# 重启应用
./deploy.sh restart

# 扩缩容 (例如扩展到 8 个副本)
./deploy.sh scale 8

# 清理部署
./deploy.sh cleanup
```

### Podman 集成

如果您使用 Podman 而不是 Docker：

```bash
# 设置 Podman 集成
./podman-setup.sh

# 使用 Podman 构建镜像
cd /home/mr-chi/prod/right/right-backend
./build-with-podman.sh
```

### Kubernetes 原生命令

```bash
# 查看 Pods
microk8s kubectl get pods -n right-backend

# 查看 Services
microk8s kubectl get services -n right-backend

# 查看应用日志
microk8s kubectl logs -l app=right-backend -n right-backend

# 进入 Pod 调试
microk8s kubectl exec -it <pod-name> -n right-backend -- /bin/bash

# 手动端口转发
microk8s kubectl port-forward service/right-backend-service 8080:8080 -n right-backend
```

## 🌐 访问方式

部署完成后，应用可通过以下方式访问：

1. **直接访问**: http://localhost:8080
2. **Cloudflare 隧道**: https://prod-right-api.mr-chi-tech.com
3. **NodePort**: http://localhost:30082

## ⚙️ 配置说明

### 环境变量

应用使用以下环境变量（在 `deployment.yaml` 中配置）：

- `PORT`: 应用监听端口 (8080)
- `MONGO_URI`: MongoDB 连接地址
- `REDIS_ADDR`: Redis 连接地址
- `RABBITMQ_URL`: RabbitMQ 连接地址

### 资源限制

每个 Pod 的资源配置：

```yaml
resources:
  requests:
    memory: "256Mi"
    cpu: "250m"
  limits:
    memory: "512Mi"
    cpu: "500m"
```

### 健康检查

- **存活探针**: `/health` 端点
- **就绪探针**: `/ready` 端点

## 🔄 负载均衡

系统使用两种负载均衡机制：

1. **Kubernetes Service**: 在 5 个 Pod 之间分发流量
2. **会话亲和性**: 设置为 `None`，确保请求均匀分布

## 📊 监控和日志

### 查看实时日志

```bash
# 所有 Pod 的日志
microk8s kubectl logs -l app=right-backend -n right-backend -f

# 特定 Pod 的日志
microk8s kubectl logs <pod-name> -n right-backend -f

# 端口转发日志
tail -f /tmp/k8s-port-forward.log
```

### 监控 Pod 状态

```bash
# 实时监控 Pod 状态
watch microk8s kubectl get pods -n right-backend

# 查看 Pod 详细信息
microk8s kubectl describe pod <pod-name> -n right-backend
```

## 🚨 故障排除

### 常见问题

1. **镜像拉取失败**
   ```bash
   # 检查镜像是否存在
   docker images | grep right-backend
   # 或
   podman images | grep right-backend
   ```

2. **端口转发失败**
   ```bash
   # 检查端口是否被占用
   lsof -i :8080
   
   # 手动重启端口转发
   pkill -f "kubectl.*port-forward"
   ./deploy.sh status
   ```

3. **Pod 无法启动**
   ```bash
   # 查看 Pod 详细信息
   microk8s kubectl describe pod <pod-name> -n right-backend
   
   # 查看事件
   microk8s kubectl get events -n right-backend --sort-by='.lastTimestamp'
   ```

### 重置环境

如果遇到严重问题，可以完全重置：

```bash
# 清理现有部署
./cleanup.sh

# 重新部署
./deploy.sh
```

## 🔒 安全配置

- Google Services 密钥通过 Kubernetes Secret 管理
- 敏感配置通过 ConfigMap 和环境变量注入
- 容器以非特权用户运行

## 📝 版本控制

所有 Kubernetes 配置文件都在版本控制下，确保：

1. 配置变更可追踪
2. 可以回滚到之前的版本
3. 团队协作时配置同步

## 🤝 贡献

如需修改配置：

1. 编辑相应的 YAML 文件
2. 测试配置的有效性
3. 提交到版本控制系统
4. 使用 `./deploy.sh restart` 应用更改

---

**注意**: 确保所有脚本都有执行权限 (`chmod +x *.sh`)，并且 MicroK8s 正常运行。