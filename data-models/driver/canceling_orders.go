package driver

import "right-backend/data-models/common"

// CheckCancelingOrderInput 檢查取消中訂單的輸入（從 JWT token 獲取司機ID）
type CheckCancelingOrderInput struct {
}

// CancelingOrderData 取消中訂單資料
type CancelingOrderData struct {
	Fleet              string  `json:"fleet" example:"WEI"`
	PickupAddress      string  `json:"pickup_address" example:"638台灣雲林縣麥寮鄉中山路119號"`
	InputPickupAddress string  `json:"input_pickup_address" example:"麥寮農會"`
	DestinationAddress string  `json:"destination_address" example:""`
	InputDestAddress   string  `json:"input_dest_address" example:""`
	Remarks            string  `json:"remarks" example:"測試訂單請不要接喔"`
	CancelTime         int64   `json:"cancel_time" example:"1756291114"`
	PickupLat          string  `json:"pickup_lat" example:"23.748718"`
	PickupLng          string  `json:"pickup_lng" example:"120.258089"`
	DestinationLat     *string `json:"destination_lat" example:"null"`
	DestinationLng     *string `json:"destination_lng" example:"null"`
	OriText            string  `json:"ori_text" example:"W測/麥寮農會 測試訂單請不要接喔"`
	OriTextDisplay     string  `json:"ori_text_display" example:"W測 / 麥寮農會"`
	CancelReason       string  `json:"cancel_reason" example:"乘客取消"`
	TimeoutSeconds     int     `json:"timeout_seconds" example:"30"`
	OrderType          string  `json:"order_type,omitempty" example:"即時" doc:"訂單類型：即時或預約"`
	ScheduledTime      *string `json:"scheduled_time,omitempty" example:"2025-09-21 14:30:00" doc:"預約時間（僅預約單）"`
}

// CancelingOrder 取消中訂單
type CancelingOrder struct {
	OrderID          string              `json:"order_id" example:"68aee0265ac3591b32e2d13a"`
	RemainingSeconds int                 `json:"remaining_seconds" example:"25"`
	OrderData        *CancelingOrderData `json:"order_data"`
}

// CheckCancelingOrderData 檢查取消中訂單的資料
type CheckCancelingOrderData struct {
	HasCancelingOrder bool            `json:"has_canceling_order" example:"true"`
	CancelingOrder    *CancelingOrder `json:"canceling_order,omitempty"`
}

// CheckCancelingOrderResponse 檢查取消中訂單的回應
type CheckCancelingOrderResponse struct {
	Body common.APIResponse[CheckCancelingOrderData] `json:"body"`
}

// RedisCancelingOrder Redis 中儲存的取消中訂單資料
type RedisCancelingOrder struct {
	OrderID        string              `json:"order_id"`
	DriverID       string              `json:"driver_id"`
	CancelTime     int64               `json:"cancel_time"`     // 取消時間戳記
	TimeoutSeconds int                 `json:"timeout_seconds"` // 超時秒數
	OrderData      *CancelingOrderData `json:"order_data"`      // 訂單資料
}
