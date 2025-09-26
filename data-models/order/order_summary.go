package order

import (
	"right-backend/data-models/common"
	"right-backend/model"
)

// GetOrderSummaryInput 獲取訂單報表列表輸入
type GetOrderSummaryInput struct {
	common.BasePaginationInput
	Fleet         string `query:"fleet" doc:"車隊過濾，必須" validate:"required"`
	StartDate     string `query:"startDate" doc:"開始日期 (YYYY-MM-DD)，可選"`
	EndDate       string `query:"endDate" doc:"結束日期 (YYYY-MM-DD)，可選"`
	Sort          string `query:"sort" doc:"排序欄位，格式：欄位名:asc|desc，可選，預設:created_at:desc" default:"created_at:desc"`
	OrderID       string `query:"orderID" doc:"訂單號碼過濾 (支援完整ID或shortId格式#xxxxx)，可選"`
	Status        string `query:"status" doc:"狀態過濾，可選，支援多個狀態以逗號分隔"`
	PickupAddress string `query:"pickupAddress" doc:"上車地點過濾，可選"`
	Driver        string `query:"driver" doc:"司機過濾 (司機姓名)，可選"`
	CustomerGroup string `query:"customerGroup" doc:"客群過濾，可選"`
	PassengerID   string `query:"passengerID" doc:"乘客ID過濾，可選"`
}

// OrderSummaryIDInput 訂單報表ID輸入
type OrderSummaryIDInput struct {
	ID string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"訂單ID"`
}

// DriverWithJkoAccount 擴展的司機資訊，包含街口帳號和車輛顏色
type DriverWithJkoAccount struct {
	model.Driver
	JkoAccount string `json:"jko_account,omitempty" doc:"司機街口帳號"`
	CarColor   string `json:"car_color,omitempty" doc:"車輛顏色"`
}

// OrderSummaryItem 訂單報表項目，包含擴展的司機資訊
type OrderSummaryItem struct {
	*model.Order `bson:",inline"`
	Driver       DriverWithJkoAccount `json:"driver,omitempty" bson:"driver,omitempty"`
}

// GetOrderSummaryResponse 獲取訂單報表列表回應
type GetOrderSummaryResponse struct {
	Body struct {
		Data       []*OrderSummaryItem   `json:"data" doc:"訂單列表"`
		Pagination common.PaginationInfo `json:"pagination" doc:"分頁資訊"`
	} `json:"body"`
}

// OrderSummaryResponse 單個訂單報表回應
type OrderSummaryResponse struct {
	Body *model.Order `json:"order"`
}

// CreateOrderSummaryInput 創建訂單輸入
type CreateOrderSummaryInput struct {
	Body model.Order `json:"order" doc:"訂單資料"`
}

// UpdateOrderSummaryInput 更新訂單輸入
type UpdateOrderSummaryInput struct {
	ID   string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"訂單ID"`
	Body struct {
		Type          *model.OrderType   `json:"type,omitempty" doc:"訂單類型"`
		Status        *model.OrderStatus `json:"status,omitempty" doc:"訂單狀態"`
		Amount        *int               `json:"amount,omitempty" doc:"金額"`
		AmountNote    *string            `json:"amount_note,omitempty" doc:"金額備註"`
		PassengerID   *string            `json:"passenger_id,omitempty" doc:"乘客ID"`
		CustomerGroup *string            `json:"customer_group,omitempty" doc:"客群"`
		Customer      *model.Customer    `json:"customer,omitempty" doc:"客戶資訊"`
	} `json:"body"`
}
