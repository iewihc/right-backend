package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Role 角色模型
type Role struct {
	ID          primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty" example:"684a73ad0e3a583c37e4b30d" doc:"角色ID"`
	Name        UserRole           `json:"name" bson:"name" example:"自定義管理員" doc:"角色名稱"`
	TagColor    string             `json:"tag_color" bson:"tag_color" example:"#FF6B6B" doc:"標籤顏色"`
	Fleet       *FleetType         `json:"fleet,omitempty" bson:"fleet,omitempty" example:"RSK" doc:"專屬車隊（為空時表示適用於所有車隊）"`
	FleetAccess FleetAccess        `json:"fleet_access" bson:"fleet_access" example:"全部" doc:"可檢視的車隊"`
	Permissions []Permission       `json:"permissions" bson:"permissions" doc:"權限列表"`
	IsSystem    bool               `json:"is_system" bson:"is_system" example:"false" doc:"是否為系統預設角色"`
	IsActive    bool               `json:"is_active" bson:"is_active" example:"true" doc:"是否啟用"`
	CreatedAt   time.Time          `json:"created_at" bson:"created_at" doc:"建立時間"`
	UpdatedAt   time.Time          `json:"updated_at" bson:"updated_at" doc:"更新時間"`
}

// GetSystemRoles 獲取系統預設角色列表
func GetSystemRoles() []UserRole {
	return []UserRole{
		RoleSystemAdmin,
		RoleModerator,
		RoleAdmin,
		RoleDispatcher,
		RoleNone,
	}
}

// IsSystemRole 檢查是否為系統預設角色
func IsSystemRole(role UserRole) bool {
	systemRoles := GetSystemRoles()
	for _, systemRole := range systemRoles {
		if role == systemRole {
			return true
		}
	}
	return false
}

// CreateSystemRole 創建系統角色記錄
func CreateSystemRole(role UserRole) *Role {
	return &Role{
		ID:          primitive.NewObjectID(),
		Name:        role,
		TagColor:    getDefaultTagColor(role),
		FleetAccess: GetDefaultFleetAccess(role),
		Permissions: GetDefaultPermissions(role),
		IsSystem:    true,
		IsActive:    true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// getDefaultTagColor 獲取系統角色的預設標籤顏色
func getDefaultTagColor(role UserRole) string {
	switch role {
	case RoleSystemAdmin:
		return "#FF4D4F" // 紅色
	case RoleModerator:
		return "#FA8C16" // 橙色
	case RoleAdmin:
		return "#1890FF" // 藍色
	case RoleDispatcher:
		return "#52C41A" // 綠色
	case RoleNone:
		return "#D9D9D9" // 灰色
	default:
		return "#722ED1" // 紫色
	}
}
