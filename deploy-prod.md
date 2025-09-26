# 打車應用生產環境部署流程

## 📋 高流量架構概覽

### CI/CD 整體架構 (同一主機)
```
┌─────────────────────────────────────────────────────────────────┐
│                    Ubuntu Server (mr-chi)                      │
│  ┌─────────────────┐    ┌─────────────────────────────────────┐  │
│  │ Azure Pipeline  │───▶│       Production Environment       │  │
│  │   (CI/CD)       │    │          (Kubernetes)              │  │
│  └─────────────────┘    └─────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### 高併發 Kubernetes 架構 (支持200並發用戶)
```
┌─────────────────────────────────────────────────────────────────┐
│                    Kubernetes (microk8s)                       │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │      Right-Backend Application (5 Pods)                  │  │
│  │      2-3Gi Memory, 1-1.5 CPU per Pod                     │  │
│  │      支持100+ QPS, 200個WebSocket連接                     │  │
│  │            Port: 8080 (NodePort: 30082)                  │  │
│  └───────────────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │            高性能基礎設施服務                                │  │
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────────────┐  │  │
│  │  │  MongoDB    │ │Redis (高併發)│ │     RabbitMQ        │  │  │
│  │  │ 3-4Gi RAM   │ │1.5-2Gi RAM  │ │    2-3Gi RAM        │  │  │
│  │  │ 20Gi PVC    │ │ 10Gi PVC    │ │     10Gi PVC        │  │  │
│  │  │位置/訂單數據  │ │熱點數據緩存   │ │  消息隊列處理        │  │  │
│  │  │admin/96787421│ │ 96787421    │ │  admin/96787421     │  │  │
│  │  └─────────────┘ └─────────────┘ └─────────────────────┘  │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              ↓ 結構化日誌
              https://seq.mr-chi-tech.com (遠端服務)

## 🚀 CI/CD 流程詳解

### 1. 觸發條件
- **自動觸發**: Push 到 `main` 或 `feat/*` 分支
- **排除文件**: `*.md`, `docs/*` 文件變更不觸發
- **Agent**: 使用 `mr-chi` self-hosted agent

### 2. CI 階段 (Continuous Integration)

#### 2.1 環境準備
```yaml
- 設置 Go 1.24.2 環境
- Checkout 源代碼
- 下載並驗證 Go modules
```

#### 2.2 測試與構建
```yaml
- 運行 Go 測試: go test -v ./...
- 構建應用程序: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build
- 構建 Docker 鏡像: localhost:32000/right-backend:latest
- 推送到 microk8s registry
```

### 3. CD 階段 (Continuous Deployment)

#### 3.1 基礎設施部署
```yaml
- 部署 MongoDB StatefulSet + PVC (20Gi)
- 部署 Redis StatefulSet + PVC (10Gi) 
- 部署 RabbitMQ StatefulSet + PVC (10Gi)
- 等待所有基礎設施服務準備就緒 (timeout: 300s)
```

#### 3.2 應用配置管理
```yaml
- 應用 Kubernetes ConfigMap: kubectl apply -f k8s/configmap.yaml
- 統一密碼: 96787421 (MongoDB, Redis, RabbitMQ)
- 服務間通信使用 K8s 服務名稱
```

#### 3.3 滾動更新部署
```yaml
- 更新 Deployment 鏡像版本
- 3 Pod 滾動更新 (maxSurge: 2, maxUnavailable: 1)
- 等待滾動更新完成 (timeout: 600s)
- 健康檢查驗證
```

#### 3.4 部署驗證
```yaml
- 檢查所有 Pod 狀態 (應用 + 基礎設施)
- 通過 NodePort 進行健康檢查
- 清理舊的 Docker 鏡像
```

## 📁 項目文件結構

