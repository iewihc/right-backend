package model

// CreateOrderResponse 創建訂單的響應
type CreateOrderResponse struct {
	Type    string      `json:"type"`    // "order" 或 "schedule"
	Data    interface{} `json:"data"`    // Order
	Message string      `json:"message"` // 成功訊息
}

// CreateOrderResult 創建訂單的結果（內部使用）
type CreateOrderResult struct {
	IsScheduled bool        // 是否為排程訂單
	Order       *Order      // 立即創建的訂單
	Schedule    interface{} // 排程請求（避免循環依賴，使用 interface{}）
	Message     string      // 訊息
}
