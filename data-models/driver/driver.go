package driver

import (
	"mime/multipart"
	"right-backend/data-models/common"
	"right-backend/model"
)

// DriverWithDist 帶距離信息的司機結構
type DriverWithDist struct {
	Driver *model.DriverInfo `json:"driver"`
	Dist   float64           `json:"distance"`
}

type CreateDriverInput struct {
	Body model.DriverInfo `json:"driver" example:"{\"account\":\"driver001@taxi.com\",\"password\":\"driver123\",\"car_plate\":\"6793黑\",\"fleet\":\"RSK\",\"car_model\":\"納智捷S5\",\"referrer\":\"推薦人A\",\"jko_account\":\"jko001\",\"joined_on\":\"2025-06-07T00:00:00Z\",\"reject_count\":0,\"completed_orders\":0,\"lat\":\"25.0675657\",\"lng\":\"121.5526993\",\"fcm_token\":\"token123\",\"is_online\":true,\"is_active\":true,\"status\":\"active\"}"`
}

type DriverResponse struct {
	Body *model.DriverInfo `json:"driver" example:"{\"_id\":\"684a73ad0e3a583c37e4b30d\",\"name\":\"王小明\",\"account\":\"driver001@taxi.com\",\"car_plate\":\"6793黑\",\"fleet\":\"RSK\",\"car_model\":\"納智捷S5\",\"referrer\":\"推薦人A\",\"jko_account\":\"jko001\",\"joined_on\":\"2025-06-07T00:00:00Z\",\"reject_count\":0,\"completed_orders\":0,\"lat\":\"25.0675657\",\"lng\":\"121.5526993\",\"fcm_token\":\"token123\",\"is_online\":true,\"is_active\":true,\"status\":\"active\"}"`
}

type DriversResponse struct {
	Body []*model.DriverInfo `json:"drivers"`
}

type DriverStatusResponse struct {
	Body struct {
		OrderId             *string `json:"order_id,omitempty" example:"684a73ad0e3a583c37e4b30d" doc:"訂單ID，無單時為null"`
		ScheduleOrderId     *string `json:"schedule_order_id,omitempty" example:"684a73ad0e3a583c37e4b30d" doc:"預約訂單ID，無預約單時為null"`
		OrderStatus         *string `json:"order_status,omitempty" example:"司機接單" doc:"當前訂單狀態，無單時為null"`
		ScheduleOrderStatus *string `json:"schedule_order_status,omitempty" example:"預約中" doc:"預約訂單狀態，無預約單時為null"`
		DriverStatus        string  `json:"driver_status" example:"閒置" doc:"司機當前狀態"`
	} `json:"body"`
}

type DriverIDInput struct {
	ID string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"司機ID"`
}

type UpdateLocationInput struct {
	ID   string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"司機ID"`
	Body struct {
		Lat string `json:"lat" doc:"緯度" example:"\"25.0675657\""`
		Lng string `json:"lng" doc:"經度" example:"\"121.5526993\""`
	} `json:"body"`
}

type UpdateStatusInput struct {
	Body struct {
		IsOnline bool `json:"is_online" doc:"是否在線" example:"true"`
	} `json:"body"`
}

type AcceptOrderInput struct {
	Body struct {
		OrderID    string `json:"order_id" doc:"訂單ID" example:"664a73ad0e3a583c37e4b30d"`
		AdjustMins int    `json:"adjust_mins" default:"0" doc:"調整分鐘數（預設0）" example:"5"`
	} `json:"body"`
}

type RejectOrderInput struct {」
	Body struct {
		OrderID string `json:"order_id" doc:"訂單ID" example:"664a73ad0e3a583c37e4b30d"`
	} `json:"body"`
}

type OrderIDInput struct {
	OrderID string `path:"orderId" doc:"訂單ID"`
}

type PickupCustomerInput struct {
	OrderID string `path:"orderId" doc:"訂單ID"`
	Body    struct {
		HasMeterJump bool `json:"has_meter_jump,omitempty" doc:"是否跳表，預設為false" example:"true"`
	} `json:"body"`
}

type CompleteOrderInput struct {
	Body struct {
		OrderID  string `json:"order_id" doc:"訂單ID" example:"664a73ad0e3a583c37e4b30d"`
		Duration int    `json:"duration" doc:"司機用時時間(秒)" example:"1800"`
	} `json:"body"`
}

type ArrivePickupLocationInput struct {
	Body struct {
		OrderID string `json:"order_id" doc:"訂單ID" example:"664a73ad0e3a583c37e4b30d"`
	} `json:"body"`
}

type ArrivePickupLocationResponse struct {
	Body struct {
		BaseDriverActionResponse
		DeviationSecs int    `json:"deviationSecs" example:"120" doc:"偏差秒數，正數表示遲到，負數表示提早"`
		ArrivalTime   string `json:"arrivalTime" example:"2025-09-25T14:25:00Z" doc:"實際到達時間，ISO 8601格式"`
	} `json:"body"`
}

