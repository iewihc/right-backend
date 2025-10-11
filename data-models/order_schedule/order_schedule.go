package order_schedule

// AcceptScheduledOrderInput 司機接收預約訂單輸入
type AcceptScheduledOrderInput struct {
	Body struct {
		OrderID string `json:"order_id" doc:"預約訂單ID" example:"664a73ad0e3a583c37e4b30d"`
	} `json:"body"`
}

// AcceptScheduledOrderResponse 司機接收預約訂單回應
type AcceptScheduledOrderResponse struct {
	Body struct {
		Message       string `json:"message" example:"預約單接單成功"`
		ScheduledTime string `json:"scheduled_time" example:"2025-09-21 14:30:00" doc:"預約時間"`
		PickupAddress string `json:"pickup_address" example:"台北101" doc:"上車地點"`
	} `json:"body"`
}

// ActivateScheduledOrderInput 激活預約單輸入
type ActivateScheduledOrderInput struct {
	OrderID string `path:"id" doc:"預約訂單ID" example:"664a73ad0e3a583c37e4b30d"`
}

// ActivateScheduledOrderData 激活預約單回應資料
type ActivateScheduledOrderData struct {
	OrderID       string `json:"order_id" example:"664a73ad0e3a583c37e4b30d"`
	ScheduledTime string `json:"scheduled_time" example:"2025-09-21 14:30:00" doc:"預約時間"`
	PickupAddress string `json:"pickup_address" example:"台北101" doc:"上車地點"`
	OrderStatus   string `json:"order_status" example:"schedule_activated" doc:"訂單狀態"`
	DriverStatus  string `json:"driver_status" example:"busy" doc:"司機狀態"`
}

// ActivateScheduledOrderResponse 激活預約單回應
type ActivateScheduledOrderResponse struct {
	Body ActivateScheduledOrderData `json:"body"`
}

// CurrentScheduledOrderInput 獲取當前預約單輸入
type CurrentScheduledOrderInput struct{}

// CurrentScheduledOrderData 當前預約單完整資料
type CurrentScheduledOrderData struct {
	OrderID        string `json:"order_id" example:"664a73ad0e3a583c37e4b30d"`
	ShortID        string `json:"short_id" example:"#fa95" doc:"訂單短ID"`
	Type           string `json:"type" example:"預約" doc:"訂單類型"`
	Status         string `json:"status" example:"預約單已接受" doc:"預約單狀態"`
	OrderStatus    string `json:"order_status" example:"schedule_accepted" doc:"訂單狀態"`
	DriverStatus   string `json:"driver_status" example:"busy" doc:"司機狀態"`
	ScheduledTime  string `json:"scheduled_time" example:"2025-09-21 14:30:00" doc:"預約時間"`
	AmountNote     string `json:"amount_note,omitempty" doc:"金額備註"`
	Income         *int   `json:"income,omitempty" example:"100" doc:"收入"`
	Expense        *int   `json:"expense,omitempty" example:"50" doc:"支出"`
	PassengerID    string `json:"passenger_id,omitempty" doc:"乘客ID"`
	Fleet          string `json:"fleet" example:"RSK" doc:"車隊"`
	Rounds         *int   `json:"rounds,omitempty" example:"1" doc:"派單輪數"`
	IsPhotoTaken   bool   `json:"is_photo_taken,omitempty" doc:"是否已拍照"`
	HasMeterJump   bool   `json:"has_meter_jump,omitempty" doc:"是否跳表"`
	IsErrand       bool   `json:"is_errand,omitempty" doc:"是否為跑腿"`
	IsScheduled    bool   `json:"is_scheduled,omitempty" doc:"是否為預約單"`
	CreatedBy      string `json:"created_by,omitempty" doc:"建立者姓名"`
	CreatedAt      string `json:"created_at,omitempty" doc:"建立時間"`
	UpdatedAt      string `json:"updated_at,omitempty" doc:"更新時間"`
	OriText        string `json:"ori_text,omitempty" doc:"原始輸入文字"`
	OriTextDisplay string `json:"ori_text_display,omitempty" doc:"去除hints的原始輸入文字"`

	// 客戶資訊
	Customer CustomerData `json:"customer"`

	// 司機資訊
	Driver DriverData `json:"driver,omitempty"`
}

// CustomerData 客戶資料
type CustomerData struct {
	InputPickupAddress string  `json:"input_pickup_address" doc:"客戶輸入的上車地點"`
	PickupAddress      string  `json:"pickup_address,omitempty" doc:"實際上車地點(經 Google 解析)"`
	PickupLat          *string `json:"pickup_lat,omitempty" doc:"上車點緯度"`
	PickupLng          *string `json:"pickup_lng,omitempty" doc:"上車點經度"`
	Remarks            string  `json:"remarks" example:"我有帶寵物" doc:"備註"`
	InputDestAddress   string  `json:"input_dest_address,omitempty" doc:"客戶輸入的目的地"`
	DestAddress        string  `json:"dest_address,omitempty" doc:"實際目的地(經 Google 解析)"`
	DestLat            *string `json:"dest_lat,omitempty" doc:"目的地緯度"`
	DestLng            *string `json:"dest_lng,omitempty" doc:"目的地經度"`
	EstPickToDestMins  *int    `json:"est_pick_to_dest_mins,omitempty" doc:"預估上車到目的地時間(分鐘)"`
	EstPickToDestDist  *string `json:"est_pick_to_dest_dist,omitempty" doc:"預估上車到目的地距離(公里)" example:"5.2"`
	EstPickToDestTime  *string `json:"est_pick_to_dest_time,omitempty" doc:"預估到達目的地時間(HH:MM:SS)"`
	LineUserID         string  `json:"line_user_id,omitempty" example:"user123" doc:"Line用戶ID"`
}

