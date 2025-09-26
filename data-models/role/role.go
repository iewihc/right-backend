package role

import (
	"right-backend/data-models/common"
	"right-backend/model"
)

// CreateRoleInput 創建角色輸入
type CreateRoleInput struct {
	Body struct {
		Name        string             `json:"name" minLength:"1" maxLength:"50" example:"自定義管理員" doc:"角色名稱"`
		TagColor    string             `json:"tag_color" minLength:"1" maxLength:"20" example:"#FF6B6B" doc:"標籤顏色"`
		FleetAccess model.FleetAccess  `json:"fleet_access" example:"全部" doc:"可檢視的車隊" enum:"全部,自家"`
		Permissions []model.Permission `json:"permissions" doc:"權限列表"`
	} `json:"body"`
}

// RoleResponse 角色回應
type RoleResponse struct {
	Body *model.Role `json:"role"`
}

// RolesResponse 角色列表回應
type RolesResponse struct {
	Body []*model.Role `json:"roles"`
}

// PaginatedRolesResponse 分頁角色列表回應
type PaginatedRolesResponse struct {
	Body struct {
		Roles      []*model.Role         `json:"roles" doc:"角色列表"`
		Pagination common.PaginationInfo `json:"pagination" doc:"分頁資訊"`
	} `json:"body"`
}

// GetRolesInput 獲取角色列表輸入
type GetRolesInput struct {
	common.BasePaginationInput
	IncludeSystem bool   `query:"include_system" example:"true" doc:"是否包含系統角色，預設為 false"`
	Search        string `query:"search" example:"管理" doc:"模糊搜尋角色名稱"`
	Fleet         string `query:"fleet" example:"RSK" doc:"根據車隊過濾角色" enum:"RSK,KD,WEI"`
}

// RoleIDInput 角色ID輸入
type RoleIDInput struct {
	ID string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"角色ID"`
}

// UpdateRoleInput 更新角色輸入
type UpdateRoleInput struct {
	ID   string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"角色ID"`
	Body struct {
		Name        string             `json:"name,omitempty" minLength:"1" maxLength:"50" example:"自定義管理員" doc:"角色名稱"`
		TagColor    string             `json:"tag_color,omitempty" minLength:"1" maxLength:"20" example:"#FF6B6B" doc:"標籤顏色"`
		FleetAccess model.FleetAccess  `json:"fleet_access,omitempty" example:"全部" doc:"可檢視的車隊" enum:"全部,自家"`
		Permissions []model.Permission `json:"permissions,omitempty" doc:"權限列表"`
	} `json:"body"`
}

// DeleteRoleInput 刪除角色輸入
type DeleteRoleInput struct {
	ID string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"角色ID"`
}

// DeleteRoleResponse 刪除角色回應
type DeleteRoleResponse struct {
	Body struct {
		Message string `json:"message" example:"角色已刪除" doc:"操作結果訊息"`
		RoleID  string `json:"role_id" example:"507f1f77bcf86cd799439011" doc:"被刪除的角色ID"`
	} `json:"body"`
}