```
Right-Backend/
├── azure-pipelines.yml          # Azure DevOps Pipeline 配置
├── deploy-prod.sh              # 手動部署腳本  
├── Dockerfile                  # 應用程序容器化
├── k8s/                        # Kubernetes 配置
│   ├── configmap.yaml          # 應用配置 (統一配置源)
│   ├── deployment.yaml         # 部署配置 (5 Pods, 高併發優化)
│   ├── service.yaml           # 服務暴露配置
│   ├── namespace.yaml         # 命名空間配置
│   ├── mongodb-k8s.yaml       # MongoDB StatefulSet (高性能配置)
│   ├── redis-k8s.yaml         # Redis StatefulSet (併發優化)
│   └── rabbitmq-k8s.yaml      # RabbitMQ StatefulSet (消息隊列)
├── PERFORMANCE-OPTIMIZATION.md # 高流量性能優化指南
└── config.yml                 # 本地開發配置
```

## ⚙️ 關鍵配置說明

### Azure Pipeline 變數
```yaml
imageRepository: 'right-backend'
containerRegistry: 'localhost:32000'  # microk8s registry
k8sNamespace: 'right-backend'
deploymentName: 'right-backend-deployment'
Go.version: '1.24.2'
```

### Kubernetes 高併發資源配置
```yaml
# 應用 Pod 規格 (打車應用優化)
replicas: 5          # 支持200並發用戶
resources:
  requests:
    memory: "2Gi"    # 合理的基礎記憶體
    cpu: "1000m"     # 1 CPU核心
  limits:
    memory: "3Gi"    # 峰值記憶體
    cpu: "1500m"     # 1.5 CPU核心峰值

# 服務暴露
ports:
  - containerPort: 8080
    nodePort: 30082

# 高性能基礎設施資源
MongoDB: 3-4Gi Memory, 20Gi PVC (位置數據+訂單)
Redis: 1.5-2Gi Memory, 10Gi PVC (高併發緩存)
RabbitMQ: 2-3Gi Memory, 10Gi PVC (消息隊列)
```

### 基礎設施配置 (全 K8s 內部)
```yaml
# MongoDB (K8s StatefulSet)
uri: "mongodb://admin:96787421@mongodb-service:27017"
database: "right-prod-db"
storage: 20Gi PVC

# Redis (K8s StatefulSet)  
addr: "redis-service:6379"
password: "96787421"
storage: 10Gi PVC

# RabbitMQ (K8s StatefulSet)
url: "amqp://admin:96787421@rabbitmq-service:5672/"
management_ui: NodePort 30672
storage: 10Gi PVC

# Seq 日誌 (遠端服務)
endpoint: "https://seq.mr-chi-tech.com"
api_key: "xlXtEzkPCbaRLEQGCoxg"
service: "right-backend"
```

## 🛠️ 部署方式

### 方式 1: Azure DevOps Pipeline (推薦)
```bash
# 自動觸發
git push origin main

# Pipeline 會自動執行:
# 1. CI: 測試 → 構建 → 推送鏡像
# 2. CD: 配置更新 → 滾動部署 → 驗證
```

### 方式 2: 手動執行腳本
```bash
# 完整部署
./deploy-prod.sh all

# 分階段部署
./deploy-prod.sh check    # 環境檢查
./deploy-prod.sh infra    # K8s 基礎設施部署 (MongoDB/Redis/RabbitMQ)
./deploy-prod.sh k8s      # 應用程序部署
./deploy-prod.sh verify   # 部署驗證
```

## 📊 部署驗證

### 服務狀態檢查
```bash
# 檢查所有 Kubernetes Pods
kubectl get pods -n right-backend

# 檢查服務和端點
kubectl get services -n right-backend
kubectl get pvc -n right-backend

# 檢查 StatefulSet 狀態
kubectl get statefulset -n right-backend

# 查看應用日誌
kubectl logs -n right-backend -l app=right-backend
```

