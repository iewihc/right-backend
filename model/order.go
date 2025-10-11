package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type OrderType string

const (
	OrderTypeInstant   OrderType = "即時"
	OrderTypeScheduled OrderType = "預約"
)

type Customer struct {
	InputPickupAddress string  `json:"input_pickup_address" bson:"input_pickup_address" doc:"客戶輸入的上車地點"`
	PickupAddress      string  `json:"pickup_address,omitempty" bson:"pickup_address,omitempty" doc:"實際上車地點(經 Google 解析)"`
	PickupLat          *string `json:"pickup_lat,omitempty" bson:"pickup_lat,omitempty" doc:"上車點緯度"`
	PickupLng          *string `json:"pickup_lng,omitempty" bson:"pickup_lng,omitempty" doc:"上車點經度"`
	Remarks            string  `json:"remarks" bson:"remarks" example:"我有帶寵物" doc:"備註"`
	InputDestAddress   string  `json:"input_dest_address,omitempty" bson:"input_dest_address,omitempty" doc:"客戶輸入的目的地"`
	DestAddress        string  `json:"dest_address,omitempty" bson:"dest_address,omitempty" doc:"實際目的地(經 Google 解析)"`
	DestLat            *string `json:"dest_lat,omitempty" bson:"dest_lat,omitempty" doc:"目的地緯度"`
	DestLng            *string `json:"dest_lng,omitempty" bson:"dest_lng,omitempty" doc:"目的地經度"`
	EstPickToDestMins  *int    `json:"est_pick_to_dest_mins,omitempty" bson:"est_pick_to_dest_mins,omitempty" doc:"預估上車到目的地時間(分鐘)"`
	EstPickToDestDist  *string `json:"est_pick_to_dest_dist,omitempty" bson:"est_pick_to_dest_dist,omitempty" doc:"預估上車到目的地距離(公里)" example:"5.2"`
	EstPickToDestTime  *string `json:"est_pick_to_dest_time,omitempty" bson:"est_pick_to_dest_time,omitempty" doc:"預估到達目的地時間(HH:MM:SS)"`
	LineUserID         string  `json:"line_user_id,omitempty" bson:"line_user_id,omitempty" example:"user123" doc:"Line用戶ID"`
}

// LineMessageInfo 記錄 LINE 消息資訊
type LineMessageInfo struct {
	ConfigID    string    `json:"config_id" bson:"config_id" doc:"LINE Bot 配置 ID"`
	UserID      string    `json:"user_id" bson:"user_id" doc:"LINE 用戶 ID"`
	MessageType string    `json:"message_type" bson:"message_type" doc:"消息類型"`
	Timestamp   time.Time `json:"timestamp" bson:"timestamp" doc:"時間戳記"`
}

type Driver struct {
	AssignedDriver       string  `json:"assigned_driver,omitempty" bson:"assigned_driver,omitempty" example:"driver123" doc:"分派的司機ID"`
	CarNo                string  `json:"car_no,omitempty" bson:"car_no,omitempty" example:"AAA-5678黑" doc:"車牌號碼"`
	CarColor             string  `json:"car_color,omitempty" bson:"car_color,omitempty" example:"白色" doc:"車輛顏色"`
	EstPickupMins        int     `json:"est_pickup_mins,omitempty" bson:"est_pickup_mins,omitempty" example:"15" doc:"預估到達時間(分鐘)"`
	EstPickupDistKm      float64 `json:"est_pickup_dist_km,omitempty" bson:"est_pickup_dist_km,omitempty" doc:"預估到達距離(公里)"`
	EstPickupTime        string  `json:"est_pickup_time,omitempty" bson:"est_pickup_time,omitempty" example:"14:30:00" doc:"預估到達時間(HH:MM:SS)"`
	AdjustMins           *int    `json:"adjust_mins,omitempty" bson:"adjust_mins,omitempty" doc:"司機調整的分鐘數"`
	Lat                  *string `json:"lat,omitempty" bson:"lat,omitempty" example:"25.0675657" doc:"司機當前緯度"`
	Lng                  *string `json:"lng,omitempty" bson:"lng,omitempty" example:"121.5526993" doc:"司機當前經度"`
	LineUserID           string  `json:"line_user_id,omitempty" bson:"line_user_id,omitempty" example:"driver123" doc:"司機Line用戶ID"`
	Name                 string  `json:"name,omitempty" bson:"name,omitempty" example:"王小明" doc:"司機姓名"`
	Duration             int     `json:"duration,omitempty" bson:"duration,omitempty" doc:"司機用時時間(秒)"`
	ArrivalDeviationSecs *int    `json:"arrival_deviation_secs,omitempty" bson:"arrival_deviation_secs,omitempty" doc:"到達時間偏差(秒)，正值表示遲到，負值表示早到"`
	FCMSentTime          *int64  `json:"fcm_sent_time,omitempty" bson:"fcm_sent_time,omitempty" example:"1756291114" doc:"FCM 推送發送時間（Unix timestamp UTC+8）"`
}

