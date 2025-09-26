package driver

import "right-backend/data-models/common"

// CheckNotifyingOrderInput 檢查通知中訂單的輸入（從 JWT token 獲取司機ID）
type CheckNotifyingOrderInput struct {
}

// NotifyingOrderData 通知中訂單資料
type NotifyingOrderData struct {
	Fleet              string  `json:"fleet" example:"WEI"`
	PickupAddress      string  `json:"pickup_address" example:"638台灣雲林縣麥寮鄉中山路119號"`
	InputPickupAddress string  `json:"input_pickup_address" example:"麥寮農會"`
	DestinationAddress string  `json:"destination_address" example:""`
	InputDestAddress   string  `json:"input_dest_address" example:""`
	Remarks            string  `json:"remarks" example:"測試訂單請不要接喔"`
	Timestamp          int64   `json:"timestamp" example:"1756291114"`
	PickupLat          string  `json:"pickup_lat" example:"23.748718"`
	PickupLng          string  `json:"pickup_lng" example:"120.258089"`
	DestinationLat     *string `json:"destination_lat" example:"null"`
	DestinationLng     *string `json:"destination_lng" example:"null"`
	OriText            string  `json:"ori_text" example:"W測/麥寮農會 測試訂單請不要接喔"`
	OriTextDisplay     string  `json:"ori_text_display" example:"W測 / 麥寮農會"`
	EstPickUpDist      float64 `json:"est_pick_up_dist" example:"0.4"`
	EstPickupMins      int     `json:"est_pickup_mins" example:"1"`
	EstPickupTime      string  `json:"est_pickup_time" example:"18:39:34"`
	EstPickToDestDist  string  `json:"est_pick_to_dest_dist" example:""`
	EstPickToDestMins  int     `json:"est_pick_to_dest_mins" example:"0"`
	EstPickToDestTime  string  `json:"est_pick_to_dest_time" example:""`
	TimeoutSeconds     int     `json:"timeout_seconds" example:"15"`
	OrderType          string  `json:"order_type,omitempty" example:"即時" doc:"訂單類型：即時或預約"`
	ScheduledTime      *string `json:"scheduled_time,omitempty" example:"2025-09-21 14:30:00" doc:"預約時間（僅預約單）"`
}

// NotifyingOrder 通知中訂單
type NotifyingOrder struct {
	OrderID          string              `json:"order_id" example:"68aee0265ac3591b32e2d13a"`
	RemainingSeconds int                 `json:"remaining_seconds" example:"12"`
	OrderData        *NotifyingOrderData `json:"order_data"`
}

// CheckNotifyingOrderData 檢查通知中訂單的資料
type CheckNotifyingOrderData struct {
	HasNotifyingOrder bool            `json:"has_pending_order" example:"true"`
	NotifyingOrder    *NotifyingOrder `json:"pending_order,omitempty"`
}

// CheckNotifyingOrderResponse 檢查通知中訂單的回應
type CheckNotifyingOrderResponse struct {
	Body common.APIResponse[CheckNotifyingOrderData] `json:"body"`
}

// RedisNotifyingOrder Redis 中儲存的通知中訂單資料
type RedisNotifyingOrder struct {
	OrderID        string              `json:"order_id"`
	DriverID       string              `json:"driver_id"`
	PushTime       int64               `json:"push_time"`       // 推送時間戳記
	TimeoutSeconds int                 `json:"timeout_seconds"` // 超時秒數
	OrderData      *NotifyingOrderData `json:"order_data"`      // 訂單資料
}