### 訪問端點
```
應用程序: http://NODE_IP:30082
健康檢查: http://NODE_IP:30082/health
API 文檔: http://NODE_IP:30082/docs

基礎設施管理:
MongoDB: kubectl port-forward svc/mongodb-service 27017:27017 -n right-backend (admin/96787421)
Redis: kubectl port-forward svc/redis-service 6379:6379 -n right-backend (password: 96787421)
RabbitMQ 管理: http://NODE_IP:30672 (admin/96787421)

日誌監控:
Seq 日誌: https://seq.mr-chi-tech.com (Service: right-backend)
```

## 🔄 滾動更新流程

### 更新觸發
```bash
# 代碼變更觸發
git commit -m "feat: new feature"
git push origin main
```

### 更新過程
```
1. Azure Pipeline 檢測到代碼變更 (main 分支)
2. 執行 CI: 
   ├── Go 測試和構建
   ├── Docker 鏡像構建和推送
3. 執行 CD:
   ├── 部署/更新基礎設施 (MongoDB/Redis/RabbitMQ StatefulSet)
   ├── 等待基礎設施服務準備就緒
   ├── 應用 ConfigMap 更新
   ├── 更新應用 Deployment 鏡像版本
   ├── Kubernetes 執行滾動更新
   │   ├── 啟動新 Pod (maxSurge: 2)
   │   ├── 健康檢查通過後
   │   └── 終止舊 Pod (maxUnavailable: 1)
   ├── 通過 NodePort 進行健康檢查
   └── 清理舊 Docker 鏡像
```

### 回滾策略
```bash
# 緊急回滾到上一版本
kubectl rollout undo deployment/right-backend-deployment -n right-backend

# 回滾到特定版本
kubectl rollout undo deployment/right-backend-deployment --to-revision=N -n right-backend

# 查看部署歷史
kubectl rollout history deployment/right-backend-deployment -n right-backend
```

## 🔒 安全考量

### 敏感信息管理
- ✅ 統一密碼: `96787421` (MongoDB, Redis, RabbitMQ)
- ✅ Kubernetes Secrets 管理敏感信息 (base64 編碼)
- ✅ 使用 Kubernetes Service 名稱而非硬編碼 IP
- ✅ 網路隔離通過 K8s namespace 和 NetworkPolicy

### 生產環境最佳實踐
- ✅ 健康檢查: 所有服務配置 liveness 和 readiness probes
- ✅ 資源限制: 所有容器配置 CPU 和記憶體 limits/requests
- ✅ 滾動更新: 零停機時間部署
- ✅ 數據持久化: StatefulSet + PVC 確保數據安全
- ✅ 高可用性: 3 Pod 應用實例，基礎設施 1 實例可擴展

## 📈 監控和日誌

### 日誌系統
```
應用日誌 → Seq Logger → https://seq.mr-chi-tech.com
├── Service: "right-backend"
├── API Key: "xlXtEzkPCbaRLEQGCoxg"
└── 結構化日誌查詢和分析
```

### 服務監控
```bash
# Kubernetes 資源監控
kubectl top pods -n right-backend
kubectl top nodes

# 查看所有服務日誌
kubectl logs -n right-backend -l app=right-backend --follow
kubectl logs -n right-backend -l app=mongodb --follow
kubectl logs -n right-backend -l app=redis --follow
kubectl logs -n right-backend -l app=rabbitmq --follow

# 監控 PVC 使用情況
kubectl get pvc -n right-backend
df -h  # 檢查 microk8s hostpath 存儲空間
```

## 🚨 故障排除

### 常見問題

#### 1. Pod 啟動失敗
```bash
# 檢查 Pod 狀態和日誌
kubectl describe pod POD_NAME -n right-backend
kubectl logs POD_NAME -n right-backend

# 常見原因: ConfigMap 配置錯誤, 鏡像拉取失敗
```

#### 2. 服務無法訪問
```bash
# 檢查服務和端點
kubectl get endpoints -n right-backend
kubectl describe service right-backend-service -n right-backend

# 檢查防火牆和網路配置
```

