package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type DriverInfo struct {
	ID                     primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty" example:"684a73ad0e3a583c37e4b30d" doc:"司機ID"`
	Name                   string             `json:"name" bson:"name" example:"王小明" doc:"司機本名"`
	Nickname               string             `json:"nickname" bson:"nickname" example:"小明" doc:"司機暱稱"`
	DriverNo               string             `json:"driver_no" bson:"driver_no" example:"D001" doc:"司機編號"`
	Account                string             `json:"account" bson:"account" example:"driver001@taxi.com" doc:"司機帳號"`
	Password               string             `json:"password" bson:"password" example:"driver123" doc:"密碼"`
	CarPlate               string             `json:"car_plate" bson:"car_plate" example:"6793黑" doc:"車牌號碼"`
	Fleet                  FleetType          `json:"fleet" bson:"fleet" example:"RSK" doc:"所屬車隊"`
	CarModel               string             `json:"car_model" bson:"car_model" example:"納智捷S5" doc:"車型"`
	CarAge                 int                `json:"car_age" bson:"car_age" example:"3" doc:"車齡"`
	CarColor               string             `json:"car_color" bson:"car_color" example:"白色" doc:"車輛顏色"`
	Referrer               string             `json:"referrer" bson:"referrer" example:"推薦人A" doc:"推薦人"`
	JkoAccount             string             `json:"jko_account" bson:"jko_account" example:"jko001" doc:"JKO帳號"`
	JoinedOn               time.Time          `json:"joined_on" bson:"joined_on" doc:"加入日期"`
	LastOnline             time.Time          `json:"last_online,omitempty" bson:"last_online,omitempty" doc:"最後上線時間"`
	RejectedCount          int                `json:"rejected_count" bson:"rejected_count" example:"0" doc:"拒絕次數"`
	CompletedCount         int                `json:"completed_count" bson:"completed_count" example:"0" doc:"完成單數"`
	AcceptedCount          int                `json:"accepted_count" bson:"accepted_count" example:"0" doc:"接單次數"`
	Lat                    string             `json:"lat" bson:"lat" example:"\"25.0675657\"" doc:"當前緯度"`
	Lng                    string             `json:"lng" bson:"lng" example:"\"121.5526993\"" doc:"當前經度"`
	FcmToken               string             `json:"fcm_token" bson:"fcm_token" example:"token123" doc:"FCM推播令牌"`
	FCMType                string             `json:"fcm_type,omitempty" bson:"fcm_type,omitempty" example:"web" doc:"FCM令牌類型 (web/mobile)"`
	LineUID                string             `json:"line_uid" bson:"line_uid" example:"Uxxxxxxxx" doc:"Line用戶ID"`
	DeviceModelName        string             `json:"device_model_name,omitempty" bson:"device_model_name,omitempty" example:"iPhone15,4" doc:"設備型號名稱"`
	DeviceDeviceName       string             `json:"device_device_name,omitempty" bson:"device_device_name,omitempty" example:"My iPhone" doc:"設備名稱"`
	DeviceBrand            string             `json:"device_brand,omitempty" bson:"device_brand,omitempty" example:"Apple" doc:"設備品牌"`
	DeviceManufacturer     string             `json:"device_manufacturer,omitempty" bson:"device_manufacturer,omitempty" example:"Apple Inc." doc:"設備製造商"`
	DeviceAppVersion       string             `json:"device_app_version,omitempty" bson:"device_app_version,omitempty" example:"1.0.0" doc:"應用程式版本"`
	IsOnline               bool               `json:"is_online" bson:"is_online" example:"true" doc:"是否在線"`
	IsActive               bool               `json:"is_active" bson:"is_active" example:"true" doc:"是否啟用"`
	IsApproved             bool               `json:"is_approved" bson:"is_approved" example:"false" doc:"是否已審核"`
	Status                 DriverStatus       `json:"status" bson:"status" example:"active" doc:"司機狀態"`
	RejectList             []string           `json:"reject_list" bson:"reject_list" example:"[\"RSK\",\"KD\",\"WEI\"]" doc:"拒絕車隊列表，不接受這些車隊的訂單"`
	AvatarPath             *string            `json:"avatar_path,omitempty" bson:"avatar_path,omitempty" doc:"頭像相對路徑"`
	HasSchedule            bool               `json:"has_schedule,omitempty" bson:"has_schedule,omitempty" example:"false" doc:"是否有預約訂單"`
	ScheduledTime          *time.Time         `json:"scheduled_time,omitempty" bson:"scheduled_time,omitempty" doc:"預約時間"`
	CurrentOrderScheduleId *string            `json:"current_order_schedule_id,omitempty" bson:"current_order_schedule_id,omitempty" doc:"當前預約訂單ID"`
	CurrentOrderId         *string            `json:"current_order_id,omitempty" bson:"current_order_id,omitempty" doc:"當前即時訂單ID"`
	CreatedAt              time.Time          `json:"created_at" bson:"created_at" doc:"建立時間"`
	UpdatedAt              time.Time          `json:"updated_at" bson:"updated_at" doc:"更新時間"`
}