type Order struct {
	ID                   *primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty" example:"684a73ad0e3a583c37e4b30d" doc:"訂單ID"`
	ShortID              string              `json:"short_id,omitempty" bson:"short_id,omitempty" example:"#9011" doc:"訂單短ID"`
	Type                 OrderType           `json:"type" bson:"type" example:"即時" doc:"訂單類型"`
	Status               OrderStatus         `json:"status,omitempty" bson:"status,omitempty" example:"等待接單" doc:"訂單狀態"`
	ScheduledAt          *time.Time          `json:"scheduled_at,omitempty" bson:"scheduled_at,omitempty" doc:"預約時間"`
	AmountNote           string              `json:"amount_note,omitempty" bson:"amount_note,omitempty" doc:"金額備註"`
	Income               *int                `json:"income,omitempty" bson:"income,omitempty" example:"100" doc:"收入"`
	Expense              *int                `json:"expense,omitempty" bson:"expense,omitempty" example:"50" doc:"支出"`
	PassengerID          string              `json:"passenger_id,omitempty" bson:"passenger_id,omitempty" doc:"乘客ID"`
	Customer             Customer            `json:"customer" bson:"customer"`
	CustomerGroup        string              `json:"customer_group" bson:"customer_group"`
	Fleet                FleetType           `json:"fleet" bson:"fleet" example:"RSK" doc:"車隊"`
	Rounds               *int                `json:"rounds,omitempty" bson:"rounds,omitempty" example:"1" doc:"派單輪數"`
	PickupCertificateURL string              `json:"pickup_certificate_url,omitempty" bson:"pickup_certificate_url,omitempty" doc:"司機抵達上車點的證明圖片URL"`
	IsPhotoTaken         bool                `json:"is_photo_taken,omitempty" bson:"is_photo_taken,omitempty" doc:"是否已拍照"`
	HasMeterJump         bool                `json:"has_meter_jump,omitempty" bson:"has_meter_jump,omitempty" doc:"是否跳表"`
	IsErrand             bool                `json:"is_errand,omitempty" bson:"is_errand,omitempty" doc:"是否為跑腿"`
	IsScheduled          bool                `json:"is_scheduled,omitempty" bson:"is_scheduled,omitempty" doc:"是否為預約單"`
	CreatedBy            string              `json:"created_by,omitempty" bson:"created_by,omitempty" doc:"建立者姓名"`
	CreatedType          string              `json:"created_type,omitempty" bson:"created_type,omitempty" doc:"建立者類型(discord/line/system)"`
	CreatedAt            *time.Time          `json:"created_at,omitempty" bson:"created_at,omitempty" doc:"建立時間"`
	UpdatedAt            *time.Time          `json:"updated_at,omitempty" bson:"updated_at,omitempty" doc:"更新時間"`
	AcceptanceTime       *time.Time          `json:"acceptance_time,omitempty" bson:"acceptance_time,omitempty" doc:"接單時間"`
	ArrivalTime          *time.Time          `json:"arrival_time,omitempty" bson:"arrival_time,omitempty" doc:"到達時間"`
	PickUpTime           *time.Time          `json:"pickup_time,omitempty" bson:"pickup_time,omitempty" doc:"客人上車時間"`
	CompletionTime       *time.Time          `json:"completion_time,omitempty" bson:"completion_time,omitempty" doc:"完成時間"`
	Driver               Driver              `json:"driver,omitempty" bson:"driver,omitempty"`
	OriText              string              `json:"ori_text,omitempty" bson:"ori_text,omitempty" doc:"原始輸入文字"`
	OriTextDisplay       string              `json:"ori_text_display,omitempty" bson:"ori_text_display,omitempty" doc:"去除hints的原始輸入文字"`
	Hints                string              `json:"hints,omitempty" bson:"hints,omitempty" doc:"備註+時間欄位(空格後方)"`
	HasCopy              bool                `json:"has_copy" bson:"has_copy,omitempty" doc:"是否已複製"`
	HasNotify            bool                `json:"has_notify" bson:"has_notify,omitempty" doc:"是否已通知"`
	DriverNotified       bool                `json:"driver_notified,omitempty" bson:"driver_notified,omitempty" doc:"是否已通知司機(預約單一小時前提醒)"`
	DiscordChannelID     string              `json:"discord_channel_id,omitempty" bson:"discord_channel_id,omitempty" doc:"Discord頻道ID"`
	DiscordMessageID     string              `json:"discord_message_id,omitempty" bson:"discord_message_id,omitempty" doc:"Discord訊息ID"`
	ConvertedFrom        string              `json:"converted_from,omitempty" bson:"converted_from,omitempty" doc:"轉換來源（如：scheduled）"`
	LineMessages         []LineMessageInfo   `json:"line_messages,omitempty" bson:"line_messages,omitempty" doc:"LINE 消息記錄"`
	Logs                 []OrderLogEntry     `json:"logs,omitempty" bson:"logs,omitempty" doc:"訂單日誌"`
	HasPets              bool                `json:"has_pets,omitempty" bson:"has_pets,omitempty" doc:"訂單是否包含寵物（偵測到關鍵字：狗、貓、寵、籠）"`
	HasOverloaded        bool                `json:"has_overloaded,omitempty" bson:"has_overloaded,omitempty" doc:"訂單是否超載（偵測到關鍵字：5人、五人、6人、六人）"`
}