type DriverArrivalResponse struct {
	Body struct {
		BaseDriverActionResponse
		IsPhotoTaken bool `json:"isPhotoTaken" example:"true" doc:"是否有拍照"`
	} `json:"body"`
}

type PickupCustomerResponse struct {
	Body struct {
		BaseDriverActionResponse
		HasMeterJump bool   `json:"hasMeterJump" example:"true" doc:"是否跳表"`
		PickupTime   string `json:"pickup_time" example:"2025-09-25T14:30:00Z" doc:"上車時間，ISO 8601格式"`
	} `json:"body"`
}

type CompleteOrderResponse struct {
	Body struct {
		BaseDriverActionResponse
		CompleteTime string `json:"complete_time" example:"2025-09-25T14:45:00Z" doc:"完成時間，ISO 8601格式"`
	} `json:"body"`
}

// BaseDriverActionResponse 基础司机操作响应结构
type BaseDriverActionResponse struct {
	Success      bool   `json:"success" example:"true"`
	Message      string `json:"message" example:"操作成功"`
	DriverStatus string `json:"driver_status" example:"閒置" doc:"司機當前狀態"`
	OrderStatus  string `json:"order_status" example:"已完成" doc:"訂單當前狀態"`
}

type SimpleResponse struct {
	Success bool   `json:"success" example:"true"`
	Message string `json:"message" example:"司機已成功接單"`
}

type AcceptOrderResponse struct {
	Body struct {
		BaseDriverActionResponse
		Distance             float64 `json:"distance" example:"5.2"`
		EstimatedTime        int     `json:"estimated_time" example:"15"`
		EstimatedArrivalTime string  `json:"estimated_arrival_time" example:"14:35:20"`
		AcceptanceTime       string  `json:"acceptance_time" example:"2025-09-25T14:20:00Z" doc:"接單時間，ISO 8601格式"`
	} `json:"body"`
}

type AcceptScheduledOrderInput struct {
	Body struct {
		OrderID string `json:"order_id" doc:"預約訂單ID" example:"664a73ad0e3a583c37e4b30d"`
	} `json:"body"`
}

type AcceptScheduledOrderResponse struct {
	Body struct {
		Message            string `json:"message" example:"預約單接單成功"`
		ScheduledTime      string `json:"scheduled_time" example:"2025-09-21 14:30:00" doc:"預約時間"`
		PickupAddress      string `json:"pickup_address" example:"台北101" doc:"上車地點"`
		DestinationAddress string `json:"destination_address,omitempty" example:"台北車站" doc:"目的地"`
	} `json:"body"`
}

// ActivateScheduledOrderInput 激活預約單輸入
type ActivateScheduledOrderInput struct {
	OrderID string `path:"id" doc:"預約訂單ID" example:"664a73ad0e3a583c37e4b30d"`
}

// ActivateScheduledOrderData 激活預約單回應資料
type ActivateScheduledOrderData struct {
	OrderID       string `json:"order_id" example:"664a73ad0e3a583c37e4b30d" doc:"訂單ID"`
	Status        string `json:"status" example:"前往上車點" doc:"新的訂單狀態"`
	PickupAddress string `json:"pickup_address" example:"台北市信義區信義路五段7號" doc:"上車地址"`
	EstPickupMins int    `json:"est_pickup_mins" example:"15" doc:"預計到達時間(分鐘)"`
	EstPickupTime string `json:"est_pickup_time" example:"14:45" doc:"預計到達時間"`
}

// ActivateScheduledOrderResponse 激活預約單回應
type ActivateScheduledOrderResponse struct {
	Body common.APIResponse[ActivateScheduledOrderData] `json:"body"`
}

// CurrentScheduledOrderInput 獲取當前預約單輸入（從JWT獲取司機ID）
type CurrentScheduledOrderInput struct {
}

// CurrentScheduledOrderData 當前預約單資料
type CurrentScheduledOrderData struct {
	HasScheduledOrder bool                       `json:"has_scheduled_order" example:"true" doc:"是否有預約單"`
	ScheduledOrder    *CurrentScheduledOrderInfo `json:"scheduled_order,omitempty" doc:"預約單資訊"`
}

// CurrentScheduledOrderInfo 預約單詳細資訊
type CurrentScheduledOrderInfo struct {
	OrderID            string `json:"order_id" example:"664a73ad0e3a583c37e4b30d" doc:"訂單ID"`
	ShortID            string `json:"short_id" example:"8027" doc:"訂單短ID"`
	Status             string `json:"status" example:"預約單已接受" doc:"訂單狀態"`
	OriText            string `json:"ori_text" example:"W測/麥寮農會 測試訂單請不要接喔" doc:"原始文字"`
	PickupAddress      string `json:"pickup_address" example:"638台灣雲林縣麥寮鄉中山路119號" doc:"上車地址"`
	DestinationAddress string `json:"destination_address,omitempty" example:"台北車站" doc:"目的地"`
	Remarks            string `json:"remarks" example:"測試訂單請不要接喔" doc:"備註"`
	ScheduledTime      string `json:"scheduled_time" example:"2025-09-21 14:30:00" doc:"預約時間"`
	Fleet              string `json:"fleet" example:"WEI" doc:"車隊"`
	AcceptedAt         string `json:"accepted_at" example:"2025-09-21 10:15:00" doc:"接單時間"`
}

