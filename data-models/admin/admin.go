package admin

import (
	"right-backend/data-models/common"
	"right-backend/model"
)

// GetApprovalDriverInput 獲取待審核司機列表的輸入參數
type GetApprovalDriverInput struct {
	common.BaseSearchPaginationInput
	Fleet      string `query:"fleet" doc:"車隊名稱過濾（選填）"`
	HasFleet   string `query:"has_fleet" doc:"車隊狀態過濾：true(有車隊)/false(沒車隊)，不填表示全部"`
	IsApproved string `query:"is_approved" doc:"審核狀態：true(已審核)/false(未審核)，不填表示全部"`
}

// PaginatedDriversResponse 分頁司機回應
type PaginatedDriversResponse struct {
	Body struct {
		Drivers    []*model.DriverInfo   `json:"drivers" doc:"司機列表"`
		Pagination common.PaginationInfo `json:"pagination" doc:"分頁資訊"`
	} `json:"body"`
}

// ApproveDriverInput 審核司機輸入參數
type ApproveDriverInput struct {
	DriverID string `path:"driverId" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"司機ID"`
}

// AdminUpdateDriverProfileInputBody 管理員更新司機個人資料輸入參數 Body
type AdminUpdateDriverProfileInputBody struct {
	Name        *string `json:"name,omitempty" example:"王大明" doc:"新的司機姓名"`
	CarModel    *string `json:"car_model,omitempty" example:"豐田 Camry" doc:"新的車型"`
	CarAge      *int    `json:"car_age,omitempty" example:"5" doc:"新的車齡"`
	CarPlate    *string `json:"car_plate,omitempty" example:"ABC-1234" doc:"新的車牌號碼"`
	CarColor    *string `json:"car_color,omitempty" example:"白色" doc:"新的車輛顏色"`
	NewPassword *string `json:"newPassword,omitempty" example:"newpassword123" doc:"新的密碼"`
	Fleet       *string `json:"fleet,omitempty" example:"RSK" doc:"所屬車隊"`
	IsActive    *bool   `json:"is_active,omitempty" example:"true" doc:"是否啟用"`
	IsApproved  *bool   `json:"is_approved,omitempty" example:"true" doc:"是否已審核"`
}

// UpdateDriverProfileInput 管理員更新司機個人資料輸入參數
type UpdateDriverProfileInput struct {
	DriverID string                            `path:"driverId" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"司機ID"`
	Body     AdminUpdateDriverProfileInputBody `json:"body"`
}

// UpdateDriverProfileResponse 更新司機個人資料回應
type UpdateDriverProfileResponse struct {
	Body *model.DriverInfo `json:"body"`
}

// RemoveDriverFromFleetInput 移除司機車隊輸入參數
type RemoveDriverFromFleetInput struct {
	DriverID string `path:"driverId" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"司機ID"`
}

// SimpleResponse 簡單回應
type SimpleResponse struct {
	Body struct {
		Success bool   `json:"success" example:"true"`
		Message string `json:"message" example:"司機審核成功"`
	} `json:"body"`
}
