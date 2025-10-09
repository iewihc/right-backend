package common

// APIResponse 標準化的 API 回傳結構
type APIResponse[T any] struct {
	Success bool   `json:"success" doc:"請求是否成功"`
	Message string `json:"message" doc:"回傳訊息"`
	Data    *T     `json:"data,omitempty" doc:"回傳資料"`
	Error   string `json:"error,omitempty" doc:"錯誤訊息"`
}

// SuccessResponse 成功回傳
func SuccessResponse[T any](message string, data *T) *APIResponse[T] {
	return &APIResponse[T]{
		Success: true,
		Message: message,
		Data:    data,
	}
}

// ErrorResponse 錯誤回傳
func ErrorResponse[T any](message string, errorMsg string) *APIResponse[T] {
	return &APIResponse[T]{
		Success: false,
		Message: message,
		Error:   errorMsg,
	}
}

// AuthAppVersionInput App版本驗證輸入
type AuthAppVersionInput struct {
	Body struct {
		AppVersion string `json:"app_version" doc:"App版本號" example:"1.0.0(102)"`
	} `json:"body"`
}

// AuthAppVersionData App版本驗證回傳資料
type AuthAppVersionData struct {
	Valid          bool   `json:"valid" example:"true" doc:"版本是否有效"`
	CurrentVersion string `json:"current_version" example:"1.0.0(102)" doc:"當前要求的版本"`
	ClientVersion  string `json:"client_version" example:"1.0.0(101)" doc:"客戶端版本"`
}

// AuthAppVersionResponse App版本驗證回傳
type AuthAppVersionResponse struct {
	Body APIResponse[AuthAppVersionData] `json:"body"`
}