type OrderLogAction string

const (
	OrderLogActionCreated        OrderLogAction = "訂單建立"
	OrderLogActionDriverNotified OrderLogAction = "司機通知"
	OrderLogActionDriverReject   OrderLogAction = "司機拒絕"
	OrderLogActionDriverTimeout  OrderLogAction = "司機未接"
	OrderLogActionDriverAccept   OrderLogAction = "司機接單"
	OrderLogActionDriverArrived  OrderLogAction = "司機抵達"
	OrderLogActionCustomerPickup OrderLogAction = "執行任務"
	OrderLogActionOrderCompleted OrderLogAction = "司機完成"
	OrderLogActionDispatchCancel OrderLogAction = "調度取消"
)

type OrderLogEntry struct {
	Action     OrderLogAction `json:"action" bson:"action" doc:"動作類型"`
	Timestamp  time.Time      `json:"timestamp" bson:"timestamp" doc:"時間戳記"`
	Fleet      FleetType      `json:"fleet,omitempty" bson:"fleet,omitempty" doc:"車隊名稱"`
	DriverName string         `json:"driver_name,omitempty" bson:"driver_name,omitempty" doc:"司機姓名"`
	CarPlate   string         `json:"car_plate,omitempty" bson:"car_plate,omitempty" doc:"車牌號碼"`
	DriverID   string         `json:"driver_id,omitempty" bson:"driver_id,omitempty" doc:"司機ID"`
	DriverInfo string         `json:"driver_info,omitempty" bson:"driver_info,omitempty" doc:"司機資訊(車隊|司機編號|司機名稱)"`
	Details    string         `json:"details,omitempty" bson:"details,omitempty" doc:"額外詳情"`
	Rounds     int            `json:"rounds,omitempty" bson:"rounds,omitempty" doc:"派單輪數"`
}

// OrderInfo 用於在服務之間傳遞訂單資訊，特別是為了WebSocket推送
type OrderInfo struct {
	ID                 primitive.ObjectID
	InputPickupAddress string
	InputDestAddress   string
	PickupAddress      string
	DestinationAddress string
	PickupLat          *string
	PickupLng          *string
	DestinationLat     *string
	DestinationLng     *string
	Remarks            string
	Fleet              FleetType
	Timestamp          time.Time
	EstPickUpDist      float64 `json:"est_pick_up_dist"`
	EstPickupMins      int
	EstPickupTime      string
	EstPickToDestDist  string
	EstPickToDestMins  int
	EstPickToDestTime  string
	OriText            string
	OriTextDisplay     string
}