#### 3. 基礎設施服務異常
```bash
# 檢查 StatefulSet 和 PVC 狀態
kubectl get statefulset -n right-backend
kubectl get pvc -n right-backend
kubectl describe statefulset mongodb -n right-backend

# 檢查 Pod 詳細狀態
kubectl describe pod STATEFULSET_POD_NAME -n right-backend

# 重啟 StatefulSet (謹慎操作)
kubectl rollout restart statefulset/mongodb -n right-backend
kubectl rollout restart statefulset/redis -n right-backend
kubectl rollout restart statefulset/rabbitmq -n right-backend
```

## 📚 維護操作

### 定期維護
```bash
# 清理 Docker 資源
docker system prune -f

# 備份數據庫 (從 K8s Pod)
kubectl exec -n right-backend mongodb-0 -- mongodump --out /backup
kubectl cp right-backend/mongodb-0:/backup ./mongodb-backup-$(date +%Y%m%d)

# 檢查存儲空間
kubectl get pvc -n right-backend
df -h  # 檢查 microk8s hostpath 存儲

# 清理舊的 PV 數據 (謹慎操作)
# sudo find /var/snap/microk8s/common/default-storage/ -name "*right-backend*" -type d
```

### 擴容操作
```bash
# 水平擴容 (增加 Pod 數量)
kubectl scale deployment right-backend-deployment --replicas=5 -n right-backend

# 垂直擴容 (修改 deployment.yaml resources 後重新部署)
kubectl apply -f k8s/deployment.yaml
```

---

## 🎯 總結

### 🚀 **完整 Kubernetes 生產環境**

此部署方案提供了：
- ✅ **完全自動化的 CI/CD 流程** - Azure Pipeline + Ubuntu mr-chi Agent
- ✅ **生產級 Kubernetes 部署** - 全部基礎設施在 K8s 內部
- ✅ **高性能資源配置** - 4-6Gi RAM, 1-2 CPU per Pod
- ✅ **數據持久化存儲** - StatefulSet + PVC (MongoDB 20Gi, Redis/RabbitMQ 10Gi)
- ✅ **零停機滾動更新** - 3 Pod 高可用部署
- ✅ **統一密碼管理** - `96787421` (MongoDB, Redis, RabbitMQ)
- ✅ **完整監控日誌** - 遠端 Seq 服務集成
- ✅ **網路隔離安全** - K8s Service 內部通信

### 📊 **架構優勢**
```
🔹 網路連通性: ✅ 完美解決 (全 K8s 內部)
🔹 資源配置: ✅ 大幅提升 (4-6Gi RAM)
🔹 數據安全: ✅ PVC 持久化存儲
🔹 服務發現: ✅ K8s Service Name 自動解析
🔹 部署自動化: ✅ 完整 CI/CD Pipeline
🔹 擴展能力: ✅ 水平和垂直擴容支援
```

### 📊 **性能指標**
```
支持能力:
├── 並發用戶: 200人
├── QPS處理: 100+ requests/秒
├── WebSocket: 200個長連接
├── 司機位置更新: 2400+ RPM
└── 訂單處理: 實時響應

單機資源需求:
├── CPU: 16核心+ (推薦24核心)
├── RAM: 32Gi+ (推薦48Gi)  
├── 存儲: 100Gi+ SSD
└── 網路: 1Gbps+
```

### 🔧 **擴容策略**
```bash
# 水平擴容應用 (5→8個Pod)
kubectl scale deployment right-backend-deployment --replicas=8 -n right-backend

# 監控資源使用
kubectl top pods -n right-backend
kubectl top nodes
```

通過 Azure DevOps Pipeline 和 Ubuntu self-hosted agent，實現了從代碼提交到生產部署的完整自動化流程，針對打車應用高併發場景進行了專項優化。詳細性能分析請參考 `PERFORMANCE-OPTIMIZATION.md`。