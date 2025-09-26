package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Permission string

const (
	// 基本功能
	PermissionDashboard        Permission = "Dashboard"
	PermissionFavoriteLocation Permission = "收藏位置"
	PermissionCommonAddress    Permission = "常用地址列表"

	// 調度功能
	PermissionRSKDispatch Permission = "RSK調度"
	PermissionKDDispatch  Permission = "KD調度"
	PermissionWEIDispatch Permission = "WEI調度"
	PermissionDispatch    Permission = "調度派單"

	// 報表功能
	PermissionOrderReport     Permission = "訂單報表"
	PermissionRSKReport       Permission = "RSK訂單報表"
	PermissionKDReport        Permission = "KD訂單報表"
	PermissionWEIReport       Permission = "WEI訂單報表"
	PermissionOperationReport Permission = "營運報表"

	// 管理功能
	PermissionDriverList  Permission = "司機列表"
	PermissionVehicleList Permission = "車輛款式列表"
)

type FleetAccess string

const (
	FleetAccessAll FleetAccess = "全部"
	FleetAccessOwn FleetAccess = "自家"
)

type User struct {
	ID          primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty" example:"684a73ad0e3a583c37e4b30d" doc:"用戶ID"`
	Name        string             `json:"name" bson:"name" example:"Admin" doc:"姓名"`
	Account     string             `json:"account" bson:"account" example:"admin@taxi.com" doc:"帳號"`
	Password    string             `json:"password" bson:"password" example:"admin123" doc:"密碼"`
	Role        UserRole           `json:"role" bson:"role" example:"系統管理員" doc:"角色"`
	Fleet       FleetType          `json:"fleet" bson:"fleet" example:"RSK" doc:"所屬車隊"`
	Permissions []Permission       `json:"permissions" bson:"permissions" doc:"權限列表"`
	FleetAccess FleetAccess        `json:"fleet_access" bson:"fleet_access" example:"全部" doc:"可檢視的車隊"`
	IsActive    bool               `json:"is_active" bson:"is_active" example:"true" doc:"是否啟用"`
	CreatedAt   time.Time          `json:"created_at" bson:"created_at" doc:"建立時間"`
	UpdatedAt   time.Time          `json:"updated_at" bson:"updated_at" doc:"更新時間"`
}

// GetDefaultPermissions 根據角色返回預設權限
func GetDefaultPermissions(role UserRole) []Permission {
	switch role {
	case RoleSystemAdmin, RoleModerator:
		return []Permission{
			PermissionDashboard,
			PermissionRSKDispatch,
			PermissionKDDispatch,
			PermissionWEIDispatch,
			PermissionFavoriteLocation,
			PermissionOrderReport,
			PermissionRSKReport,
			PermissionKDReport,
			PermissionWEIReport,
			PermissionDriverList,
			PermissionVehicleList,
		}
	case RoleAdmin:
		return []Permission{
			PermissionDashboard,
			PermissionDispatch,
			PermissionOrderReport,
			PermissionOperationReport,
			PermissionDriverList,
		}
	case RoleDispatcher:
		return []Permission{
			PermissionDashboard,
			PermissionDispatch,
			PermissionCommonAddress,
		}
	default:
		return []Permission{}
	}
}

// GetDefaultFleetAccess 根據角色返回預設車隊檢視權限
func GetDefaultFleetAccess(role UserRole) FleetAccess {
	switch role {
	case RoleSystemAdmin, RoleModerator:
		return FleetAccessAll
	case RoleAdmin, RoleDispatcher:
		return FleetAccessOwn
	default:
		return FleetAccessOwn
	}
}

// CanCreateRole 檢查是否可以創建指定角色的用戶
func CanCreateRole(currentUserRole UserRole, targetRole UserRole) bool {
	switch currentUserRole {
	case RoleSystemAdmin:
		// 系統管理員可以創建所有角色
		return targetRole == RoleSystemAdmin || targetRole == RoleModerator || targetRole == RoleAdmin || targetRole == RoleDispatcher
	case RoleModerator:
		// 版主可以創建版主、管理員、調度
		return targetRole == RoleModerator || targetRole == RoleAdmin || targetRole == RoleDispatcher
	case RoleAdmin:
		// 管理員只能創建調度
		return targetRole == RoleDispatcher
	case RoleDispatcher:
		// 調度只能創建調度
		return targetRole == RoleDispatcher
	default:
		return false
	}
}

// CanRemoveRole 檢查是否可以移除角色
func CanRemoveRole(currentUserRole UserRole, targetRole UserRole) bool {
	// 系統管理員可以移除版主
	if currentUserRole == RoleSystemAdmin && targetRole == RoleModerator {
		return true
	}

	// 同階層可以移除該人的身份/降級
	roleHierarchy := map[UserRole]int{
		RoleSystemAdmin: 4,
		RoleModerator:   3,
		RoleAdmin:       2,
		RoleDispatcher:  1,
	}

	currentLevel := roleHierarchy[currentUserRole]
	targetLevel := roleHierarchy[targetRole]

	// 只能移除同級或低級的角色，不能升級
	return currentLevel >= targetLevel
}
