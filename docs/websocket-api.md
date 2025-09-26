# WebSocket API 參考文檔

## 🔗 連線端點

### 基本資訊
- **URL**: `ws://dev.mr-chi-tech.com/ws/driver`
- **協議**: WebSocket (ws://) 或 WebSocket Secure (wss://)
- **認證**: JWT Token (URL參數)

### 連線URL格式
```
ws://dev.mr-chi-tech.com/ws/driver?token=YOUR_JWT_TOKEN
```

### JWT Token 要求
```json
{
  "driver_id": "司機ID",
  "type": "driver",
  "exp": 1234567890,
  "iat": 1234567890
}
```

---

## 📨 消息類型總覽

| 消息類型 | 方向 | 描述 | 頻率建議 |
|----------|------|------|----------|
| `check_notifying_order` | Client → Server | 檢查通知中訂單 | 每秒1次 |
| `check_canceling_order` | Client → Server | 檢查取消中訂單 | 每秒1次 |
| `location_update` | Client → Server | 更新司機位置 | 每5秒1次 |
| `ping` | Client → Server | 心跳檢測 | 每30秒1次 |
| `check_notifying_order_response` | Server → Client | 訂單檢查回應 | 響應式 |
| `check_canceling_order_response` | Server → Client | 取消訂單回應 | 響應式 |
| `location_update_response` | Server → Client | 位置更新回應 | 響應式 |
| `pong` | Server → Client | 心跳回應 | 響應式 |

---

## 📋 詳細API規格

### 1. 檢查通知中訂單

#### 請求 (`check_notifying_order`)
```json
{
  "type": "check_notifying_order",
  "data": {}
}
```

#### 成功回應
```json
{
  "type": "check_notifying_order_response",
  "data": {
    "success": true,
    "message": "檢查通知中訂單成功",
    "data": {
      "has_pending_order": true,
      "pending_order": {
        "order_id": "68aee0265ac3591b32e2d13a",
        "remaining_seconds": 12,
        "order_data": {
          "fleet": "WEI",
          "pickup_address": "638台灣雲林縣麥寮鄉中山路119號",
          "input_pickup_address": "麥寮農會",
          "destination_address": "",
          "input_dest_address": "",
          "remarks": "測試訂單請不要接喔",
          "timestamp": 1756291114,
          "pickup_lat": "23.748718",
          "pickup_lng": "120.258089",
          "destination_lat": null,
          "destination_lng": null,
          "ori_text": "W測/麥寮農會 測試訂單請不要接喔",
          "ori_text_display": "W測 / 麥寮農會",
          "est_pick_up_dist": 0.4,
          "est_pickup_mins": 1,
          "est_pickup_time": "18:39:34",
          "est_pick_to_dest_dist": "",
          "est_pick_to_dest_mins": 0,
          "est_pick_to_dest_time": "",
          "timeout_seconds": 15
        }
      }
    }
  }
}
```

#### 無訂單回應
```json
{
  "type": "check_notifying_order_response",
  "data": {
    "success": true,
    "message": "沒找到訂單",
    "data": {
      "has_pending_order": false,
      "pending_order": null
    }
  }
}
```

#### 錯誤回應
```json
{
  "type": "check_notifying_order_response",
  "data": {
    "success": false,
    "message": "檢查通知中訂單失敗",
    "data": null,
    "error": "具體錯誤信息"
  }
}
```

---

### 2. 檢查取消中訂單

#### 請求 (`check_canceling_order`)
```json
{
  "type": "check_canceling_order",
  "data": {}
}
```

#### 有取消訂單回應
```json
{
  "type": "check_canceling_order_response",
  "data": {
    "success": true,
    "message": "檢查取消中訂單成功",
    "data": {
      "has_canceling_order": true,
      "canceling_order": {
        "order_id": "685827d11b8d093d31a074a1",
        "reason": "客戶取消",
        "cancel_time": 1672531200
      }
    }
  }
}
```

#### 無取消訂單回應
```json
{
  "type": "check_canceling_order_response",
  "data": {
    "success": true,
    "message": "檢查取消中訂單成功",
    "data": {
      "has_canceling_order": false,
      "canceling_order": null
    }
  }
}
```

---

### 3. 位置更新

#### 請求 (`location_update`)
```json
{
  "type": "location_update",
  "data": {
    "lat": "25.0675657",
    "lng": "121.5526993"
  }
}
```

**欄位說明**:
- `lat`: 緯度 (字串格式，必須)
- `lng`: 經度 (字串格式，必須)

#### 成功回應
```json
{
  "type": "location_update_response",
  "data": {
    "success": true,
    "message": "司機位置已更新"
  }
}
```

#### 錯誤回應
```json
{
  "type": "location_update_response",
  "data": {
    "success": false,
    "message": "更新司機位置失敗"
  }
}
```

**錯誤原因可能包括**:
- 位置資料格式錯誤
- 緯度或經度為空
- 資料庫更新失敗

---

### 4. 心跳檢測

#### 請求 (`ping`)
```json
{
  "type": "ping",
  "data": {
    "timestamp": "2024-01-01T12:00:00.000Z"
  }
}
```

#### 回應 (`pong`)
```json
{
  "type": "pong",
  "data": {
    "timestamp": 1704110400
  }
}
```

---

## 🔄 連線生命週期

### 1. 建立連線
```javascript
const ws = new WebSocket('ws://dev.mr-chi-tech.com/ws/driver?token=JWT_TOKEN');
```

### 2. 連線成功
- 客戶端收到 `onopen` 事件
- 服務器建立WebSocket連線記錄
- 開始心跳機制

### 3. 消息通信
- 客戶端發送請求消息
- 服務器處理並回應
- 支持並發消息處理

### 4. 連線關閉
- 服務器清理WebSocket連線資源
- 客戶端收到 `onclose` 事件
- **注意**: 不會影響司機的業務上線狀態

---

## ⚡ 性能建議

### 消息發送頻率
| 消息類型 | 建議頻率 | 最高頻率 |
|----------|----------|----------|
| `check_notifying_order` | 1秒/次 | 1秒/次 |
| `check_canceling_order` | 1秒/次 | 1秒/次 |
| `location_update` | 5秒/次 | 1秒/次 |
| `ping` | 30秒/次 | 10秒/次 |

### 連線管理
- **連線超時**: 60秒無活動自動斷線
- **重連間隔**: 建議3秒後重連
- **最大重連**: 建議最多重試10次

### 消息隊列
- 斷線時將消息放入隊列
- 重連後自動重發隊列中的消息
- 避免消息丟失

---

## 🔧 錯誤處理

### 連線錯誤
| 錯誤碼 | 描述 | 處理方式 |
|--------|------|----------|
| 1002 | 協議錯誤 | 檢查消息格式 |
| 1003 | 不支持的數據類型 | 檢查消息內容 |
| 1006 | 異常關閉 | 實現自動重連 |
| 1011 | 服務器錯誤 | 稍後重試 |

### 認證錯誤
- **401**: Token無效或過期
- **403**: Token類型錯誤（非driver類型）
- **404**: 司機不存在

### 業務錯誤
- 位置格式錯誤
- 服務不可用
- 資料庫連接失敗

---

## 📊 監控指標

### 連線統計
```json
{
  "connected_drivers": 150,
  "connections_by_fleet": {
    "WEI": 50,
    "TAXI": 100
  },
  "connection_status": {
    "driver_123": "connected",
    "driver_456": "connected"
  }
}
```

### 消息統計
- 每秒消息處理量
- 消息類型分布
- 錯誤率統計

---

## 🧪 測試工具

### 使用 wscat 測試
```bash
# 安裝
npm install -g wscat

# 連線測試
wscat -c "ws://dev.mr-chi-tech.com/ws/driver?token=YOUR_TOKEN"

# 發送消息
{"type":"ping","data":{"timestamp":"2024-01-01T12:00:00.000Z"}}
```

### 使用 Postman 測試
1. 新建 WebSocket Request
2. URL: `ws://dev.mr-chi-tech.com/ws/driver?token=TOKEN`
3. 發送測試消息

---

## 🔒 安全考量

### Token 安全
- 使用HTTPS獲取Token
- Token設置合理過期時間
- 定期刷新Token

### 連線安全
- 生產環境使用WSS協議
- 實現速率限制
- 監控異常連線行為

### 數據驗證
- 驗證所有輸入數據
- 過濾惡意消息
- 記錄安全事件

這份API文檔提供了WebSocket接口的完整技術規格，方便開發和集成！🚀