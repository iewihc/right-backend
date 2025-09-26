package order

import (
	"right-backend/data-models/common"
	"right-backend/model"
)

type GetOrdersInput struct {
	Limit int64 `query:"limit" default:"100" doc:"每頁的訂單數量"`
	Skip  int64 `query:"skip" default:"0" doc:"跳過的訂單數量"`
}

type GetDispatchOrdersInput struct {
	common.BasePaginationInput
	StartDate     string `query:"startDate" doc:"開始日期 (YYYY-MM-DD)，可選"`
	EndDate       string `query:"endDate" doc:"結束日期 (YYYY-MM-DD)，可選"`
	Fleet         string `query:"fleet" doc:"車隊過濾 (RSK/KD/WEI)，可選"`
	CustomerGroup string `query:"customerGroup" doc:"客群過濾，可選"`
	Status        string `query:"status" doc:"狀態過濾，可選，支援多個狀態以逗號分隔 (例：完成,取消)"`
	PickupAddress string `query:"pickupAddress" doc:"上車地點過濾，可選"`
	OrderID       string `query:"orderID" doc:"訂單號碼過濾 (支援完整ID或shortId格式#xxxxx)，可選"`
	Driver        string `query:"driver" doc:"司機過濾 (司機姓名)，可選"`
	Sort          string `query:"sort" doc:"排序欄位，格式：欄位名:asc|desc，可選，預設:created_at:desc" default:"created_at:desc"`
}

type CreateOrderInput struct {
	Body model.Order `json:"order" doc:"僅必填欄位，其他選填"`
}

type OrderResponse struct {
	Body *model.Order `json:"order"`
}

type DispatchCancelInput struct {
	OrderID string `path:"orderID" doc:"訂單ID"`
	Body    *struct {
		CancelReason string `json:"cancel_reason,omitempty" doc:"取消原因" example:"調度取消"`
	} `json:"body,omitempty"`
}

type DispatchCancelData struct {
	Success bool   `json:"success" example:"true"`
	Message string `json:"message" example:"取消中訂單已記錄到 Redis"`
}

type DispatchCancelResponse struct {
	Body DispatchCancelData `json:"body"`
}

type OrdersResponse struct {
	Body []*model.Order `json:"orders"`
}

type OrderIDInput struct {
	ID string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"訂單ID"`
}

type UpdateOrderStatusInput struct {
	ID   string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"訂單ID"`
	Body struct {
		Status model.OrderStatus `json:"status" doc:"訂單狀態" example:"完成"`
	} `json:"body"`
}

// SimpleCreateOrderInput is the input for creating a single order from a text line.
type SimpleCreateOrderInput struct {
	Body struct {
		OriText string `json:"oriText" huma:"description=A single line of text representing one order" validate:"required"`
		Fleet   string `json:"fleet" huma:"description=Fleet type for the order" example:"RSK" validate:"required" enum:"RSK,KD,WEI"`
	} `json:"body"`
}

// AddressSuggestionsResponse is returned when multiple address suggestions are found
type AddressSuggestionsResponse struct {
	Body struct {
		Message     string        `json:"message" doc:"說明訊息"`
		Suggestions []interface{} `json:"suggestions" doc:"地址建議列表"`
	} `json:"body"`
}

// SimpleCreateOrderResponse can be either an order or address suggestions
type SimpleCreateOrderResponse struct {
	Body interface{} `json:"body"`
}

// OrderWithShortID extends model.Order with a short ID for dispatch orders
type OrderWithShortID struct {
	*model.Order
	ShortID      string            `json:"short_id" doc:"訂單簡短ID，格式為 #+訂單後五碼"`
	DriverInfo   string            `json:"driver_info" doc:"司機資訊，格式為：車隊-司機編號-司機名稱"`
	DriverDetail *model.DriverInfo `json:"driver_detail,omitempty" doc:"司機完整資訊，包含車輛顏色等詳細信息"`
}

type GetDispatchOrdersResponse struct {
	Body struct {
		Data       []*OrderWithShortID   `json:"data" doc:"訂單列表"`
		Pagination common.PaginationInfo `json:"pagination" doc:"分頁資訊"`
	} `json:"body"`
}

type UpdateDispatchOrderStatusInput struct {
	ID   string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"訂單ID"`
	Body struct {
		HasCopy   *bool `json:"has_copy,omitempty" doc:"是否已複製，可選"`
		HasNotify *bool `json:"has_notify,omitempty" doc:"是否已通知，可選"`
	} `json:"body"`
}

type CheckOrderResponse struct {
	Body struct {
		OrderID  string `json:"order_id" doc:"訂單ID"`
		DriverID string `json:"driver_id" doc:"司機ID"`
		OriText  string `json:"ori_text" doc:"原始文字"`
		IsExpire bool   `json:"is_expire" doc:"是否已過期"`
		Found    bool   `json:"found" doc:"是否找到推送記錄"`
	} `json:"body"`
}

type GetScheduleOrdersInput struct {
	common.BasePaginationInput
}

type GetScheduleOrdersResponse struct {
	Body struct {
		Data       []*OrderWithShortID   `json:"data" doc:"預約訂單列表"`
		Pagination common.PaginationInfo `json:"pagination" doc:"分頁資訊"`
	} `json:"body"`
}

// AddressSuggestionError 包含多個地址建議的錯誤類型
type AddressSuggestionError struct {
	Message     string        `json:"message"`
	Suggestions []interface{} `json:"suggestions"`
}

func (e *AddressSuggestionError) Error() string {
	return e.Message
}

// GetScheduleOrderCountInput 獲取預約單數量輸入
type GetScheduleOrderCountInput struct {
}

// GetScheduleOrderCountData 預約單數量資料
type GetScheduleOrderCountData struct {
	Count int `json:"count" example:"15" doc:"當前可接預約單數量"`
}

// GetScheduleOrderCountResponse 獲取預約單數量回應
type GetScheduleOrderCountResponse struct {
	Body common.APIResponse[GetScheduleOrderCountData] `json:"body"`
}

// CurrentOrderWithDriverStatusResponse 當前訂單與司機狀態回應
type CurrentOrderWithDriverStatusResponse struct {
	Body struct {
		Order        *model.Order `json:"order" doc:"當前訂單資訊"`
		DriverStatus string       `json:"driver_status" doc:"司機當前狀態" example:"前往中"`
	} `json:"body"`
}