// DriverData 司機資料
type DriverData struct {
	AssignedDriver       string  `json:"assigned_driver,omitempty" example:"driver123" doc:"分派的司機ID"`
	CarNo                string  `json:"car_no,omitempty" example:"AAA-5678黑" doc:"車牌號碼"`
	CarColor             string  `json:"car_color,omitempty" example:"白色" doc:"車輛顏色"`
	EstPickupMins        int     `json:"est_pickup_mins,omitempty" example:"15" doc:"預估到達時間(分鐘)"`
	EstPickupDistKm      float64 `json:"est_pickup_dist_km,omitempty" doc:"預估到達距離(公里)"`
	EstPickupTime        string  `json:"est_pickup_time,omitempty" example:"14:30:00" doc:"預估到達時間(HH:MM:SS)"`
	AdjustMins           *int    `json:"adjust_mins,omitempty" doc:"司機調整的分鐘數"`
	Lat                  *string `json:"lat,omitempty" example:"25.0675657" doc:"司機當前緯度"`
	Lng                  *string `json:"lng,omitempty" example:"121.5526993" doc:"司機當前經度"`
	LineUserID           string  `json:"line_user_id,omitempty" example:"driver123" doc:"司機Line用戶ID"`
	Name                 string  `json:"name,omitempty" example:"王小明" doc:"司機姓名"`
	Duration             int     `json:"duration,omitempty" doc:"司機用時時間(秒)"`
	ArrivalDeviationSecs *int    `json:"arrival_deviation_secs,omitempty" doc:"到達時間偏差(秒)，正值表示遲到，負值表示早到"`
}

// CurrentScheduledOrderResponse 獲取當前預約單回應
type CurrentScheduledOrderResponse struct {
	Body *CurrentScheduledOrderData `json:"body"`
}

// ScheduleOrderCountInput 獲取預約單數量輸入（合併版本）
type ScheduleOrderCountInput struct {
	Fleet string `query:"fleet" example:"right_taxi" doc:"車隊過濾，留空則獲取全部車隊的數量"`
}

// ScheduleOrderCountResponse 獲取預約單數量回應（合併版本）
type ScheduleOrderCountResponse struct {
	Body struct {
		Count int64 `json:"count" example:"15" doc:"可接預約單數量"`
	} `json:"body"`
}

// GetScheduleOrdersInput 獲取預約訂單列表輸入
type GetScheduleOrdersInput struct {
	PageNum  int `query:"page_num" example:"1" doc:"頁碼，從1開始" default:"1"`
	PageSize int `query:"page_size" example:"10" doc:"每頁數量" default:"10"`
}

// GetPageNum 獲取頁碼，預設為1
func (input *GetScheduleOrdersInput) GetPageNum() int {
	if input.PageNum < 1 {
		return 1
	}
	return input.PageNum
}

// GetPageSize 獲取每頁數量，預設為10
func (input *GetScheduleOrdersInput) GetPageSize() int {
	if input.PageSize < 1 {
		return 10
	}
	return input.PageSize
}

// ScheduleOrderData 預約訂單資料
type ScheduleOrderData struct {
	ID            string `json:"id" example:"664a73ad0e3a583c37e4b30d"`
	ShortID       string `json:"short_id" example:"#fa95"`
	PickupAddress string `json:"pickup_address" example:"台北101"`
	OriText       string `json:"ori_text" example:"W0/台北101 14:30"`
	ScheduledTime string `json:"scheduled_time" example:"2025-09-21 14:30:00"`
	Fleet         string `json:"fleet" example:"right_taxi"`
	Status        string `json:"status" example:"waiting" doc:"訂單狀態"`
	CreatedAt     string `json:"created_at" example:"2025-09-21 12:00:00"`
}

// GetScheduleOrdersResponse 獲取預約訂單列表回應
type GetScheduleOrdersResponse struct {
	Body struct {
		Orders []ScheduleOrderData `json:"orders"`
		Total  int64               `json:"total" example:"25"`
	} `json:"body"`
}

// CalcDistanceAndMinsInput 重新計算距離和時間輸入
type CalcDistanceAndMinsInput struct {
	OrderID string `path:"id" doc:"訂單ID" example:"664a73ad0e3a583c37e4b30d"`
}

// CalcDistanceAndMinsData 重新計算距離和時間回應資料
type CalcDistanceAndMinsData struct {
	OrderID       string  `json:"order_id" example:"664a73ad0e3a583c37e4b30d"`
	DistanceKm    float64 `json:"distance_km" example:"5.2" doc:"距離（公里）"`
	EstPickupMins int     `json:"est_pickup_mins" example:"15" doc:"預估到達時間（分鐘）"`
	EstPickupTime string  `json:"est_pickup_time" example:"14:45:00" doc:"預估到達時間"`
	UpdatedAt     string  `json:"updated_at" example:"2025-09-22 14:30:00" doc:"更新時間"`
}

// CalcDistanceAndMinsResponse 重新計算距離和時間回應
type CalcDistanceAndMinsResponse struct {
	Body CalcDistanceAndMinsData `json:"body"`
}