// OrderPushData 統一的訂單推送資料結構，用於WebSocket和推送服務
type OrderPushData struct {
	Type               string    `json:"type"`
	OrderID            string    `json:"order_id"`
	InputPickupAddress string    `json:"input_pickup_address"`
	InputDestAddress   string    `json:"input_dest_address,omitempty"`
	PickupAddress      string    `json:"pickup_address"`
	DestinationAddress string    `json:"destination_address,omitempty"`
	PickupLat          string    `json:"pickup_lat,omitempty"`
	PickupLng          string    `json:"pickup_lng,omitempty"`
	DestinationLat     string    `json:"destination_lat,omitempty"`
	DestinationLng     string    `json:"destination_lng,omitempty"`
	Remarks            string    `json:"remarks,omitempty"`
	Fleet              FleetType `json:"fleet"`
	Timestamp          int64     `json:"timestamp"`
	OrderCreatedTime   string    `json:"order_created_time"`
	TimeoutSeconds     int       `json:"timeout_seconds"`
	EstPickUpDist      float64   `json:"est_pick_up_dist"`
	EstPickupMins      int       `json:"est_pickup_mins"`
	EstPickupTime      string    `json:"est_pickup_time"`
	EstPickToDestDist  string    `json:"est_pick_to_dest_dist"`
	EstPickToDestMins  int       `json:"est_pick_to_dest_mins"`
	EstPickToDestTime  string    `json:"est_pick_to_dest_time"`
	OriText            string    `json:"ori_text"`
	OriTextDisplay     string    `json:"ori_text_display"`
}

// ToOrderPushData 將OrderInfo轉換為統一的推送資料格式
func (oi *OrderInfo) ToOrderPushData(timeoutSeconds int) *OrderPushData {
	data := &OrderPushData{
		Type:               string(NotifyTypeNewOrder),
		OrderID:            oi.ID.Hex(),
		InputPickupAddress: oi.InputPickupAddress,
		PickupAddress:      oi.PickupAddress,
		DestinationAddress: oi.DestinationAddress,
		Remarks:            oi.Remarks,
		Fleet:              oi.Fleet,
		Timestamp:          time.Now().Unix(),
		OrderCreatedTime:   formatTimeInTaipeiHMS(time.Now()),
		TimeoutSeconds:     timeoutSeconds,
		EstPickUpDist:      oi.EstPickUpDist,
		EstPickupMins:      oi.EstPickupMins,
		EstPickupTime:      oi.EstPickupTime,
		EstPickToDestDist:  oi.EstPickToDestDist,
		EstPickToDestMins:  oi.EstPickToDestMins,
		EstPickToDestTime:  oi.EstPickToDestTime,
		OriText:            oi.OriText,
		OriTextDisplay:     getDisplayText(oi),
	}

	// 添加座標資訊（如果存在）
	if oi.PickupLat != nil {
		data.PickupLat = *oi.PickupLat
	}
	if oi.PickupLng != nil {
		data.PickupLng = *oi.PickupLng
	}

	return data
}

// ToMap 將OrderPushData轉換為map[string]interface{}格式，用於推送服務
func (opd *OrderPushData) ToMap() map[string]interface{} {
	data := map[string]interface{}{
		"type":     opd.Type,
		"order_id": opd.OrderID,
		"ori_text": opd.OriText,
	}

	// 只在非空時添加座標資訊
	if opd.PickupLat != "" {
		data["pickup_lat"] = opd.PickupLat
	}
	if opd.PickupLng != "" {
		data["pickup_lng"] = opd.PickupLng
	}

	return data
}

// SimpleOrderData 簡化的訂單資料結構
type SimpleOrderData struct {
	OrderID   string `json:"order_id" doc:"訂單ID"`
	ShortID   string `json:"short_id" doc:"訂單簡短ID"`
	Type      string `json:"type" doc:"訂單類型"`
	CreatedAt string `json:"created_at" doc:"創建時間"`
	OriText   string `json:"ori_text" doc:"原始文字"`
}

// AddressSuggestionData 地址建議資料結構
type AddressSuggestionData struct {
	Message     string        `json:"message" doc:"說明訊息"`
	Suggestions []interface{} `json:"suggestions" doc:"地址建議列表"`
}

// SimpleOrderResponse 簡化的訂單回應結構 - 可以是訂單資料或地址建議
type SimpleOrderResponse struct {
	Body interface{} `json:"body"`
}

// getDisplayText 取得顯示文字，如果OriTextDisplay為空則使用InputPickupAddress
func getDisplayText(oi *OrderInfo) string {
	if oi.OriTextDisplay != "" {
		return oi.OriTextDisplay
	}
	// 如果OriTextDisplay為空，使用InputPickupAddress作為顯示文字
	return oi.InputPickupAddress
}

// formatTimeInTaipeiHMS 將時間轉換為台北時區的時分秒格式
func formatTimeInTaipeiHMS(t time.Time) string {
	// 台北時區 (UTC+8)
	taipei := time.FixedZone("Asia/Taipei", 8*3600)
	return t.In(taipei).Format("15:04:05")
}
