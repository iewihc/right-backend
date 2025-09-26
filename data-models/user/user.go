package user

import (
	"right-backend/data-models/common"
	"right-backend/model"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// UserWithoutPassword 不包含密碼的用戶模型，用於 API 回應
type UserWithoutPassword struct {
	ID          primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty" example:"684a73ad0e3a583c37e4b30d" doc:"用戶ID"`
	Name        string             `json:"name" bson:"name" example:"Admin" doc:"姓名"`
	Account     string             `json:"account" bson:"account" example:"admin@taxi.com" doc:"帳號"`
	Role        model.UserRole     `json:"role" bson:"role" example:"系統管理員" doc:"角色"`
	Fleet       model.FleetType    `json:"fleet" bson:"fleet" example:"RSK" doc:"所屬車隊"`
	Permissions []model.Permission `json:"permissions" bson:"permissions" doc:"權限列表"`
	FleetAccess model.FleetAccess  `json:"fleet_access" bson:"fleet_access" example:"全部" doc:"可檢視的車隊"`
	IsActive    bool               `json:"is_active" bson:"is_active" example:"true" doc:"是否啟用"`
	CreatedAt   time.Time          `json:"created_at" bson:"created_at" doc:"建立時間"`
	UpdatedAt   time.Time          `json:"updated_at" bson:"updated_at" doc:"更新時間"`
}

type CreateUserInput struct {
	Body struct {
		Account  string          `json:"account" minLength:"1" maxLength:"100" example:"admin@taxi.com" doc:"帳號"`
		Password string          `json:"password" minLength:"1" maxLength:"100" example:"admin123" doc:"密碼"`
		Name     string          `json:"name" minLength:"1" maxLength:"50" example:"管理員" doc:"姓名"`
		Fleet    model.FleetType `json:"fleet" example:"RSK" doc:"所屬車隊" enum:"RSK,KD,WEI"`
		Role     model.UserRole  `json:"role" example:"管理員" doc:"角色" enum:"系統管理員,版主,管理員,調度"`
	} `json:"body"`
}

type UserResponse struct {
	Body *model.User `json:"user"`
}

type CreateUserResponse struct {
	Body *UserWithoutPassword `json:"user"`
}

type UsersResponse struct {
	Body []*UserWithoutPassword `json:"users"`
}

type PaginatedUsersResponse struct {
	Body struct {
		Users      []*UserWithoutPassword `json:"users" doc:"用戶列表"`
		Pagination common.PaginationInfo  `json:"pagination" doc:"分頁資訊"`
	} `json:"body"`
}

type GetUsersInput struct {
	common.BasePaginationInput
	Fleet  string `query:"fleet" example:"RSK" doc:"車隊篩選: 全部, RSK, KD, WEI，空值表示全部"`
	Search string `query:"search" example:"admin" doc:"模糊搜尋用戶姓名和帳號"`
}

type UserIDInput struct {
	ID string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"用戶ID"`
}

type ChangePasswordInput struct {
	ID   string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"用戶ID"`
	Body struct {
		OldPassword string `json:"old_password" minLength:"1" maxLength:"100" example:"oldpassword123" doc:"舊密碼"`
		NewPassword string `json:"new_password" minLength:"1" maxLength:"100" example:"newpassword123" doc:"新密碼"`
	} `json:"body"`
}

type ChangePasswordResponse struct {
	Body struct {
		Message     string `json:"message" example:"密碼修改成功" doc:"回應訊息"`
		NewPassword string `json:"new_password" example:"newpassword123" doc:"新密碼"`
	} `json:"body"`
}

type UpdateUserProfileInput struct {
	Body struct {
		Name     string `json:"name,omitempty" maxLength:"50" example:"管理員" doc:"姓名"`
		Account  string `json:"account,omitempty" maxLength:"100" example:"admin@taxi.com" doc:"帳號"`
		Role     string `json:"role,omitempty" maxLength:"50" example:"管理員" doc:"角色"`
		Fleet    string `json:"fleet,omitempty" maxLength:"50" example:"RSK" doc:"所屬車隊"`
		Password string `json:"password,omitempty" minLength:"1" maxLength:"100" example:"newpassword123" doc:"新密碼（若提供則立即更改密碼）"`
	} `json:"body"`
}

type UpdateUserProfileByIdInput struct {
	ID   string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"用戶ID"`
	Body struct {
		Name     string `json:"name,omitempty" maxLength:"50" example:"管理員" doc:"姓名"`
		Account  string `json:"account,omitempty" maxLength:"100" example:"admin@taxi.com" doc:"帳號"`
		Role     string `json:"role,omitempty" maxLength:"50" example:"管理員" doc:"角色"`
		Fleet    string `json:"fleet,omitempty" maxLength:"50" example:"RSK" doc:"所屬車隊"`
		Password string `json:"password,omitempty" minLength:"1" maxLength:"100" example:"newpassword123" doc:"新密碼（若提供則立即更改密碼）"`
	} `json:"body"`
}

type RemoveUserInput struct {
	ID string `path:"id" maxLength:"24" minLength:"24" example:"507f1f77bcf86cd799439011" doc:"用戶ID"`
}

type RemoveUserResponse struct {
	Body struct {
		Message string `json:"message" example:"用戶角色已移除" doc:"操作結果訊息"`
		UserID  string `json:"user_id" example:"507f1f77bcf86cd799439011" doc:"被移除角色的用戶ID"`
	} `json:"body"`
}