// CurrentScheduledOrderResponse 獲取當前預約單回應
type CurrentScheduledOrderResponse struct {
	Body common.APIResponse[CurrentScheduledOrderData] `json:"body"`
}

// AvailableScheduledOrdersCountInput 獲取可用預約單數量輸入（從JWT獲取司機ID）
type AvailableScheduledOrdersCountInput struct {
}

// AvailableScheduledOrdersCountData 可用預約單數量資料
type AvailableScheduledOrdersCountData struct {
	TotalCount     int `json:"total_count" example:"15" doc:"總預約單數量"`
	AvailableCount int `json:"available_count" example:"8" doc:"可用預約單數量"`
}

// AvailableScheduledOrdersCountResponse 獲取可用預約單數量回應
type AvailableScheduledOrdersCountResponse struct {
	Body common.APIResponse[AvailableScheduledOrdersCountData] `json:"body"`
}

type UpdateDriverLocationInput struct {
	Body struct {
		Lat string `json:"lat" example:"25.0675657" doc:"緯度"`
		Lng string `json:"lng" example:"121.5526993" doc:"經度"`
	} `json:"body"`
}

type UpdateFCMTokenInput struct {
	Body struct {
		FCMToken string `json:"fcm_token" example:"fGci1ZrNTUmL0QcsvhWX8L:APA91bHu7j..." doc:"FCM推播令牌"`
		FCMType  string `json:"fcm_type" example:"fcm" enum:"fcm,expo,web,mobile" doc:"FCM令牌類型 - fcm: Google FCM, expo: Expo Push, web/mobile: 向後兼容"`
	} `json:"body"`
}

type UploadPickupCertificateInput struct {
	OrderID string `path:"orderId" doc:"訂單ID" example:"664a73ad0e3a583c37e4b30d"`
	RawBody multipart.Form
}

type RawBodyInput struct {
	RawBody multipart.Form
}

type CreateDriverTrafficLogInput struct {
	Body struct {
		Service string `json:"service" example:"google" doc:"服務名稱 (e.g., google)"`
		API     string `json:"api" example:"geocode-address" doc:"API端點名稱"`
		Params  string `json:"params" example:"{\"address\":\"台北101\"}" doc:"請求參數 (JSON string)"`
	} `json:"body"`
}

type CreateDriverTrafficLogResponse struct {
	Body *model.TrafficUsageLog `json:"traffic_usage_log"`
}

type UpdateDriverProfileInputBody struct {
	Name        *string `json:"name,omitempty" example:"王大明" doc:"新的司機姓名"`
	CarModel    *string `json:"car_model,omitempty" example:"豐田 Camry" doc:"新的車型"`
	CarAge      *int    `json:"car_age,omitempty" example:"5" doc:"新的車齡"`
	CarPlate    *string `json:"car_plate,omitempty" example:"ABC-1234" doc:"新的車牌號碼"`
	CarColor    *string `json:"car_color,omitempty" example:"白色" doc:"新的車輛顏色"`
	NewPassword *string `json:"newPassword,omitempty" example:"newpassword123" doc:"新的密碼"`
}

type UpdateDriverProfileInput struct {
	Body UpdateDriverProfileInputBody `json:"body"`
}

type GetDriverOrdersInput struct {
	common.BasePaginationInput
}

type PaginatedOrdersResponse struct {
	Body struct {
		Orders     []*model.Order        `json:"orders"`
		Pagination common.PaginationInfo `json:"pagination" doc:"分頁資訊"`
	} `json:"body"`
}

type GetDriverHistoryOrdersInput struct {
	common.BasePaginationInput
}

type DriverHistoryOrdersResponse struct {
	Body struct {
		Orders     []*model.Order        `json:"orders"`
		Pagination common.PaginationInfo `json:"pagination" doc:"分頁資訊"`
	} `json:"body"`
}

type UpdateDeviceInput struct {
	Body struct {
		DeviceModelName    string `json:"device_model_name" doc:"設備型號名稱" example:"iPhone 14 Pro"`
		DeviceDeviceName   string `json:"device_device_name" doc:"設備名稱" example:"John's iPhone"`
		DeviceBrand        string `json:"device_brand" doc:"設備品牌" example:"Apple"`
		DeviceManufacturer string `json:"device_manufacturer" doc:"設備製造商/OS版本" example:"17.0.1"`
		DeviceAppVersion   string `json:"device_app_version" doc:"應用程式版本" example:"1.2.3"`
	} `json:"body"`
}
