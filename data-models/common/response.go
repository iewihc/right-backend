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
