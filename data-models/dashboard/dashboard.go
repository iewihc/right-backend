package dashboard

import (
	"right-backend/data-models/common"
	"right-backend/model"
	"time"
)

type DashboardStats struct {
	TodayOrders          OrderStats       `json:"todayOrders"`
	ReservationOrders    ReservationStats `json:"reservationOrders"`
	OnlineDrivers        DriverStats      `json:"onlineDrivers"`
	OfflineDrivers       DriverStats      `json:"offlineDrivers"`
	IdleDrivers          DriverStats      `json:"idleDrivers"`
	PickingUpDrivers     int              `json:"pickingUpDrivers"`
	ExecutingTaskDrivers int              `json:"executingTaskDrivers"`
}

type OrderStats struct {
	SuccessCount int `json:"successCount"`
	TotalCount   int `json:"totalCount"`
}

type ReservationStats struct {
	Count int `json:"count"`
}

type DriverStats struct {
	Count int `json:"count"`
}

type DashboardStatsResponse struct {
	Body *DashboardStats `json:"stats"`
}

type GetAllDriversResponse struct {
	Body struct {
		Drivers    []model.DriverInfo `json:"drivers"`
		TotalCount int                `json:"totalCount"`
	} `json:"body"`
}

type GetDriverOrdersInput struct {
	common.BaseSearchPaginationInput
	DriverStatus string `query:"driverStatus" doc:"司機狀態過濾：all(全部)/閒置/前往上車點/司機抵達/執行任務，預設為 all"`
}

// DriverOrderItem 司機訂單項目
type DriverOrderItem struct {
	// 司機資訊
	DriverInfo   string `json:"driverInfo" doc:"司機資訊：車牌 | 司機名稱" example:"ABC-5808(黑) | 料理鼠王"`
	DriverFleet  string `json:"driverFleet" doc:"司機車隊"`
	DriverStatus string `json:"driverStatus" doc:"司機狀態"`

	// 即時訂單資訊（當司機有即時訂單時）
	OrderID        string     `json:"orderId,omitempty" doc:"即時訂單ID"`
	ShortID        string     `json:"shortId,omitempty" doc:"訂單短ID"`
	OrderStatus    string     `json:"orderStatus,omitempty" doc:"訂單狀態"`
	PickupAddress  string     `json:"pickupAddress,omitempty" doc:"上車地點"`
	OriText        string     `json:"ori_text,omitempty" doc:"原始文字"`
	OriTextDisplay string     `json:"ori_text_display,omitempty" doc:"顯示文字"`
	Hints          string     `json:"hints,omitempty" doc:"備註"`
	Time           *time.Time `json:"time,omitempty" doc:"訂單時間"`

	// 預約訂單資訊（當司機有預約訂單時）
	ScheduleOrderID     string     `json:"scheduleOrderId,omitempty" doc:"預約訂單ID"`
	ScheduleShortID     string     `json:"scheduleShortId,omitempty" doc:"預約訂單短ID"`
	ScheduleOrderStatus string     `json:"scheduleOrderStatus,omitempty" doc:"預約訂單狀態"`
	ScheduleOriText     string     `json:"scheduleOriText,omitempty" doc:"預約訂單原始文字"`
	SchedulePickup      string     `json:"schedulePickupAddress,omitempty" doc:"預約訂單上車地點"`
	ScheduleTime        *time.Time `json:"scheduleTime,omitempty" doc:"預約時間"`
	ScheduleHints       string     `json:"scheduleHints,omitempty" doc:"預約訂單備註"`
}

type GetDriverOrdersResponse struct {
	Body struct {
		Data       []DriverOrderItem     `json:"data" doc:"司機訂單列表"`
		Pagination common.PaginationInfo `json:"pagination" doc:"分頁資訊"`
	} `json:"body"`
}

// 司機週接單排行榜相關結構
type DriverWeeklyOrderRank struct {
	DriverID   string `json:"driver_id" bson:"_id" doc:"司機ID"`
	DriverName string `json:"driver_name" bson:"driver_name" doc:"司機姓名"`
	CarPlate   string `json:"car_plate" bson:"car_plate" doc:"車牌號碼"`
	Fleet      string `json:"fleet" bson:"fleet" doc:"所屬車隊"`
	OrderCount int    `json:"order_count" bson:"order_count" doc:"接單數量"`
	Rank       int    `json:"rank" doc:"排名"`
}

type GetDriverWeeklyOrderRanksInput struct {
	WeekOffset int `query:"week_offset" doc:"週數偏移，0為本週，-1為上週，1為下週" example:"0"`
	PageNum    int `query:"page_num" doc:"頁碼" example:"1"`
	PageSize   int `query:"page_size" doc:"每頁筆數" example:"20"`
}

func (input *GetDriverWeeklyOrderRanksInput) GetPageNum() int {
	if input.PageNum <= 0 {
		return 1
	}
	return input.PageNum
}

func (input *GetDriverWeeklyOrderRanksInput) GetPageSize() int {
	if input.PageSize <= 0 {
		return 20
	}
	if input.PageSize > 100 {
		return 100
	}
	return input.PageSize
}

type GetDriverWeeklyOrderRanksResponseData struct {
	Ranks      []DriverWeeklyOrderRank `json:"ranks" doc:"司機週接單排行榜"`
	WeekStart  string                  `json:"week_start" doc:"本週開始日期"`
	WeekEnd    string                  `json:"week_end" doc:"本週結束日期"`
	Pagination common.PaginationInfo   `json:"pagination" doc:"分頁資訊"`
}

type GetDriverWeeklyOrderRanksResponse struct {
	Body GetDriverWeeklyOrderRanksResponseData `json:"body"`
}
