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
	DriverStatus string `query:"driverStatus" doc:"司機狀態過濾：all(全部)/enroute(前往載客)/executing(執行任務)/idle(空車)，預設為 all"`
}

// DashboardItem 統一的 dashboard 項目，可以是訂單或司機狀態
type DashboardItem struct {
	Type string     `json:"type" doc:"類型：order(訂單) 或 driver(司機狀態)"`
	Time *time.Time `json:"time" doc:"時間（訂單時間或司機狀態更新時間）"`

	// 訂單相關欄位（當 Type = "order" 時使用）
	OrderID        string `json:"orderId,omitempty"`
	ShortID        string `json:"shortId,omitempty"`
	OrderStatus    string `json:"orderStatus,omitempty"`
	PickupAddress  string `json:"pickupAddress,omitempty"`
	OriText        string `json:"ori_text,omitempty"`
	OriTextDisplay string `json:"ori_text_display,omitempty"`
	Hints          string `json:"hints,omitempty"`

	// 司機相關欄位（兩種類型都會有）
	DriverStatus string `json:"driverStatus,omitempty" doc:"司機狀態"`
	DriverInfo   string `json:"driverInfo,omitempty" doc:"司機資訊：車隊|司機編號|司機名稱"`
}

type GetDriverOrdersResponse struct {
	Body struct {
		Data       []DashboardItem       `json:"data" doc:"司機訂單和狀態列表"`
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
