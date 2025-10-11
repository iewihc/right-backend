package order

import "mime/multipart"

// ImportOrdersCheckInput 匯入訂單檢查輸入
type ImportOrdersCheckInput struct {
	Fleet     string         `query:"fleet" doc:"車隊名稱，必須" validate:"required"`
	HasHeader bool           `query:"hasHeader" doc:"Excel 是否包含標題行，預設為 true" default:"true"`
	RawBody   multipart.Form `contentType:"multipart/form-data"`
}

// ImportOrdersCheckResponse 匯入訂單檢查回應
type ImportOrdersCheckResponse struct {
	Body struct {
		CurrentOrdersCount int `json:"current_orders_count" doc:"當前訂單總數量"`
		ImportOrdersCount  int `json:"import_orders_count" doc:"預計要匯入的數量"`
	}
}

// ImportOrdersInput 匯入訂單輸入
type ImportOrdersInput struct {
	Fleet     string         `query:"fleet" doc:"車隊名稱，必須" validate:"required"`
	HasHeader bool           `query:"hasHeader" doc:"Excel 是否包含標題行，預設為 true" default:"true"`
	RawBody   multipart.Form `contentType:"multipart/form-data"`
}

// ImportOrdersResponse 匯入訂單回應
type ImportOrdersResponse struct {
	Body struct {
		SuccessCount int      `json:"success_count" doc:"成功匯入的訂單數量"`
		FailedCount  int      `json:"failed_count" doc:"失敗的訂單數量"`
		Errors       []string `json:"errors,omitempty" doc:"錯誤訊息列表"`
	}
}

// ExportOrdersInput 匯出訂單輸入
type ExportOrdersInput struct {
	Fleet     string `query:"fleet" doc:"車隊名稱，可選"`
	StartDate string `query:"startDate" doc:"開始日期 (YYYY-MM-DD)，可選"`
	EndDate   string `query:"endDate" doc:"結束日期 (YYYY-MM-DD)，可選"`
	HasHeader bool   `query:"hasHeader" doc:"Excel 是否包含標題行，預設為 true" default:"true"`
}

// ExcelOrderRow Excel 訂單資料行結構
type ExcelOrderRow struct {
	ShortID      string // 編號 (列1)
	OrderDate    string // 訂單日期 (列2)
	OrderTime    string // 時間 (列3)
	CustomerGroup string // 客群 (列4)
	PassengerID  string // 乘客姓名/編號 (列5)
	OriText      string // 上車地點 (列6)
	DriverName   string // 承接司機 (列7)
	Fleet        string // 車隊/取消 (列8)
	Expense      int    // 支出 (列9)
	Income       int    // 收入 (列10)
	AmountNote   string // 備註 (列11)
	SystemID     string // 系統編號 (列12)
}
