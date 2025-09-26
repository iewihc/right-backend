# Huma v2 與 OpenTelemetry 整合指南

## 概述

這個文檔說明如何在 Huma v2 框架中整合最新的 OpenTelemetry 2025 最佳實踐，實現完整的觀測性堆疊。

## 架構概述

```
Huma v2 Application 
    ↓ (OTLP)
OpenTelemetry Collector 
    ↓ (traces) → Jaeger
    ↓ (metrics) → Prometheus
    ↓ (logs) → Console/Loki (可選)
    ↓ (visualization) → Grafana
```

## 功能特性

### ✅ 已實現功能

1. **分散式追蹤 (Traces)**
   - 自動生成 HTTP 請求的 spans
   - Trace context 傳播
   - 詳細的請求屬性記錄
   - 錯誤狀態追蹤

2. **指標收集 (Metrics)**
   - HTTP 請求計數器
   - 請求持續時間直方圖
   - 活躍請求計數器
   - 自定義業務指標支援

3. **日誌關聯 (Logs)**
   - 與現有 zerolog 整合
   - Trace ID 和 Span ID 關聯
   - 結構化日誌記錄

4. **生產準備**
   - 開發/生產環境配置分離
   - 批次處理和採樣配置
   - 記憶體限制和效能優化

## 快速開始

### 1. 依賴項安裝

```bash
go mod tidy
```

所需的依賴項已在 `go.mod` 中配置：

```go
go.opentelemetry.io/otel v1.38.0
go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v1.38.0
go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.38.0
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.38.0
go.opentelemetry.io/otel/exporters/stdout/stdoutlog v1.38.0
go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.38.0
go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.38.0
go.opentelemetry.io/otel/log v1.38.0
go.opentelemetry.io/otel/metric v1.38.0
go.opentelemetry.io/otel/sdk v1.38.0
go.opentelemetry.io/otel/sdk/log v1.38.0
go.opentelemetry.io/otel/sdk/metric v1.38.0
go.opentelemetry.io/otel/trace v1.38.0
go.opentelemetry.io/contrib/bridges/otelslog v1.38.0
```

### 2. 啟動觀測性基礎設施

```bash
# 啟動 Jaeger, Prometheus, Grafana, OpenTelemetry Collector
docker-compose -f docker-compose-otel.yaml up -d
```

### 3. 啟動應用程式

```bash
# 開發模式（使用 stdout exporters）
go run main.go --port 8080

# 生產模式（需要在 main.go 中設置 DevelopmentMode: false）
go run main.go --port 8080
```

### 4. 驗證設置

檢查各個服務是否正常運行：

- **Jaeger UI**: http://localhost:16686
- **Prometheus**: http://localhost:9090
- **Grafana**: http://localhost:3000 (admin/admin)
- **OpenTelemetry Collector Health**: http://localhost:13133

## 配置選項

### OpenTelemetry 配置

在 `main.go` 中的配置：

```go
otelConfig := otelMiddleware.OtelConfig{
    ServiceName:     "right-backend",
    ServiceVersion:  "1.0.0",
    Environment:     "development",
    OTLPEndpoint:    "http://localhost:4318", // OTLP HTTP endpoint
    JaegerEndpoint:  "http://localhost:4318", // 向後兼容
    PrometheusPort:  "8888",
    Enabled:         true,
    TracesEnabled:   true,  // 啟用分散式追蹤
    MetricsEnabled:  true,  // 啟用指標收集
    LogsEnabled:     false, // 暫時停用 logs（使用現有的 zerolog）
    DevelopmentMode: true,  // 開發模式：使用 stdout，生產模式：使用 OTLP
}
```

### 環境配置

**開發環境** (`DevelopmentMode: true`):
- Traces, Metrics, Logs 輸出到 stdout
- 方便本地調試和開發

**生產環境** (`DevelopmentMode: false`):
- 透過 OTLP 協議發送到 OpenTelemetry Collector
- 批次處理和優化的效能配置

## 使用方式

### 1. 自動追蹤

所有 HTTP 請求都會自動生成 traces，包含：

- 請求方法和路徑
- 響應狀態碼
- 請求持續時間
- 用戶代理和來源 IP
- Trace ID 和 Span ID

### 2. 手動添加 Spans

在業務邏輯中添加自定義 spans：

```go
import "go.opentelemetry.io/otel"

func businessLogic(ctx context.Context) error {
    tracer := otel.Tracer("right-backend")
    _, span := tracer.Start(ctx, "business-operation")
    defer span.End()
    
    span.SetAttributes(
        attribute.String("operation.type", "data-processing"),
        attribute.Int("records.count", 100),
    )
    
    // 業務邏輯...
    
    return nil
}
```

### 3. 添加自定義指標

```go
import "go.opentelemetry.io/otel/metric"

// 創建自定義指標
var orderCounter metric.Int64Counter

func init() {
    meter := otel.Meter("right-backend")
    orderCounter, _ = meter.Int64Counter(
        "orders_processed_total",
        metric.WithDescription("Total number of orders processed"),
    )
}

func processOrder(ctx context.Context) {
    // 處理訂單...
    
    // 記錄指標
    orderCounter.Add(ctx, 1, metric.WithAttributes(
        attribute.String("order.type", "taxi"),
        attribute.String("status", "completed"),
    ))
}
```

