# CrawlerService 測試指南

## 概述

為了方便對 `CrawlerService` 進行測試，我們新增了靈活的配置選項，讓您可以：

- 顯示瀏覽器窗口（便於調試）
- 跳過頁面預熱（加快測試啟動）
- 使用單一瀏覽器實例（避免資源競爭）
- 禁用快取（確保測試結果準確）
- **測試完成後手動關閉瀏覽器**（便於觀察結果）

## 快速開始

### 方法一：使用測試專用構造函數（推薦）

```go
// 創建適合測試的服務實例
service, err := NewCrawlerServiceForTesting(logger, nil)
if err != nil {
    log.Fatal(err)
}
defer service.Close()

// 進行測試
routes, err := service.GetGoogleMapsDirections(ctx, "台北車站", "松山機場")
```

**特點：**
- ✅ 顯示瀏覽器（便於觀察）
- ✅ 跳過預熱（立即可用）  
- ✅ 單一頁面池（避免競爭）
- ✅ 禁用快取（純淨測試）

### 方法二：使用自定義配置

```go
config := CrawlerConfig{
    IsDebug:        true,  // 顯示瀏覽器
    SkipWarmup:     true,  // 跳過預熱
    PoolSize:       1,     // 頁面池大小
    DisableCache:   true,  // 禁用快取
}

service, err := NewCrawlerServiceWithConfig(logger, nil, config)
```

## 配置選項詳解

| 選項 | 說明 | 生產環境 | 測試環境 |
|------|------|----------|----------|
| `IsDebug` | 顯示瀏覽器窗口 | `false` | `true` |
| `SkipWarmup` | 跳過頁面預熱 | `false` | `true` |
| `PoolSize` | 頁面池大小 | `5` | `1` |
| `DisableCache` | 禁用 Redis 快取 | `false` | `true` |

## 測試範例

### 基本測試

```go
func TestCrawlerBasic(t *testing.T) {
    service, err := NewCrawlerServiceForTesting(logger, nil)
    require.NoError(t, err)
    defer service.Close()

    routes, err := service.GetGoogleMapsDirections(ctx, "起點", "終點")
    require.NoError(t, err)
    assert.NotEmpty(t, routes)
}
```

### 批量測試

```go
func TestMultipleRoutes(t *testing.T) {
    service, err := NewCrawlerServiceForTesting(logger, nil)
    require.NoError(t, err)
    defer service.Close()

    testCases := []struct{
        start, end string
    }{
        {"台北車站", "松山機場"},
        {"信義區", "大安區"},
        {"板橋", "淡水"},
    }

    for _, tc := range testCases {
        routes, err := service.GetAllDirections(ctx, tc.start, tc.end)
        assert.NoError(t, err)
        assert.NotEmpty(t, routes)
    }
}
```

## 注意事項

1. **測試環境變數**：確保測試時不會影響生產環境的快取
2. **瀏覽器資源**：測試後記得呼叫 `service.Close()` 釋放資源
3. **網路依賴**：這些是真實的網路請求，需要穩定的網路連線
4. **執行時間**：即使跳過預熱，首次頁面載入仍需要一些時間

## 完整測試套件

現在包含以下測試案例：

1. **TestGetGoogleMapsDirections** - 基本路線查詢測試
   - 台北到松山機場
   - 信義到大安
   - 板橋到淡水

2. **TestGetAllDirections** - 多路線查詢測試
   - 台北到桃園機場
   - 西門町到台北101
   - 中山到士林夜市

3. **TestDirectionsMatrixInverse** - 司機到客戶路線矩陣測試
   - 測試多個司機位置到單一客戶上車點

4. **TestCrawlerStressTest** - 壓力測試（可選）
   - 連續多次查詢測試瀏覽器穩定性

## 執行測試

```bash
# 執行所有爬蟲測試（瀏覽器會保持開啟，測試完成後需手動按 Enter 關閉）
go test -v ./service -run TestCrawler

# 執行單一測試
go test -v ./service -run TestGetGoogleMapsDirections

# 執行壓力測試
go test -v ./service -run TestCrawlerStressTest

# 跳過壓力測試的快速模式
go test -short -v ./service -run TestCrawler
```

### 測試流程說明

1. **啟動階段**：`TestMain` 建立單一瀏覽器實例，顯示在螢幕上
2. **測試執行**：所有測試共用這個瀏覽器實例，避免重複開啟關閉
3. **觀察階段**：測試完成後，瀏覽器保持開啟狀態，可觀察最後的頁面狀態
4. **手動關閉**：按 Enter 鍵後程式結束並關閉瀏覽器

## 故障排除

### 瀏覽器啟動失敗
確保系統已安裝 Playwright 瀏覽器：
```bash
npx playwright install chromium
```

### 測試超時
增加 context timeout：
```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
```

### 頁面載入失敗
檢查網路連線和 Google Maps 可用性。