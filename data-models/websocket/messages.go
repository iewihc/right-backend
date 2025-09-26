package websocket

import "time"

// WSMessage WebSocket 訊息類型
type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// PingRequest 心跳請求
type PingRequest struct {
	Timestamp time.Time `json:"timestamp"`
}

// PongResponse 心跳回應
type PongResponse struct {
	Timestamp int64 `json:"timestamp"`
}

// CheckNotifyingOrderRequest 檢查通知中訂單請求
type CheckNotifyingOrderRequest struct {
	// 無需額外參數，司機ID從連線中獲取
}

// CheckNotifyingOrderResponse WebSocket版本的檢查通知中訂單回應 - 與API版本格式相同
type CheckNotifyingOrderResponse struct {
	Success bool        `json:"success" doc:"請求是否成功"`
	Message string      `json:"message" doc:"回傳訊息"`
	Data    interface{} `json:"data,omitempty" doc:"回傳資料"`
	Error   string      `json:"error,omitempty" doc:"錯誤訊息"`
}

// CheckCancelingOrderRequest 檢查取消中訂單請求
type CheckCancelingOrderRequest struct {
	// 無需額外參數，司機ID從連線中獲取
}

// CheckCancelingOrderResponse WebSocket版本的檢查取消中訂單回應 - 與API版本格式相同
type CheckCancelingOrderResponse struct {
	Success bool        `json:"success" doc:"請求是否成功"`
	Message string      `json:"message" doc:"回傳訊息"`
	Data    interface{} `json:"data,omitempty" doc:"回傳資料"`
	Error   string      `json:"error,omitempty" doc:"錯誤訊息"`
}

// LocationUpdateRequest 位置更新請求
type LocationUpdateRequest struct {
	Lat string `json:"lat" example:"25.0675657" doc:"緯度"`
	Lng string `json:"lng" example:"121.5526993" doc:"經度"`
}

// LocationUpdateResponse 位置更新回應 - 與driver.SimpleResponse格式相同
type LocationUpdateResponse struct {
	Success bool   `json:"success" example:"true"`
	Message string `json:"message" example:"司機位置已更新"`
}

// OrderUpdatePushMessage 用於更新目的地後的資訊推送
type OrderUpdatePushMessage struct {
	OrderID            string `json:"order_id"`
	DestinationAddress string `json:"destination_address"`
	DestinationLat     string `json:"destination_lat"`
	DestinationLng     string `json:"destination_lng"`
	EstPickToDestDist  string `json:"est_pick_to_dest_dist"`
	EstPickToDestMins  int    `json:"est_pick_to_dest_mins"`
	EstPickToDestTime  string `json:"est_pick_to_dest_time"`
}

// OrderStatusUpdateMessage 用於推送訂單狀態變更
type OrderStatusUpdateMessage struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"` // 可選，用於提供額外資訊，如取消原因
}