### 4. 關聯日誌

日誌會自動包含 trace 資訊：

```go
func handler(ctx huma.Context) {
    logger.Info().
        Str("trace_id", middleware.GetTraceIDFromContext(ctx)).
        Str("span_id", middleware.GetSpanIDFromContext(ctx)).
        Msg("Processing request")
}
```

## 監控和查詢

### Jaeger 查詢

1. 訪問 Jaeger UI: http://localhost:16686
2. 選擇服務 "right-backend"
3. 查看 traces 和性能分析

常用查詢：
- 查找錯誤請求: `error=true`
- 查找慢請求: `duration > 1s`
- 按操作類型過濾: `operation="GET /api/orders"`

### Prometheus 查詢

1. 訪問 Prometheus: http://localhost:9090
2. 使用 PromQL 查詢指標

常用查詢：
```promql
# HTTP 請求速率
rate(http_requests_total[5m])

# 平均響應時間
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))

# 錯誤率
rate(http_requests_total{status_code=~"5.."}[5m]) / rate(http_requests_total[5m])

# 活躍請求數
http_requests_active
```

### Grafana 儀表板

1. 訪問 Grafana: http://localhost:3000
2. 添加 Prometheus 資料源: http://prometheus:9090
3. 添加 Jaeger 資料源: http://jaeger:16686

推薦的儀表板指標：
- HTTP 請求速率和錯誤率
- 響應時間分布
- 系統資源使用率
- 業務指標趨勢

## 效能優化

### 採樣配置

生產環境建議調整採樣率：

```go
// 在 setupTraceProvider 中
tp := sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(exporter),
    sdktrace.WithResource(res),
    sdktrace.WithSampler(
        sdktrace.TraceIDRatioBased(0.1), // 10% 採樣率
    ),
)
```

### 批次處理優化

在 `otel-collector-config.yaml` 中調整：

```yaml
processors:
  batch:
    timeout: 1s
    send_batch_size: 1024      # 增加批次大小
    send_batch_max_size: 2048  # 最大批次大小
```

### 記憶體限制

```yaml
processors:
  memory_limiter:
    check_interval: 1s
    limit_mib: 512  # 根據可用記憶體調整
```

## 故障排除

### 常見問題

1. **Traces 沒有出現在 Jaeger 中**
   - 檢查 OpenTelemetry Collector 是否運行
   - 確認 OTLP endpoint 配置正確
   - 查看 collector 日誌: `docker logs otel-collector`

2. **Metrics 沒有在 Prometheus 中顯示**
   - 檢查 Prometheus 目標狀態: http://localhost:9090/targets
   - 確認 OpenTelemetry Collector metrics endpoint 可訪問
   - 檢查 collector 配置中的 prometheus exporter

3. **高記憶體使用**
   - 調整 memory_limiter 設置
   - 減少採樣率
   - 增加批次處理間隔

### 日誌檢查

```bash
# 檢查各服務日誌
docker logs otel-collector
docker logs jaeger-tracing
docker logs prometheus-metrics
docker logs grafana-dashboard

# 檢查應用程式 traces（開發模式）
go run main.go  # traces 會輸出到 stdout
```

## 最佳實踐

### 2025 OpenTelemetry 最佳實踐

1. **使用語義約定 (Semantic Conventions)**
   - 使用標準化的屬性名稱
   - 遵循 OpenTelemetry 規範

2. **合理的採樣策略**
   - 開發環境: 100% 採樣
   - 測試環境: 50% 採樣
   - 生產環境: 1-10% 採樣

3. **指標命名約定**
   - 使用描述性名稱
   - 包含單位信息
   - 避免高基數標籤

4. **錯誤處理**
   - 記錄所有錯誤到 spans
   - 使用適當的狀態碼
   - 添加錯誤詳細信息

5. **安全考慮**
   - 不要記錄敏感資訊
   - 過濾 PII 資料
   - 使用安全的傳輸協議（生產環境使用 TLS）

## 部署注意事項

### 生產環境配置

1. 設置 `DevelopmentMode: false`
2. 配置適當的採樣率
3. 啟用 TLS 加密
4. 設置適當的資源限制
5. 配置持久化存儲

### 擴展性考慮

- OpenTelemetry Collector 可以水平擴展
- Jaeger 支持分片和集群部署
- Prometheus 可以配置聯邦和遠程存儲

## 總結

這個整合方案提供了：

- ✅ 完整的分散式追蹤
- ✅ 自動化的指標收集
- ✅ 結構化的日誌關聯
- ✅ 生產就緒的配置
- ✅ 2025 年最新的 OpenTelemetry 最佳實踐

通過這個設置，您可以獲得對 Huma v2 應用程式的完整可觀測性，包括性能監控、錯誤追蹤和業務指標分析。