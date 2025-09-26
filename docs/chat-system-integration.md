# 聊天系統集成指南

## 概述

本指南說明如何將聊天系統集成到現有的計程車調度API系統中。聊天系統基於WebSocket實現，支持司機與客服間的實時通訊。

## 已創建的文件

### 數據模型
- `model/chat.go` - 聊天相關的數據模型和類型定義
- `data-models/websocket/chat_types.go` - 聊天WebSocket消息類型
- `data-models/websocket/types.go` - 更新的WebSocket消息常量

### 服務層
- `service/chat_service.go` - 聊天業務邏輯服務
- `infra/chat_mongodb.go` - MongoDB集合初始化和索引

### 控制器
- `controller/chat_controller.go` - 聊天HTTP API控制器
- `controller/websocket_controller.go` - 更新的WebSocket控制器（已集成聊天功能）

## 集成步驟

### 1. 初始化MongoDB集合

在應用啟動時調用聊天集合初始化：

```go
// 在 main.go 或初始化代碼中
import "right-backend/infra"

func initializeDatabase(logger zerolog.Logger, db *mongo.Database) error {
    // 現有的初始化代碼...
    
    // 初始化聊天集合
    if err := infra.InitializeChatCollections(logger, db); err != nil {
        return fmt.Errorf("初始化聊天集合失敗: %w", err)
    }
    
    return nil
}
```

### 2. 創建並註冊聊天服務

```go
// 在服務初始化代碼中
func setupServices(logger zerolog.Logger, db *mongo.Database) (*service.ChatService, error) {
    // 假設已有的服務
    orderService := service.NewOrderService(logger, db)
    driverService := service.NewDriverService(logger, db)
    
    // 創建聊天服務
    chatService := service.NewChatService(logger, db, orderService, driverService)
    
    return chatService, nil
}
```

### 3. 創建並註冊聊天控制器

```go
// 在控制器初始化代碼中
func setupControllers(logger zerolog.Logger, chatService *service.ChatService, api huma.API) {
    // 創建聊天控制器
    chatController := controller.NewChatController(logger, chatService)
    
    // 註冊HTTP路由
    chatController.RegisterRoutes(api)
    
    // 更新WebSocket控制器以支持聊天
    // 注意：需要修改現有的WebSocket控制器初始化
}
```

### 4. 更新WebSocket控制器初始化

```go
// 修改現有的WebSocket控制器初始化代碼
func setupWebSocket(logger zerolog.Logger, driverService *service.DriverService, chatController *controller.ChatController, jwtSecret string) *controller.WebSocketController {
    // 注意：構造函數已更新，需要傳入chatController
    return controller.NewWebSocketController(logger, driverService, chatController, jwtSecret)
}
```

### 5. 路由配置

聊天系統將自動註冊以下HTTP端點：

- `POST /api/chat/upload/audio` - 上傳音頻文件
- `POST /api/chat/upload/image` - 上傳圖片文件  
- `GET /api/chat/rooms` - 獲取聊天房間列表

WebSocket端點保持不變，但現在支持額外的聊天消息類型。

## WebSocket消息類型

### 客戶端發送

```typescript
// 發送聊天消息
{
  "type": "chat_send_message",
  "data": {
    "orderId": "string",
    "message": {
      "orderId": "string",
      "type": "text|audio|image",
      "content": "string", // 可選，文字消息
      "tempId": "string"   // 臨時ID
    }
  }
}

// 獲取聊天歷史
{
  "type": "chat_get_history",
  "data": {
    "orderId": "string",
    "historyRequest": {
      "orderId": "string",
      "limit": 50,
      "offset": 0
    }
  }
}

// 標記為已讀
{
  "type": "chat_mark_as_read",
  "data": {
    "orderId": "string"
  }
}
```

### 服務端回應

```typescript
// 接收聊天消息
{
  "type": "chat_receive_message",
  "data": {
    "orderId": "string",
    "message": {
      "id": "string",
      "orderId": "string",
      "type": "text|audio|image",
      "sender": "driver|support",
      "content": "string",
      "timestamp": "2025-01-07T12:00:00Z",
      "status": "sent"
    }
  }
}

// 聊天歷史回應
{
  "type": "chat_history_response",
  "data": {
    "orderId": "string",
    "messages": [...],
    "total": 150,
    "hasMore": true
  }
}
```

## MongoDB集合結構

### order_chats - 聊天房間
- 索引：`orderId`（唯一）, `driverId`, `isActive`, `updatedAt`

### chat_messages - 聊天消息
- 索引：`orderId + createdAt`, `senderId`, `status`
- TTL：90天自動清理

### chat_unread_counts - 未讀數量
- 索引：`orderId + userId`（唯一）, `userId + userType`
- TTL：30天自動清理

## 權限控制

- **司機**：只能訪問自己被分配的訂單聊天
- **客服**：可以訪問所有聊天房間（待實現細化權限）
- **文件上傳**：驗證文件類型和大小限制

## 待完成的功能

1. **文件存儲實現**：目前文件上傳返回模擬URL，需要集成實際的文件存儲服務（如AWS S3、Azure Blob等）

2. **客服權限細化**：實現客服用戶的權限驗證和聊天房間分配

3. **訊息廣播優化**：目前廣播給所有連接的司機，需要實現精確的目標用戶廣播

4. **輸入狀態功能**：完善輸入狀態的實時廣播

5. **推送通知**：集成FCM推送服務，在用戶離線時發送通知

6. **監控和指標**：添加聊天系統的性能監控和業務指標

## 測試建議

1. **單元測試**：為各個服務方法編寫單元測試
2. **集成測試**：測試WebSocket消息流和數據庫操作
3. **壓力測試**：測試高並發聊天場景
4. **文件上傳測試**：測試各種文件格式和大小限制

## 安全考量

1. **輸入驗證**：所有用戶輸入都經過嚴格驗證
2. **權限檢查**：每個操作都驗證用戶權限
3. **文件安全**：上傳文件的類型和內容檢查
4. **數據加密**：敏感聊天內容可考慮加密存儲
5. **訪問日誌**：記錄所有聊天相關的操作

## 性能優化

1. **連接池管理**：合理管理WebSocket連接
2. **消息分頁**：避免一次性載入過多歷史消息
3. **數據庫索引**：已針對查詢模式優化索引
4. **快取策略**：可考慮為熱門聊天房間添加Redis快取

---

*此文檔版本：v1.0*  
*最後更新：2025-01-07*  
*創建者：系統架構師*